package container

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const containerStateRoot = "/var/lib/minicontainer/containers"

type ContainerState struct {
	ID        string   `json:"id"`
	Rootfs    string   `json:"rootfs"`
	Command   []string `json:"command"`
	Env       []string `json:"env"`
	Mounts    []Mount  `json:"mounts"`
	Ports     []Port   `json:"ports"`
	Status    string   `json:"status"`
	PID       int      `json:"pid"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

func Create(id, rootfs string, command []string, env []string, mounts []Mount, ports []Port) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("this command must be run as root")
	}
	if err := validateContainerID(id); err != nil {
		return err
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

	dir := containerDir(id)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("container %q already exists", id)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check container dir: %w", err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create container dir: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	state := &ContainerState{
		ID:        id,
		Rootfs:    absRootfs,
		Command:   append([]string(nil), command...),
		Env:       append([]string(nil), env...),
		Mounts:    append([]Mount(nil), mounts...),
		Ports:     append([]Port(nil), ports...),
		Status:    "created",
		PID:       0,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := writeState(state); err != nil {
		_ = os.RemoveAll(dir)
		return err
	}

	return nil
}

func Start(id string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("this command must be run as root")
	}

	state, err := loadState(id)
	if err != nil {
		return err
	}

	if state.Status == "running" && processAlive(state.PID) {
		return fmt.Errorf("container %q is already running with pid %d", id, state.PID)
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current executable: %w", err)
	}

	stdoutLog, err := os.OpenFile(filepath.Join(containerDir(id), "stdout.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open stdout log: %w", err)
	}
	defer stdoutLog.Close()

	stderrLog, err := os.OpenFile(filepath.Join(containerDir(id), "stderr.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open stderr log: %w", err)
	}
	defer stderrLog.Close()

	pidFile := filepath.Join(containerDir(id), "container.pid")
	_ = os.Remove(pidFile)

	args := []string{"run"}
	for _, item := range state.Env {
		args = append(args, "-e", item)
	}
	for _, m := range state.Mounts {
		args = append(args, "-v", formatMountArg(m))
	}
	for _, p := range state.Ports {
		args = append(args, "-p", formatPortArg(p))
	}
	args = append(args, state.Rootfs)
	args = append(args, state.Command...)

	cmd := exec.Command(self, args...)
	cmd.Stdin = nil
	cmd.Stdout = stdoutLog
	cmd.Stderr = stderrLog
	cmd.Env = append(os.Environ(), pidFileEnv+"="+pidFile)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start container process: %w", err)
	}

	containerPID, err := waitForPIDFile(pidFile, 5*time.Second)
	if err != nil {
		_ = syscall.Kill(cmd.Process.Pid, syscall.SIGKILL)
		_ = os.Remove(pidFile)
		return err
	}

	if err := waitForProcessAlive(containerPID, 500*time.Millisecond); err != nil {
		_ = syscall.Kill(cmd.Process.Pid, syscall.SIGKILL)
		_ = os.Remove(pidFile)
		_ = cleanupPublishedPortRules(state.Ports)
		state.Status = "created"
		state.PID = 0
		state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		_ = writeState(state)
		return err
	}

	state.Status = "running"
	state.PID = containerPID
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := writeState(state); err != nil {
		_ = syscall.Kill(cmd.Process.Pid, syscall.SIGKILL)
		_ = os.Remove(pidFile)
		_ = cleanupPublishedPortRules(state.Ports)
		return err
	}

	_ = os.Remove(pidFile)
	return cmd.Process.Release()
}

func waitForPIDFile(path string, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(string(bytesTrimSpace(data)))
			if parseErr != nil {
				return 0, fmt.Errorf("parse pidfile: %w", parseErr)
			}
			if pid <= 0 {
				return 0, fmt.Errorf("invalid pid %d in pidfile", pid)
			}
			return pid, nil
		}
		if !os.IsNotExist(err) {
			return 0, fmt.Errorf("read pidfile: %w", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	return 0, fmt.Errorf("timed out waiting for container pidfile")
}

func waitForProcessAlive(pid int, duration time.Duration) error {
	deadline := time.Now().Add(duration)

	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return fmt.Errorf("container process %d exited immediately", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !processAlive(pid) {
		return fmt.Errorf("container process %d exited immediately", pid)
	}

	return nil
}

func bytesTrimSpace(data []byte) string {
	start := 0
	end := len(data)

	for start < end {
		switch data[start] {
		case ' ', '\n', '\r', '\t':
			start++
		default:
			goto trimEnd
		}
	}

trimEnd:
	for end > start {
		switch data[end-1] {
		case ' ', '\n', '\r', '\t':
			end--
		default:
			return string(data[start:end])
		}
	}

	return string(data[start:end])
}

func Stop(id string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("this command must be run as root")
	}

	state, err := loadState(id)
	if err != nil {
		return err
	}

	if state.PID <= 0 || !processAlive(state.PID) {
		_ = cleanupPublishedPortRules(state.Ports)
		state.Status = "stopped"
		state.PID = 0
		state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		return writeState(state)
	}

	if err := syscall.Kill(state.PID, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		return fmt.Errorf("send SIGTERM: %w", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(state.PID) {
			_ = cleanupPublishedPortRules(state.Ports)
			state.Status = "stopped"
			state.PID = 0
			state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			return writeState(state)
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := syscall.Kill(state.PID, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		return fmt.Errorf("send SIGKILL: %w", err)
	}

	_ = cleanupPublishedPortRules(state.Ports)
	state.Status = "stopped"
	state.PID = 0
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return writeState(state)
}

func Remove(id string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("this command must be run as root")
	}

	state, err := loadState(id)
	if err != nil {
		return err
	}

	if state.PID > 0 && processAlive(state.PID) {
		return fmt.Errorf("container %q is still running", id)
	}

	_ = cleanupPublishedPortRules(state.Ports)
	return os.RemoveAll(containerDir(id))
}

func Logs(id, stream string) error {
	if err := validateContainerID(id); err != nil {
		return err
	}

	logName, err := logFileName(stream)
	if err != nil {
		return err
	}

	path := filepath.Join(containerDir(id), logName)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("log file not found: %s", path)
		}
		return fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(os.Stdout, f); err != nil {
		return fmt.Errorf("print log file: %w", err)
	}

	return nil
}

func logFileName(stream string) (string, error) {
	switch stream {
	case "", "stdout":
		return "stdout.log", nil
	case "stderr":
		return "stderr.log", nil
	default:
		return "", fmt.Errorf("invalid log stream %q, expected stdout or stderr", stream)
	}
}

func containerDir(id string) string {
	return filepath.Join(containerStateRoot, id)
}

func statePath(id string) string {
	return filepath.Join(containerDir(id), "state.json")
}

func loadState(id string) (*ContainerState, error) {
	if err := validateContainerID(id); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(statePath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("container %q does not exist", id)
		}
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var state ContainerState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse state file: %w", err)
	}

	return &state, nil
}

func writeState(state *ContainerState) error {
	if err := os.MkdirAll(containerDir(state.ID), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	data = append(data, '\n')

	tmp := statePath(state.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp state file: %w", err)
	}

	if err := os.Rename(tmp, statePath(state.ID)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace state file: %w", err)
	}

	return nil
}

func validateContainerID(id string) error {
	if id == "" {
		return fmt.Errorf("container id cannot be empty")
	}
	for _, r := range id {
		isLower := r >= 'a' && r <= 'z'
		isUpper := r >= 'A' && r <= 'Z'
		isDigit := r >= '0' && r <= '9'
		isSafe := r == '-' || r == '_'
		if !isLower && !isUpper && !isDigit && !isSafe {
			return fmt.Errorf("invalid container id %q", id)
		}
	}
	return nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}