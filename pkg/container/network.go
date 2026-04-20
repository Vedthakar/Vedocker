package container

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

type ifreqFlags struct {
	Name  [unix.IFNAMSIZ]byte
	Flags uint16
	Pad   [22]byte
}

func currentContainerIP() string {
	contCIDR := os.Getenv(contVethAddrEnv)
	if contCIDR == "" {
		return ""
	}
	parts := strings.SplitN(contCIDR, "/", 2)
	return parts[0]
}

func waitForParentNetwork() error {
	fdStr := os.Getenv(netReadyFDEnv)
	if fdStr == "" {
		return nil
	}

	fd, err := strconv.Atoi(fdStr)
	if err != nil {
		return fmt.Errorf("parse %s: %w", netReadyFDEnv, err)
	}

	file := os.NewFile(uintptr(fd), "net-ready")
	if file == nil {
		return fmt.Errorf("invalid startup pipe fd")
	}
	defer file.Close()

	_, err = io.Copy(io.Discard, file)
	if err != nil {
		return fmt.Errorf("read startup pipe: %w", err)
	}

	return nil
}

func setupVethForChild(pid int, hostIf, contIf, hostCIDR string) error {
	if err := runIP("link", "add", hostIf, "type", "veth", "peer", "name", contIf); err != nil {
		return err
	}
	if err := runIP("addr", "add", hostCIDR, "dev", hostIf); err != nil {
		_ = runIP("link", "del", hostIf)
		return err
	}
	if err := runIP("link", "set", hostIf, "up"); err != nil {
		_ = runIP("link", "del", hostIf)
		return err
	}
	if err := runIP("link", "set", contIf, "netns", strconv.Itoa(pid)); err != nil {
		_ = runIP("link", "del", hostIf)
		return err
	}

	return nil
}

func configureContainerVeth() error {
	contIf := os.Getenv(contVethNameEnv)
	contCIDR := os.Getenv(contVethAddrEnv)
	targetIf := os.Getenv(contIfRenameEnv)

	if contIf == "" || contCIDR == "" || targetIf == "" {
		return nil
	}

	if err := runIP("link", "set", contIf, "name", targetIf); err != nil {
		return err
	}
	if err := runIP("addr", "add", contCIDR, "dev", targetIf); err != nil {
		return err
	}
	if err := runIP("link", "set", targetIf, "up"); err != nil {
		return err
	}

	return nil
}

func addDefaultRoute() error {
	gateway := os.Getenv(defaultGatewayEnv)
	iface := os.Getenv(contIfRenameEnv)
	if gateway == "" || iface == "" {
		return nil
	}

	return runIP("route", "add", "default", "via", gateway, "dev", iface)
}

