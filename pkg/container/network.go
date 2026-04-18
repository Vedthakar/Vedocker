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

	var cleanups []func()

	for _, p := range ports {
		cleanup, err := setupSinglePublishedPort(p)
		if err != nil {
			for i := len(cleanups) - 1; i >= 0; i-- {
				cleanups[i]()
			}
			return nil, err
		}
		cleanups = append(cleanups, cleanup)
	}

	return func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}, nil
}

func setupSinglePublishedPort(p Port) (func(), error) {
	hostPort := strconv.Itoa(p.HostPort)
	containerDest := fmt.Sprintf("10.200.1.2:%d", p.ContainerPort)
	containerPort := strconv.Itoa(p.ContainerPort)

	if err := runIPTables("-t", "nat", "-A", "PREROUTING", "-p", "tcp", "--dport", hostPort, "-j", "DNAT", "--to-destination", containerDest); err != nil {
		return nil, err
	}
	if err := runIPTables("-t", "nat", "-A", "OUTPUT", "-p", "tcp", "--dport", hostPort, "-j", "DNAT", "--to-destination", containerDest); err != nil {
		_ = runIPTables("-t", "nat", "-D", "PREROUTING", "-p", "tcp", "--dport", hostPort, "-j", "DNAT", "--to-destination", containerDest)
		return nil, err
	}
	if err := runIPTables("-A", "FORWARD", "-p", "tcp", "-d", "10.200.1.2", "--dport", containerPort, "-j", "ACCEPT"); err != nil {
		_ = runIPTables("-t", "nat", "-D", "OUTPUT", "-p", "tcp", "--dport", hostPort, "-j", "DNAT", "--to-destination", containerDest)
		_ = runIPTables("-t", "nat", "-D", "PREROUTING", "-p", "tcp", "--dport", hostPort, "-j", "DNAT", "--to-destination", containerDest)
		return nil, err
	}

	cleanup := func() {
		_ = runIPTables("-D", "FORWARD", "-p", "tcp", "-d", "10.200.1.2", "--dport", containerPort, "-j", "ACCEPT")
		_ = runIPTables("-t", "nat", "-D", "OUTPUT", "-p", "tcp", "--dport", hostPort, "-j", "DNAT", "--to-destination", containerDest)
		_ = runIPTables("-t", "nat", "-D", "PREROUTING", "-p", "tcp", "--dport", hostPort, "-j", "DNAT", "--to-destination", containerDest)
	}

	return cleanup, nil
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