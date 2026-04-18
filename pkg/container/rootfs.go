package container

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const fallbackResolvConf = "nameserver 1.1.1.1\nnameserver 8.8.8.8\n"

func setupRootfs(rootfs string, mounts []Mount) error {
	if err := ensureRootfsBasics(rootfs); err != nil {
		return err
	}

	if err := writeContainerResolvConf(rootfs); err != nil {
		return fmt.Errorf("write resolv.conf: %w", err)
	}

	if err := applyBindMounts(rootfs, mounts); err != nil {
		return fmt.Errorf("apply bind mounts: %w", err)
	}

	if err := unix.Chroot(rootfs); err != nil {
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
		filepath.Join(rootfs, "etc"),
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

func applyBindMounts(rootfs string, mounts []Mount) error {
	for _, m := range mounts {
		if err := applySingleBindMount(rootfs, m); err != nil {
			return err
		}
	}
	return nil
}

func applySingleBindMount(rootfs string, m Mount) error {
	info, err := os.Stat(m.Source)
	if err != nil {
		return fmt.Errorf("stat mount source %s: %w", m.Source, err)
	}

	targetRel := strings.TrimPrefix(filepath.Clean(m.Target), "/")
	targetPath := filepath.Join(rootfs, targetRel)

	if info.IsDir() {
		if err := os.MkdirAll(targetPath, 0o755); err != nil {
			return fmt.Errorf("create mount target dir %s: %w", targetPath, err)
		}
	} else {
		parent := filepath.Dir(targetPath)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("create mount target parent %s: %w", parent, err)
		}
		f, err := os.OpenFile(targetPath, os.O_CREATE, 0o644)
		if err != nil {
			return fmt.Errorf("create mount target file %s: %w", targetPath, err)
		}
		_ = f.Close()
	}

	if err := unix.Mount(m.Source, targetPath, "", uintptr(unix.MS_BIND), ""); err != nil {
		return fmt.Errorf("bind mount %s -> %s: %w", m.Source, m.Target, err)
	}

	return nil
}

func writeContainerResolvConf(rootfs string) error {
	content, err := readHostResolvConf()
	if err != nil {
		return err
	}

	content = sanitizeResolvConf(content)

	dstPath := filepath.Join(rootfs, "etc", "resolv.conf")
	tmpPath := dstPath + ".tmp"

	if err := os.WriteFile(tmpPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, dstPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace %s: %w", dstPath, err)
	}

	return nil
}

func readHostResolvConf() (string, error) {
	candidates := []string{
		"/run/systemd/resolve/resolv.conf",
		"/etc/resolv.conf",
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err == nil {
			return string(data), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
	}

	return "", fmt.Errorf("no resolv.conf source found")
}

func sanitizeResolvConf(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return fallbackResolvConf
	}

	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "nameserver" && fields[1] == "127.0.0.53" {
			return fallbackResolvConf
		}
	}

	return content
}

func setupMinimalDev() error {
	if err := os.MkdirAll("/dev", 0o755); err != nil {
		return fmt.Errorf("create /dev: %w", err)
	}

	if err := unix.Mount("tmpfs", "/dev", "tmpfs", uintptr(unix.MS_NOSUID), "mode=755,size=64k"); err != nil {
		return fmt.Errorf("mount tmpfs on /dev: %w", err)
	}

	devNull := "/dev/null"
	if err := unix.Mknod(devNull, uint32(unix.S_IFCHR|0o666), int(unix.Mkdev(1, 3))); err != nil {
		return fmt.Errorf("mknod %s: %w", devNull, err)
	}

	if err := os.Chmod(devNull, 0o666); err != nil {
		return fmt.Errorf("chmod %s: %w", devNull, err)
	}

	return nil
}

func mountProc() error {
	return unix.Mount("proc", "/proc", "proc", 0, "")
}

func makeMountsPrivate() error {
	return unix.Mount("", "/", "", uintptr(unix.MS_REC|unix.MS_PRIVATE), "")
}