func setupHostEgress(hostIf string) (func(), error) {
	outIf, err := detectDefaultRouteInterface()
	if err != nil {
		return nil, err
	}

	if err := enableIPv4Forwarding(); err != nil {
		return nil, err
	}

	if err := runIPTables("-A", "FORWARD", "-i", hostIf, "-o", outIf, "-j", "ACCEPT"); err != nil {
		return nil, err
	}
	if err := runIPTables("-A", "FORWARD", "-i", outIf, "-o", hostIf, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"); err != nil {
		_ = runIPTables("-D", "FORWARD", "-i", hostIf, "-o", outIf, "-j", "ACCEPT")
		return nil, err
	}
	if err := runIPTables("-t", "nat", "-A", "POSTROUTING", "-s", "10.200.1.0/24", "-o", outIf, "-j", "MASQUERADE"); err != nil {
		_ = runIPTables("-D", "FORWARD", "-i", hostIf, "-o", outIf, "-j", "ACCEPT")
		_ = runIPTables("-D", "FORWARD", "-i", outIf, "-o", hostIf, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT")
		return nil, err
	}

	cleanup := func() {
		_ = runIPTables("-t", "nat", "-D", "POSTROUTING", "-s", "10.200.1.0/24", "-o", outIf, "-j", "MASQUERADE")
		_ = runIPTables("-D", "FORWARD", "-i", outIf, "-o", hostIf, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT")
		_ = runIPTables("-D", "FORWARD", "-i", hostIf, "-o", outIf, "-j", "ACCEPT")
	}

	return cleanup, nil
}

func setupPublishedPorts(ports []Port) (func(), error) {
	if len(ports) == 0 {
		return func() {}, nil
	}

	containerIP := currentContainerIP()
	if containerIP == "" {
		return nil, fmt.Errorf("missing container IP for published ports")
	}

	if err := cleanupPublishedPortRules(containerIP, ports); err != nil {
		return nil, err
	}

	for _, p := range ports {
		if err := addSinglePublishedPortRule(containerIP, p); err != nil {
			_ = cleanupPublishedPortRules(containerIP, ports)
			return nil, err
		}
	}

	return func() {
		_ = cleanupPublishedPortRules(containerIP, ports)
	}, nil
}

func addSinglePublishedPortRule(containerIP string, p Port) error {
	hostPort := strconv.Itoa(p.HostPort)
	containerDest := fmt.Sprintf("%s:%d", containerIP, p.ContainerPort)
	containerPort := strconv.Itoa(p.ContainerPort)

	if err := enableRouteLocalnet(); err != nil {
		return err
	}
	if err := runIPTables("-t", "nat", "-A", "PREROUTING", "-p", "tcp", "--dport", hostPort, "-j", "DNAT", "--to-destination", containerDest); err != nil {
		return err
	}
	if err := runIPTables("-t", "nat", "-A", "OUTPUT", "-p", "tcp", "--dport", hostPort, "-j", "DNAT", "--to-destination", containerDest); err != nil {
		_ = runIPTables("-t", "nat", "-D", "PREROUTING", "-p", "tcp", "--dport", hostPort, "-j", "DNAT", "--to-destination", containerDest)
		return err
	}
	if err := runIPTables("-A", "FORWARD", "-p", "tcp", "-d", containerIP, "--dport", containerPort, "-j", "ACCEPT"); err != nil {
		_ = runIPTables("-t", "nat", "-D", "OUTPUT", "-p", "tcp", "--dport", hostPort, "-j", "DNAT", "--to-destination", containerDest)
		_ = runIPTables("-t", "nat", "-D", "PREROUTING", "-p", "tcp", "--dport", hostPort, "-j", "DNAT", "--to-destination", containerDest)
		return err
	}
	if err := runIPTables("-t", "nat", "-A", "POSTROUTING", "-p", "tcp", "-d", containerIP, "--dport", containerPort, "-j", "MASQUERADE"); err != nil {
		_ = runIPTables("-D", "FORWARD", "-p", "tcp", "-d", containerIP, "--dport", containerPort, "-j", "ACCEPT")
		_ = runIPTables("-t", "nat", "-D", "OUTPUT", "-p", "tcp", "--dport", hostPort, "-j", "DNAT", "--to-destination", containerDest)
		_ = runIPTables("-t", "nat", "-D", "PREROUTING", "-p", "tcp", "--dport", hostPort, "-j", "DNAT", "--to-destination", containerDest)
		return err
	}

	return nil
}

func cleanupPublishedPortRules(containerIP string, ports []Port) error {
	hostPorts := make(map[int]struct{})
	for _, p := range ports {
		hostPorts[p.HostPort] = struct{}{}
	}

	for hostPort := range hostPorts {
		if err := cleanupDNATRulesForHostPort(containerIP, hostPort); err != nil {
			return err
		}
	}

	if err := cleanupForwardRulesForPublishedContainer(containerIP); err != nil {
		return err
	}

	if err := cleanupPostroutingRulesForPublishedContainer(containerIP); err != nil {
		return err
	}

	return nil
}

func cleanupPostroutingRulesForPublishedContainer(containerIP string) error {
	return deleteRulesMatching("nat", "POSTROUTING", func(line string) bool {
		return strings.HasPrefix(line, "-A POSTROUTING ") &&
			strings.Contains(line, "-p tcp") &&
			strings.Contains(line, "-d "+containerIP) &&
			strings.Contains(line, "-j MASQUERADE")
	})
}

func cleanupDNATRulesForHostPort(containerIP string, hostPort int) error {
	match := fmt.Sprintf("--dport %d", hostPort)

	if err := deleteRulesMatching("nat", "PREROUTING", func(line string) bool {
		return strings.HasPrefix(line, "-A PREROUTING ") &&
			strings.Contains(line, "-p tcp") &&
			strings.Contains(line, match) &&
			strings.Contains(line, "-j DNAT") &&
			strings.Contains(line, "--to-destination "+containerIP+":")
	}); err != nil {
		return err
	}

	if err := deleteRulesMatching("nat", "OUTPUT", func(line string) bool {
		return strings.HasPrefix(line, "-A OUTPUT ") &&
			strings.Contains(line, "-p tcp") &&
			strings.Contains(line, match) &&
			strings.Contains(line, "-j DNAT") &&
			strings.Contains(line, "--to-destination "+containerIP+":")
	}); err != nil {
		return err
	}

	return nil
}

func cleanupForwardRulesForPublishedContainer(containerIP string) error {
	return deleteRulesMatching("", "FORWARD", func(line string) bool {
		return strings.HasPrefix(line, "-A FORWARD ") &&
			strings.Contains(line, "-p tcp") &&
			strings.Contains(line, "-d "+containerIP) &&
			strings.Contains(line, "-j ACCEPT")
	})
}

func deleteRulesMatching(table, chain string, match func(string) bool) error {
	lines, err := listIPTablesRules(table, chain)
	if err != nil {
		return err
	}

	for _, line := range lines {
		if !match(line) {
			continue
		}
		if err := deleteRuleFromLine(table, line); err != nil {
			return err
		}
	}

	return nil
}

func listIPTablesRules(table, chain string) ([]string, error) {
	args := []string{}
	if table != "" {
		args = append(args, "-t", table)
	}
	args = append(args, "-S", chain)

	cmd := exec.Command("iptables", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return nil, fmt.Errorf("iptables %s: %w", strings.Join(args, " "), err)
		}
		return nil, fmt.Errorf("iptables %s: %s", strings.Join(args, " "), msg)
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}

func deleteRuleFromLine(table, line string) error {
	if !strings.HasPrefix(line, "-A ") {
		return nil
	}

	rule := strings.Replace(line, "-A ", "-D ", 1)
	args := []string{}
	if table != "" {
		args = append(args, "-t", table)
	}
	args = append(args, strings.Fields(rule)...)

	return runIPTables(args...)
}

func detectDefaultRouteInterface() (string, error) {
	cmd := exec.Command("ip", "route", "show", "default")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("detect default route interface: %w", err)
	}

	fields := strings.Fields(string(out))
	for i := 0; i < len(fields)-1; i++ {
		if fields[i] == "dev" {
			return fields[i+1], nil
		}
	}

	return "", fmt.Errorf("could not find outbound interface from default route")
}

func enableIPv4Forwarding() error {
	return os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1\n"), 0o644)
}

func enableRouteLocalnet() error {
	if err := os.WriteFile("/proc/sys/net/ipv4/conf/all/route_localnet", []byte("1\n"), 0o644); err != nil {
		return fmt.Errorf("enable route_localnet all: %w", err)
	}
	if err := os.WriteFile("/proc/sys/net/ipv4/conf/lo/route_localnet", []byte("1\n"), 0o644); err != nil {
		return fmt.Errorf("enable route_localnet lo: %w", err)
	}
	return nil
}
func runIP(args ...string) error {
	cmd := exec.Command("ip", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("ip %s: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("ip %s: %s", strings.Join(args, " "), msg)
	}
	return nil
}

func runIPTables(args ...string) error {
	cmd := exec.Command("iptables", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("iptables %s: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("iptables %s: %s", strings.Join(args, " "), msg)
	}
	return nil
}

func randomVethNames() (string, string) {
	suffix := randomID()[:5]
	return "mcvh" + suffix, "mcvc" + suffix
}

func bringLoopbackUp() error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("open control socket: %w", err)
	}
	defer unix.Close(fd)

	var req ifreqFlags
	copy(req.Name[:], "lo")

	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(unix.SIOCGIFFLAGS),
		uintptr(unsafe.Pointer(&req)),
	)
	if errno != 0 {
		return fmt.Errorf("get lo flags: %w", errno)
	}

	req.Flags |= unix.IFF_UP | unix.IFF_RUNNING

	_, _, errno = unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(unix.SIOCSIFFLAGS),
		uintptr(unsafe.Pointer(&req)),
	)
	if errno != 0 {
		return fmt.Errorf("set lo flags: %w", errno)
	}

	return nil
}
