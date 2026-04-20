package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

const serviceStateDir = "/var/lib/minicontainer/services"

type ServiceManifest struct {
	APIVersion string          `yaml:"apiVersion"`
	Kind       string          `yaml:"kind"`
	Metadata   ServiceMetadata `yaml:"metadata"`
	Spec       ServiceSpec     `yaml:"spec"`
}

type ServiceMetadata struct {
	Name string `yaml:"name"`
}

type ServiceSpec struct {
	Deployment string `yaml:"deployment"`
	Port       int    `yaml:"port"`
	TargetPort int    `yaml:"targetPort"`
}

type ServiceState struct {
	Name       string `json:"name"`
	Deployment string `json:"deployment"`
	Port       int    `json:"port"`
	TargetPort int    `json:"target_port"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type ServiceBackend struct {
	Replica string `json:"replica"`
	IP      string `json:"ip"`
	Port    int    `json:"port"`
}

type serviceContainerState struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	IP     string `json:"ip"`
}

func serviceCmd(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: minicontainer service apply -f <file>\n       minicontainer service get\n       minicontainer service delete <name>")
	}

	switch args[0] {
	case "apply":
		return serviceApply(args[1:])
	case "get":
		return serviceGet(args[1:])
	case "delete":
		return serviceDelete(args[1:])
	default:
		return fmt.Errorf("unknown service subcommand: %s", args[0])
	}
}

func serviceApply(args []string) error {
	if len(args) != 2 || args[0] != "-f" {
		return fmt.Errorf("usage: minicontainer service apply -f <file>")
	}

	manifestPath, err := filepath.Abs(args[1])
	if err != nil {
		return fmt.Errorf("resolve service manifest path: %w", err)
	}

	manifest, err := loadServiceManifest(manifestPath)
	if err != nil {
		return err
	}

	if _, err := loadDeploymentState(manifest.Spec.Deployment); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("deployment %q not found", manifest.Spec.Deployment)
		}
		return fmt.Errorf("load deployment %q: %w", manifest.Spec.Deployment, err)
	}

	services, err := listServiceStates()
	if err != nil {
		return err
	}
	for _, existing := range services {
		if existing.Name != manifest.Metadata.Name && existing.Port == manifest.Spec.Port {
			return fmt.Errorf("service port %d already in use by %q", manifest.Spec.Port, existing.Name)
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	createdAt := now
	if old, err := loadServiceState(manifest.Metadata.Name); err == nil {
		createdAt = old.CreatedAt
	}

	st := &ServiceState{
		Name:       manifest.Metadata.Name,
		Deployment: manifest.Spec.Deployment,
		Port:       manifest.Spec.Port,
		TargetPort: manifest.Spec.TargetPort,
		CreatedAt:  createdAt,
		UpdatedAt:  now,
	}

	if err := saveServiceState(st); err != nil {
		return err
	}

	fmt.Printf("service/%s applied\n", st.Name)
	return nil
}

func serviceGet(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: minicontainer service get")
	}

	services, err := listServiceStates()
	if err != nil {
		return err
	}

	for _, st := range services {
		fmt.Printf("%s\t%s\t%d\t%d\t%s\n", st.Name, st.Deployment, st.Port, st.TargetPort, st.UpdatedAt)
	}

	return nil
}

func serviceDelete(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: minicontainer service delete <name>")
	}

	name := args[0]
	if _, err := loadServiceState(name); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("service %q not found", name)
		}
		return err
	}

	if err := os.Remove(serviceStatePath(name)); err != nil && !os.IsNotExist(err) {
		return err
	}

	fmt.Printf("service/%s deleted\n", name)
	return nil
}

func loadServiceManifest(path string) (*ServiceManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read service manifest: %w", err)
	}

	var s ServiceManifest
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse service manifest: %w", err)
	}

	if err := validateServiceManifest(&s); err != nil {
		return nil, err
	}

	return &s, nil
}

func validateServiceManifest(s *ServiceManifest) error {
	if s.APIVersion != "v1" {
		return fmt.Errorf("unsupported apiVersion %q: only v1 is supported", s.APIVersion)
	}
	if s.Kind != "Service" {
		return fmt.Errorf("unsupported kind %q: only Service is supported", s.Kind)
	}
	if s.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if s.Spec.Deployment == "" {
		return fmt.Errorf("spec.deployment is required")
	}
	if s.Spec.Port <= 0 || s.Spec.Port > 65535 {
		return fmt.Errorf("spec.port must be 1-65535")
	}
	if s.Spec.TargetPort <= 0 || s.Spec.TargetPort > 65535 {
		return fmt.Errorf("spec.targetPort must be 1-65535")
	}
	return nil
}

func serviceStatePath(name string) string {
	return filepath.Join(serviceStateDir, name+".json")
}

func saveServiceState(st *ServiceState) error {
	if err := os.MkdirAll(serviceStateDir, 0o755); err != nil {
		return fmt.Errorf("create service state dir: %w", err)
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal service state: %w", err)
	}

	if err := os.WriteFile(serviceStatePath(st.Name), data, 0o644); err != nil {
		return fmt.Errorf("write service state: %w", err)
	}

	return nil
}

func loadServiceState(name string) (*ServiceState, error) {
	data, err := os.ReadFile(serviceStatePath(name))
	if err != nil {
		return nil, err
	}

	var st ServiceState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse service state: %w", err)
	}

	return &st, nil
}

func listServiceStates() ([]ServiceState, error) {
	entries, err := os.ReadDir(serviceStateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read service state dir: %w", err)
	}

	var out []ServiceState
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(serviceStateDir, entry.Name()))
		if err != nil {
			return nil, err
		}

		var st ServiceState
		if err := json.Unmarshal(data, &st); err != nil {
			return nil, err
		}
		out = append(out, st)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})

	return out, nil
}

func resolveServiceBackends(service *ServiceState) ([]ServiceBackend, error) {
	deploy, err := loadDeploymentState(service.Deployment)
	if err != nil {
		return nil, err
	}

	var backends []ServiceBackend
	for i := 0; i < deploy.Replicas; i++ {
		replica := deploymentReplicaName(deploy.Name, i)

		podState, err := loadPodState(replica)
		if err != nil {
			continue
		}

		containerName := podState.ContainerName
		if containerName == "" {
			containerName = replica
		}

		cs, err := loadServiceContainerState(containerName)
		if err != nil {
			continue
		}
		if cs.Status != "running" {
			continue
		}
		if cs.IP == "" || net.ParseIP(cs.IP) == nil {
			continue
		}

		backends = append(backends, ServiceBackend{
			Replica: replica,
			IP:      cs.IP,
			Port:    service.TargetPort,
		})
	}

	return backends, nil
}

func loadServiceContainerState(name string) (*serviceContainerState, error) {
	statePath := filepath.Join("/var/lib/minicontainer/containers", name, "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, err
	}

	var st serviceContainerState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}

	return &st, nil
}
