package container

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func Run(rootfs string, command []string) error {
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

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current executable: %w", err)
	}

	args := append([]string{"child", absRootfs}, command...)
	cmd := exec.Command(self, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWIPC |
			syscall.CLONE_NEWNET,
	}

	return cmd.Run()
}

func Child(rootfs string, command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("no command provided")
	}

	if err := syscall.Sethostname([]byte(randomHostname())); err != nil {
		return fmt.Errorf("set hostname: %w", err)
	}

	if err := makeMountsPrivate(); err != nil {
		return fmt.Errorf("make mounts private: %w", err)
	}

	if err := setupRootfs(rootfs); err != nil {
		return err
	}

	if err := mountProc(); err != nil {
		return fmt.Errorf("mount /proc: %w", err)
	}
	defer func() {
		_ = syscall.Unmount("/proc", 0)
	}()

	binary, err := exec.LookPath(command[0])
	if err != nil {
		return fmt.Errorf("find command %q: %w", command[0], err)
	}

	return syscall.Exec(binary, command, os.Environ())
}

func makeMountsPrivate() error {
	return syscall.Mount("", "/", "", uintptr(syscall.MS_REC|syscall.MS_PRIVATE), "")
}

func setupRootfs(rootfs string) error {
	if err := ensureRootfsBasics(rootfs); err != nil {
		return err
	}

	// Phase 1: use chroot for safety and simplicity.
	// pivot_root is intentionally deferred until mount propagation and rootfs placement
	// are handled more robustly.
	if err := syscall.Chroot(rootfs); err != nil {
		return fmt.Errorf("chroot to %s: %w", rootfs, err)
	}
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("chdir to new root: %w", err)
	}

	return nil
}

func ensureRootfsBasics(rootfs string) error {
	required := []string{
		filepath.Join(rootfs, "proc"),
		filepath.Join(rootfs, "dev"),
		filepath.Join(rootfs, "tmp"),
	}

	for _, p := range required {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return fmt.Errorf("create required path %s: %w", p, err)
		}
	}

	shPath := filepath.Join(rootfs, "bin", "sh")
	if _, err := os.Stat(shPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check %s: %w", shPath, err)
	}

	return nil
}

func mountProc() error {
	return syscall.Mount("proc", "/proc", "proc", 0, "")
}

func randomHostname() string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	buf := make([]byte, 8)
	for i := range buf {
		buf[i] = letters[r.Intn(len(letters))]
	}
	return "mini-" + string(buf)
}

