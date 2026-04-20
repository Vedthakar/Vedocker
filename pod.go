package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vthecar/minicontainer/pkg/container"
	"gopkg.in/yaml.v3"
)

const podStateRoot = "/var/lib/minicontainer/pods"

type PodManifest struct {
	APIVersion string      `yaml:"apiVersion"`
	Kind       string      `yaml:"kind"`
	Metadata   PodMetadata `yaml:"metadata"`
	Spec       PodSpec     `yaml:"spec"`
}

type PodMetadata struct {
	Name string `yaml:"name"`
}

type PodSpec struct {
	Image   string    `yaml:"image"`
	Command []string  `yaml:"command"`
	Ports   []PodPort `yaml:"ports"`
}

type PodPort struct {
	ContainerPort int `yaml:"containerPort"`
	HostPort      int `yaml:"hostPort"`
}

type PodState struct {
	Name          string `json:"name"`
	ContainerName string `json:"container_name"`
	ManifestPath  string `json:"manifest_path"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

func podCmd(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: minicontainer pod apply -f <file>\n       minicontainer pod delete <name>\n       minicontainer pod get")
	}

	switch args[0] {
	case "apply":
		return podApplyCmd(args[1:])
	case "delete":
		return podDeleteCmd(args[1:])
	case "get":
		return podGet(args[1:])
	default:
		return fmt.Errorf("unknown pod subcommand: %s", args[0])
	}
}

func podGet(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: minicontainer pod get")
	}

	entries, err := os.ReadDir(podStateRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var pods []PodState

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(podStateRoot, entry.Name())

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		var pod PodState
		if err := json.Unmarshal(data, &pod); err != nil {
			return err
		}

		pods = append(pods, pod)
	}

	sort.Slice(pods, func(i, j int) bool {
		return pods[i].Name < pods[j].Name
	})

	for _, pod := range pods {
		fmt.Printf("%s\t%s\t%s\n", pod.Name, pod.ContainerName, pod.UpdatedAt)
	}

	return nil
}

func podApplyCmd(args []string) error {
	if len(args) != 2 || args[0] != "-f" {
		return fmt.Errorf("usage: minicontainer pod apply -f <file>")
	}

	return applyPodFile(args[1])
}

func podDeleteCmd(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: minicontainer pod delete <name>")
	}
	return deletePod(args[0])
}

func applyPodFile(path string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("this command must be run as root")
	}

	absManifest, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve manifest path: %w", err)
	}

	pod, err := loadPodManifest(absManifest)
	if err != nil {
		return err
	}

	rootfs, cmd, err := resolvePodRootfsAndCommand(pod)
	if err != nil {
		return err
	}

	ports := make([]container.Port, 0, len(pod.Spec.Ports))
	for _, p := range pod.Spec.Ports {
		ports = append(ports, container.Port{
			HostPort:      p.HostPort,
			ContainerPort: p.ContainerPort,
		})
	}

	oldState, oldErr := loadPodState(pod.Metadata.Name)
	if oldErr == nil {
		_ = container.Stop(oldState.ContainerName)
		_ = container.Remove(oldState.ContainerName)
	}

	if err := container.Create(pod.Metadata.Name, rootfs, cmd, nil, nil, ports); err != nil {
		return fmt.Errorf("create pod container: %w", err)
	}

	if err := container.Start(pod.Metadata.Name); err != nil {
		_ = container.Remove(pod.Metadata.Name)
		return fmt.Errorf("start pod container: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	createdAt := now
	if oldErr == nil {
		createdAt = oldState.CreatedAt
	}

	state := &PodState{
		Name:          pod.Metadata.Name,
		ContainerName: pod.Metadata.Name,
		ManifestPath:  absManifest,
		CreatedAt:     createdAt,
		UpdatedAt:     now,
	}

	if err := savePodState(state); err != nil {
		return fmt.Errorf("save pod state: %w", err)
	}

	fmt.Printf("pod/%s applied\n", pod.Metadata.Name)
	return nil
}

func deletePod(name string) error {
	st, err := loadPodState(name)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("pod %q not found", name)
		}
		return fmt.Errorf("load pod state: %w", err)
	}

	containerName := st.ContainerName
	if containerName == "" {
		containerName = name
	}

	if err := container.Stop(containerName); err != nil && !isIgnorableStopError(err) {
		return fmt.Errorf("stop pod container %q: %w", containerName, err)
	}

	if err := container.Remove(containerName); err != nil {
		return fmt.Errorf("remove pod container %q: %w", containerName, err)
	}

	if err := os.Remove(podStatePath(name)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove pod state: %w", err)
	}

	fmt.Printf("pod/%s deleted\n", name)
	return nil
}

func isIgnorableStopError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already stopped") ||
		strings.Contains(msg, "not running") ||
		strings.Contains(msg, "is not running")
}

func loadPodManifest(path string) (*PodManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pod manifest: %w", err)
	}

	var pod PodManifest
	if err := yaml.Unmarshal(data, &pod); err != nil {
		return nil, fmt.Errorf("parse pod manifest: %w", err)
	}

	if err := validatePodManifest(&pod); err != nil {
		return nil, err
	}

	return &pod, nil
}

func validatePodManifest(p *PodManifest) error {
	if p.APIVersion != "v1" {
		return fmt.Errorf("unsupported apiVersion %q: only v1 is supported", p.APIVersion)
	}
	if p.Kind != "Pod" {
		return fmt.Errorf("unsupported kind %q: only Pod is supported", p.Kind)
	}
	if p.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if p.Spec.Image == "" {
		return fmt.Errorf("spec.image is required")
	}

	for i, port := range p.Spec.Ports {
		if port.ContainerPort <= 0 || port.ContainerPort > 65535 {
			return fmt.Errorf("spec.ports[%d].containerPort must be 1-65535", i)
		}
		if port.HostPort <= 0 || port.HostPort > 65535 {
			return fmt.Errorf("spec.ports[%d].hostPort must be 1-65535", i)
		}
	}

	return nil
}

func resolvePodRootfsAndCommand(pod *PodManifest) (string, []string, error) {
	rootfs, err := container.ResolveRootfs(pod.Spec.Image)
	if err != nil {
		return "", nil, err
	}

	if len(pod.Spec.Command) > 0 {
		return rootfs, append([]string(nil), pod.Spec.Command...), nil
	}

	img, err := container.GetImage(pod.Spec.Image)
	if err != nil {
		return "", nil, fmt.Errorf("load image metadata for default command: %w", err)
	}

	cmd := make([]string, 0, len(img.Entrypoint)+len(img.Cmd))
	cmd = append(cmd, img.Entrypoint...)
	cmd = append(cmd, img.Cmd...)

	if len(cmd) == 0 {
		return "", nil, fmt.Errorf("pod spec.command is empty and image %q has no entrypoint/cmd metadata", pod.Spec.Image)
	}

	return rootfs, cmd, nil
}

func podStatePath(name string) string {
	return filepath.Join(podStateRoot, name+".json")
}

func loadPodState(name string) (*PodState, error) {
	data, err := os.ReadFile(podStatePath(name))
	if err != nil {
		return nil, err
	}

	var st PodState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse pod state: %w", err)
	}

	return &st, nil
}

func savePodState(st *PodState) error {
	if err := os.MkdirAll(podStateRoot, 0o755); err != nil {
		return fmt.Errorf("create pod state dir: %w", err)
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pod state: %w", err)
	}

	if err := os.WriteFile(podStatePath(st.Name), data, 0o644); err != nil {
		return fmt.Errorf("write pod state: %w", err)
	}

	return nil
}
