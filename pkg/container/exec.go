package container

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"golang.org/x/sys/unix"
)

func Exec(id string, command []string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("this command must be run as root")
	}
	if len(command) == 0 {
		return fmt.Errorf("no command provided")
	}

	state, err := loadState(id)
	if err != nil {
		return err
	}
	if state.PID <= 0 || !processAlive(state.PID) {
		return fmt.Errorf("container %q is not running", id)
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current executable: %w", err)
	}

	args := append([]string{"exec-child", id}, command...)
	cmd := exec.Command(self, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func ExecChild(id string, command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("no command provided")
	}

	state, err := loadState(id)
	if err != nil {
		return err
	}
	if state.PID <= 0 || !processAlive(state.PID) {
		return fmt.Errorf("container %q is not running", id)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := unix.Unshare(unix.CLONE_FS); err != nil {
		return fmt.Errorf("unshare CLONE_FS: %w", err)
	}

	nsFiles, err := openNamespaceFiles(state.PID)
	if err != nil {
		return err
	}
	defer closeNamespaceFiles(nsFiles)

	for _, name := range []string{"uts", "ipc", "net", "mnt", "pid"} {
		if err := unix.Setns(int(nsFiles[name].Fd()), 0); err != nil {
			return fmt.Errorf("setns %s: %w", name, err)
		}
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current executable: %w", err)
	}

	args := append([]string{"exec-stage2", state.Rootfs}, command...)
	cmd := exec.Command(self, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func ExecStage2(rootfs string, command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("no command provided")
	}

	if err := unix.Chroot(rootfs); err != nil {
		return fmt.Errorf("chroot to %s: %w", rootfs, err)
	}
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("chdir to new root: %w", err)
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

	return unix.Exec(binary, command, os.Environ())
}

func openNamespaceFiles(pid int) (map[string]*os.File, error) {
	names := []string{"uts", "ipc", "net", "mnt", "pid"}
	files := make(map[string]*os.File, len(names))

	for _, name := range names {
		path := filepath.Join("/proc", fmt.Sprintf("%d", pid), "ns", name)
		f, err := os.Open(path)
		if err != nil {
			closeNamespaceFiles(files)
			return nil, fmt.Errorf("open namespace %s: %w", name, err)
		}
		files[name] = f
	}

	return files, nil
}

func closeNamespaceFiles(files map[string]*os.File) {
	for _, f := range files {
		_ = f.Close()
	}
}
