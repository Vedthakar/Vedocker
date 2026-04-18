package container

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

const (
	cgroupRoot   = "/sys/fs/cgroup"
	cgroupEnvVar = "MINICONTAINER_CGROUP_PATH"

	netReadyFDEnv     = "MINICONTAINER_NET_READY_FD"
	hostVethNameEnv   = "MINICONTAINER_HOST_VETH_NAME"
	contVethNameEnv   = "MINICONTAINER_CONT_VETH_NAME"
	hostVethAddrEnv   = "MINICONTAINER_HOST_VETH_ADDR"
	contVethAddrEnv   = "MINICONTAINER_CONT_VETH_ADDR"
	contIfRenameEnv   = "MINICONTAINER_CONT_IF_RENAME"
	defaultGatewayEnv = "MINICONTAINER_DEFAULT_GW"
	pidFileEnv        = "MINICONTAINER_PIDFILE"

	defaultHostCIDR   = "10.200.1.1/24"
	defaultContCIDR   = "10.200.1.2/24"
	defaultGatewayIP  = "10.200.1.1"
	defaultContIfName = "eth0"
)

func Run(rootfs string, command []string, extraEnv []string, mounts []Mount, ports []Port) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("this command must be run as root")
	}
	if len(command) == 0 {
		return fmt.Errorf("no command provided")
	}

	absRootfs, err := filepath.Abs(rootfs)
	if err != nil {
		return fmt.Errorf("resolve rootfs path: %w", err)
	}

	info, err := os.Stat(absRootfs)
	if err != nil {
		return fmt.Errorf("stat rootfs: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("rootfs must be a directory: %s", absRootfs)
	}

	cgroupPath, cleanupCgroup, err := setupCgroup()
	if err != nil {
		return fmt.Errorf("setup cgroup v2: %w", err)
	}
	defer cleanupCgroup()

	hostIf, contIf := randomVethNames()

	syncR, syncW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("create startup pipe: %w", err)
	}
	defer syncW.Close()

	self, err := os.Executable()
	if err != nil {
		syncR.Close()
		return fmt.Errorf("locate current executable: %w", err)
	}

	args := append([]string{"child", absRootfs}, command...)
	cmd := exec.Command(self, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{syncR}
	cmd.Env = append(os.Environ(),
		cgroupEnvVar+"="+cgroupPath,
		netReadyFDEnv+"=3",
		hostVethNameEnv+"="+hostIf,
		contVethNameEnv+"="+contIf,
		hostVethAddrEnv+"="+defaultHostCIDR,
		contVethAddrEnv+"="+defaultContCIDR,
		contIfRenameEnv+"="+defaultContIfName,
		defaultGatewayEnv+"="+defaultGatewayIP,
	)

	if pidFile := os.Getenv(pidFileEnv); pidFile != "" {
		cmd.Env = append(cmd.Env, pidFileEnv+"="+pidFile)
	}
	if len(extraEnv) > 0 {
		encoded, err := encodeEnvSpec(extraEnv)
		if err != nil {
			return fmt.Errorf("encode env spec: %w", err)
		}
		cmd.Env = append(cmd.Env, extraEnvSpecEnv+"="+encoded)
	}
	if len(mounts) > 0 {
		encoded, err := encodeMountSpec(mounts)
		if err != nil {
			return fmt.Errorf("encode mount spec: %w", err)
		}
		cmd.Env = append(cmd.Env, mountSpecEnv+"="+encoded)
	}

	cmd.SysProcAttr = &unix.SysProcAttr{
		Cloneflags: unix.CLONE_NEWUTS |
			unix.CLONE_NEWPID |
			unix.CLONE_NEWNS |
			unix.CLONE_NEWIPC |
			unix.CLONE_NEWNET,
	}

	if err := cmd.Start(); err != nil {
		syncR.Close()
		return fmt.Errorf("start child: %w", err)
	}
	syncR.Close()

	if err := writeHostPIDFile(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("write pidfile: %w", err)
	}

	cleanupNet := func() {
		_ = runIP("link", "del", hostIf)
	}
	defer cleanupNet()

	if err := setupVethForChild(cmd.Process.Pid, hostIf, contIf, defaultHostCIDR); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("setup veth: %w", err)
	}

	egressCleanup, err := setupHostEgress(hostIf)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("setup host egress: %w", err)
	}
	defer egressCleanup()

	portCleanup, err := setupPublishedPorts(ports)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("setup published ports: %w", err)
	}
	defer portCleanup()

	if err := syncW.Close(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("signal child startup complete: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return err
	}

	return nil
}

func writeHostPIDFile(pid int) error {
	pidFile := os.Getenv(pidFileEnv)
	if pidFile == "" {
		return nil
	}

	data := []byte(fmt.Sprintf("%d\n", pid))
	tmp := pidFile + ".tmp"

	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, pidFile)
}

func Child(rootfs string, command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("no command provided")
	}

	extraEnv, err := decodeEnvSpecFromProcess()
	if err != nil {
		return err
	}
	mounts, err := decodeMountSpecFromProcess()
	if err != nil {
		return err
	}

	if err := joinAssignedCgroup(); err != nil {
		return fmt.Errorf("join cgroup: %w", err)
	}

	if err := waitForParentNetwork(); err != nil {
		return fmt.Errorf("wait for parent network setup: %w", err)
	}

	if err := unix.Sethostname([]byte(randomHostname())); err != nil {
		return fmt.Errorf("set hostname: %w", err)
	}

	if err := bringLoopbackUp(); err != nil {
		return fmt.Errorf("bring loopback up: %w", err)
	}

	if err := configureContainerVeth(); err != nil {
		return fmt.Errorf("configure container veth: %w", err)
	}

	if err := addDefaultRoute(); err != nil {
		return fmt.Errorf("add default route: %w", err)
	}

	if err := makeMountsPrivate(); err != nil {
		return fmt.Errorf("make mounts private: %w", err)
	}

	if err := setupRootfs(rootfs, mounts); err != nil {
		return err
	}

	if err := setupMinimalDev(); err != nil {
		return fmt.Errorf("setup /dev: %w", err)
	}

	if err := mountProc(); err != nil {
		return fmt.Errorf("mount /proc: %w", err)
	}

	if err := hardenProcess(); err != nil {
		return fmt.Errorf("harden process: %w", err)
	}

	if err := installSeccompFilter(); err != nil {
		return fmt.Errorf("install seccomp filter: %w", err)
	}

	binary, err := exec.LookPath(command[0])
	if err != nil {
		return fmt.Errorf("find command %q: %w", command[0], err)
	}

	return unix.Exec(binary, command, mergeEnv(os.Environ(), extraEnv))
}

func randomHostname() string {
	return "mini-" + randomID()
}

func randomID() string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	buf := make([]byte, 8)
	for i := range buf {
		buf[i] = letters[r.Intn(len(letters))]
	}
	return string(buf)
}