package container

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	extraEnvSpecEnv = "MINICONTAINER_EXTRA_ENV_SPEC"
	mountSpecEnv    = "MINICONTAINER_MOUNT_SPEC"
)

type Mount struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type Port struct {
	HostPort      int `json:"host_port"`
	ContainerPort int `json:"container_port"`
}

func ParseMountSpec(spec string) (Mount, error) {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 {
		return Mount{}, fmt.Errorf("invalid mount %q, expected /host/path:/container/path", spec)
	}

	source := parts[0]
	target := parts[1]

	if source == "" || target == "" {
		return Mount{}, fmt.Errorf("invalid mount %q, source and target are required", spec)
	}
	if !strings.HasPrefix(target, "/") {
		return Mount{}, fmt.Errorf("invalid mount target %q, must be absolute", target)
	}

	absSource, err := filepath.Abs(source)
	if err != nil {
		return Mount{}, fmt.Errorf("resolve mount source %q: %w", source, err)
	}

	if _, err := os.Stat(absSource); err != nil {
		return Mount{}, fmt.Errorf("stat mount source %q: %w", absSource, err)
	}

	return Mount{
		Source: absSource,
		Target: filepath.Clean(target),
	}, nil
}

func ParsePortSpec(spec string) (Port, error) {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 {
		return Port{}, fmt.Errorf("invalid port %q, expected HOST_PORT:CONTAINER_PORT", spec)
	}

	hostPort, err := strconv.Atoi(parts[0])
	if err != nil {
		return Port{}, fmt.Errorf("invalid host port %q", parts[0])
	}
	containerPort, err := strconv.Atoi(parts[1])
	if err != nil {
		return Port{}, fmt.Errorf("invalid container port %q", parts[1])
	}

	if hostPort < 1 || hostPort > 65535 {
		return Port{}, fmt.Errorf("host port out of range: %d", hostPort)
	}
	if containerPort < 1 || containerPort > 65535 {
		return Port{}, fmt.Errorf("container port out of range: %d", containerPort)
	}

	return Port{
		HostPort:      hostPort,
		ContainerPort: containerPort,
	}, nil
}

func encodeEnvSpec(env []string) (string, error) {
	data, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func encodeMountSpec(mounts []Mount) (string, error) {
	data, err := json.Marshal(mounts)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeEnvSpecFromProcess() ([]string, error) {
	raw := os.Getenv(extraEnvSpecEnv)
	if raw == "" {
		return nil, nil
	}

	var env []string
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return nil, fmt.Errorf("decode env spec: %w", err)
	}
	return env, nil
}

func decodeMountSpecFromProcess() ([]Mount, error) {
	raw := os.Getenv(mountSpecEnv)
	if raw == "" {
		return nil, nil
	}

	var mounts []Mount
	if err := json.Unmarshal([]byte(raw), &mounts); err != nil {
		return nil, fmt.Errorf("decode mount spec: %w", err)
	}
	return mounts, nil
}

func mergeEnv(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}

	out := make([]string, 0, len(base)+len(extra))
	index := make(map[string]int)

	for _, item := range base {
		key := envKey(item)
		index[key] = len(out)
		out = append(out, item)
	}

	for _, item := range extra {
		key := envKey(item)
		if pos, ok := index[key]; ok {
			out[pos] = item
			continue
		}
		index[key] = len(out)
		out = append(out, item)
	}

	return out
}

func envKey(item string) string {
	if idx := strings.IndexByte(item, '='); idx >= 0 {
		return item[:idx]
	}
	return item
}

func formatMountArg(m Mount) string {
	return m.Source + ":" + m.Target
}

func formatPortArg(p Port) string {
	return fmt.Sprintf("%d:%d", p.HostPort, p.ContainerPort)
}