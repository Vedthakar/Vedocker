package container

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func setupCgroup() (string, func(), error) {
	if err := ensureCgroupV2Root(); err != nil {
		return "", nil, err
	}
	if err := enableControllers(cgroupRoot, []string{"cpu", "memory", "pids"}); err != nil {
		return "", nil, err
	}

	name := "minicontainer-" + randomID()
	path := filepath.Join(cgroupRoot, name)

	if err := os.Mkdir(path, 0o755); err != nil {
		return "", nil, fmt.Errorf("create cgroup %s: %w", path, err)
	}

	cleanup := func() {
		_ = os.Remove(path)
	}

	if err := writeFile(filepath.Join(path, "memory.max"), "268435456\n"); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write memory.max: %w", err)
	}

	if err := writeFile(filepath.Join(path, "pids.max"), "64\n"); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write pids.max: %w", err)
	}

	if err := writeFile(filepath.Join(path, "cpu.max"), "50000 100000\n"); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write cpu.max: %w", err)
	}

	return path, cleanup, nil
}

func joinAssignedCgroup() error {
	path := os.Getenv(cgroupEnvVar)
	if path == "" {
		return nil
	}

	pid := os.Getpid()
	procsPath := filepath.Join(path, "cgroup.procs")
	if err := writeFile(procsPath, fmt.Sprintf("%d\n", pid)); err != nil {
		return fmt.Errorf("join %s: %w", path, err)
	}

	return nil
}

func ensureCgroupV2Root() error {
	controllersPath := filepath.Join(cgroupRoot, "cgroup.controllers")
	info, err := os.Stat(controllersPath)
	if err != nil {
		return fmt.Errorf("cgroup v2 not available at %s: %w", controllersPath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory, expected file", controllersPath)
	}
	return nil
}

func enableControllers(parent string, controllers []string) error {
	availableBytes, err := os.ReadFile(filepath.Join(parent, "cgroup.controllers"))
	if err != nil {
		return fmt.Errorf("read cgroup.controllers: %w", err)
	}
	enabledBytes, err := os.ReadFile(filepath.Join(parent, "cgroup.subtree_control"))
	if err != nil {
		return fmt.Errorf("read cgroup.subtree_control: %w", err)
	}

	available := fieldsSet(string(availableBytes))
	enabled := fieldsSet(string(enabledBytes))

	var toEnable []string
	for _, c := range controllers {
		if !available[c] {
			return fmt.Errorf("controller %q is not available", c)
		}
		if !enabled[c] {
			toEnable = append(toEnable, "+"+c)
		}
	}

	if len(toEnable) == 0 {
		return nil
	}

	data := strings.Join(toEnable, " ") + "\n"
	if err := writeFile(filepath.Join(parent, "cgroup.subtree_control"), data); err != nil {
		return fmt.Errorf("enable controllers %v: %w", controllers, err)
	}

	return nil
}

func fieldsSet(s string) map[string]bool {
	out := make(map[string]bool)
	for _, f := range strings.Fields(s) {
		out[f] = true
	}
	return out
}

func writeFile(path, value string) error {
	return os.WriteFile(path, []byte(value), 0o644)
}