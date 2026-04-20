package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"github.com/vthecar/minicontainer/pkg/container"
	"gopkg.in/yaml.v3"
)

const deploymentStateDir = "/var/lib/minicontainer/deployments"

type DeploymentManifest struct {
	APIVersion string                 `yaml:"apiVersion"`
	Kind       string                 `yaml:"kind"`
	Metadata   DeploymentMetadata     `yaml:"metadata"`
	Spec       DeploymentManifestSpec `yaml:"spec"`
}

type DeploymentMetadata struct {
	Name string `yaml:"name"`
}

type DeploymentManifestSpec struct {
	Replicas int                   `yaml:"replicas"`
	Template DeploymentPodTemplate `yaml:"template"`
}

type DeploymentPodTemplate struct {
	Image   string    `yaml:"image"`
	Command []string  `yaml:"command"`
	Ports   []PodPort `yaml:"ports"`
}

type DeploymentState struct {
	Name         string                `json:"name"`
	Replicas     int                   `json:"replicas"`
	Pods         []string              `json:"pods"`
	Template     DeploymentPodTemplate `json:"template"`
	ManifestPath string                `json:"manifest_path"`
	CreatedAt    string                `json:"created_at"`
	UpdatedAt    string                `json:"updated_at"`
}

type localContainerState struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func deployCmd(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: minicontainer deploy apply -f <file>\n       minicontainer deploy get\n       minicontainer deploy delete <name>\n       minicontainer deploy reconcile <name>\n       minicontainer deploy reconcile-all")
	}

	switch args[0] {
	case "apply":
		return deployApply(args[1:])
	case "get":
		return deployGet(args[1:])
	case "delete":
		return deployDelete(args[1:])
	case "reconcile":
		return deployReconcile(args[1:])
	case "reconcile-all":
		return deployReconcileAll(args[1:])
	default:
		return fmt.Errorf("unknown deploy subcommand: %s", args[0])
	}
}

func deployApply(args []string) error {
	if len(args) != 2 || args[0] != "-f" {
		return fmt.Errorf("usage: minicontainer deploy apply -f <file>")
	}

	manifestPath, err := filepath.Abs(args[1])
	if err != nil {
		return fmt.Errorf("resolve deployment manifest path: %w", err)
	}

	manifest, err := loadDeploymentManifest(manifestPath)
	if err != nil {
		return err
	}

	old, err := loadDeploymentState(manifest.Metadata.Name)
	if err != nil {
		if os.IsNotExist(err) {
			return fullReplaceDeployment(manifest, manifestPath)
		}
		return fmt.Errorf("load deployment state: %w", err)
	}

	if !reflect.DeepEqual(old.Template, manifest.Spec.Template) {
		return fullReplaceDeployment(manifest, manifestPath)
	}

	return scaleDeployment(old, manifest, manifestPath)
}

func deployGet(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: minicontainer deploy get")
	}

	entries, err := os.ReadDir(deploymentStateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read deployment state dir: %w", err)
	}

	var items []DeploymentState

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(deploymentStateDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read deployment state %q: %w", entry.Name(), err)
		}

		var st DeploymentState
		if err := json.Unmarshal(data, &st); err != nil {
			return fmt.Errorf("parse deployment state %q: %w", entry.Name(), err)
		}

		items = append(items, st)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})

	for _, st := range items {
		fmt.Printf("%s\t%d\t%s\n", st.Name, st.Replicas, st.UpdatedAt)
	}

	return nil
}

func deployDelete(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: minicontainer deploy delete <name>")
	}

	name := args[0]

	st, err := loadDeploymentState(name)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("deployment %q not found", name)
		}
		return fmt.Errorf("load deployment state: %w", err)
	}

	for _, podName := range st.Pods {
		if err := deletePod(podName); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("delete replica %q: %w", podName, err)
		}
	}

	if err := os.Remove(deploymentStatePath(name)); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("remove deployment state: %w", err)
		}
	}

	fmt.Printf("deployment/%s deleted\n", name)
	return nil
}

func deployReconcile(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: minicontainer deploy reconcile <name>")
	}
	return reconcileDeploymentByName(args[0])
}

func deployReconcileAll(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: minicontainer deploy reconcile-all")
	}

	entries, err := os.ReadDir(deploymentStateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read deployment state dir: %w", err)
	}

	var names []string

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(deploymentStateDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read deployment state %q: %w", entry.Name(), err)
		}

		var st DeploymentState
		if err := json.Unmarshal(data, &st); err != nil {
			return fmt.Errorf("parse deployment state %q: %w", entry.Name(), err)
		}
		if st.Name != "" {
			names = append(names, st.Name)
		}
	}

	sort.Strings(names)

	for _, name := range names {
		if err := reconcileDeploymentByName(name); err != nil {
			return fmt.Errorf("reconcile deployment %q: %w", name, err)
		}
	}

	return nil
}

func loadDeploymentManifest(path string) (*DeploymentManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read deployment manifest: %w", err)
	}

	var d DeploymentManifest
	if err := yaml.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("parse deployment manifest: %w", err)
	}

	if err := validateDeploymentManifest(&d); err != nil {
		return nil, err
	}

	return &d, nil
}

func validateDeploymentManifest(d *DeploymentManifest) error {
	if d.APIVersion != "v1" {
		return fmt.Errorf("unsupported apiVersion %q: only v1 is supported", d.APIVersion)
	}
	if d.Kind != "Deployment" {
		return fmt.Errorf("unsupported kind %q: only Deployment is supported", d.Kind)
	}
	if d.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if d.Spec.Replicas < 0 {
		return fmt.Errorf("spec.replicas must be >= 0")
	}
	if d.Spec.Template.Image == "" {
		return fmt.Errorf("spec.template.image is required")
	}

	for i, port := range d.Spec.Template.Ports {
		if port.ContainerPort <= 0 || port.ContainerPort > 65535 {
			return fmt.Errorf("spec.template.ports[%d].containerPort must be 1-65535", i)
		}
		if port.HostPort < 0 || port.HostPort > 65535 {
			return fmt.Errorf("spec.template.ports[%d].hostPort must be 0-65535", i)
		}
	}

	if d.Spec.Replicas > 1 {
		for _, p := range d.Spec.Template.Ports {
			if p.HostPort != 0 {
				return fmt.Errorf("hostPort is not allowed when replicas > 1")
			}
		}
	}

	return nil
}

func deploymentStatePath(name string) string {
	return filepath.Join(deploymentStateDir, name+".json")
}

func saveDeploymentState(st *DeploymentState) error {
	if err := os.MkdirAll(deploymentStateDir, 0o755); err != nil {
		return fmt.Errorf("create deployment state dir: %w", err)
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal deployment state: %w", err)
	}

	if err := os.WriteFile(deploymentStatePath(st.Name), data, 0o644); err != nil {
		return fmt.Errorf("write deployment state: %w", err)
	}

	return nil
}

func loadDeploymentState(name string) (*DeploymentState, error) {
	data, err := os.ReadFile(deploymentStatePath(name))
	if err != nil {
		return nil, err
	}

	var st DeploymentState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse deployment state: %w", err)
	}

	return &st, nil
}

func deploymentReplicaName(name string, idx int) string {
	return fmt.Sprintf("%s-%d", name, idx)
}

func fullReplaceDeployment(manifest *DeploymentManifest, manifestPath string) error {
	name := manifest.Metadata.Name

	if old, err := loadDeploymentState(name); err == nil {
		for _, podName := range old.Pods {
			_ = deletePod(podName)
		}
		_ = os.Remove(deploymentStatePath(name))
	}

	podNames := make([]string, 0, manifest.Spec.Replicas)
	for i := 0; i < manifest.Spec.Replicas; i++ {
		replicaName := deploymentReplicaName(name, i)
		if err := createDeploymentReplica(replicaName, manifest.Spec.Template, manifestPath); err != nil {
			return err
		}
		podNames = append(podNames, replicaName)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	state := &DeploymentState{
		Name:         name,
		Replicas:     manifest.Spec.Replicas,
		Pods:         podNames,
		Template:     manifest.Spec.Template,
		ManifestPath: manifestPath,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := saveDeploymentState(state); err != nil {
		return fmt.Errorf("save deployment state: %w", err)
	}

	fmt.Printf("deployment/%s applied\n", name)
	return nil
}

func scaleDeployment(old *DeploymentState, manifest *DeploymentManifest, manifestPath string) error {
	name := manifest.Metadata.Name
	oldReplicas := old.Replicas
	newReplicas := manifest.Spec.Replicas

	if newReplicas > oldReplicas {
		for i := oldReplicas; i < newReplicas; i++ {
			replicaName := deploymentReplicaName(name, i)
			if err := createDeploymentReplica(replicaName, manifest.Spec.Template, manifestPath); err != nil {
				return err
			}
		}
	} else if newReplicas < oldReplicas {
		for i := oldReplicas - 1; i >= newReplicas; i-- {
			replicaName := deploymentReplicaName(name, i)
			if err := deletePod(replicaName); err != nil {
				return fmt.Errorf("delete replica %q: %w", replicaName, err)
			}
		}
	}

	podNames := make([]string, 0, newReplicas)
	for i := 0; i < newReplicas; i++ {
		podNames = append(podNames, deploymentReplicaName(name, i))
	}

	createdAt := old.CreatedAt
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	}

	state := &DeploymentState{
		Name:         name,
		Replicas:     newReplicas,
		Pods:         podNames,
		Template:     manifest.Spec.Template,
		ManifestPath: manifestPath,
		CreatedAt:    createdAt,
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	if err := saveDeploymentState(state); err != nil {
		return fmt.Errorf("save deployment state: %w", err)
	}

	fmt.Printf("deployment/%s applied\n", name)
	return nil
}

func createDeploymentReplica(replicaName string, template DeploymentPodTemplate, manifestPath string) error {
	podManifest := &PodManifest{
		APIVersion: "v1",
		Kind:       "Pod",
		Metadata: PodMetadata{
			Name: replicaName,
		},
		Spec: PodSpec{
			Image:   template.Image,
			Command: template.Command,
			Ports:   template.Ports,
		},
	}

	rootfs, cmd, err := resolvePodRootfsAndCommand(podManifest)
	if err != nil {
		return fmt.Errorf("resolve replica %q: %w", replicaName, err)
	}

	ports := make([]container.Port, 0, len(podManifest.Spec.Ports))
	for _, p := range podManifest.Spec.Ports {
		ports = append(ports, container.Port{
			HostPort:      p.HostPort,
			ContainerPort: p.ContainerPort,
		})
	}

	_ = container.Stop(replicaName)
	_ = container.Remove(replicaName)

	if err := container.Create(replicaName, rootfs, cmd, nil, nil, ports); err != nil {
		return fmt.Errorf("create replica %q: %w", replicaName, err)
	}

	if err := container.Start(replicaName); err != nil {
		_ = container.Remove(replicaName)
		return fmt.Errorf("start replica %q: %w", replicaName, err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	podState := &PodState{
		Name:          replicaName,
		ContainerName: replicaName,
		ManifestPath:  manifestPath,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := savePodState(podState); err != nil {
		return fmt.Errorf("save replica pod state %q: %w", replicaName, err)
	}

	return nil
}

func replicaNeedsRecreate(replicaName string) (bool, error) {
	podState, err := loadPodState(replicaName)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}

	containerName := podState.ContainerName
	if containerName == "" {
		containerName = replicaName
	}

	statePath := filepath.Join("/var/lib/minicontainer/containers", containerName, "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}

	var st localContainerState
	if err := json.Unmarshal(data, &st); err != nil {
		return false, err
	}

	if st.Status != "running" {
		return true, nil
	}

	return false, nil
}

func reconcileDeploymentByName(name string) error {
	st, err := loadDeploymentState(name)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("deployment %q not found", name)
		}
		return fmt.Errorf("load deployment state: %w", err)
	}

	recreated := 0
	healthy := 0

	for i := 0; i < st.Replicas; i++ {
		replicaName := deploymentReplicaName(name, i)

		needsRecreate, err := replicaNeedsRecreate(replicaName)
		if err != nil {
			return fmt.Errorf("check replica %q: %w", replicaName, err)
		}

		if !needsRecreate {
			healthy++
			continue
		}

		_ = deletePod(replicaName)

		if err := createDeploymentReplica(replicaName, st.Template, st.ManifestPath); err != nil {
			return fmt.Errorf("recreate replica %q: %w", replicaName, err)
		}

		recreated++
	}

	st.Pods = make([]string, 0, st.Replicas)
	for i := 0; i < st.Replicas; i++ {
		st.Pods = append(st.Pods, deploymentReplicaName(name, i))
	}
	st.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := saveDeploymentState(st); err != nil {
		return fmt.Errorf("save deployment state: %w", err)
	}

	fmt.Printf("deployment/%s reconciled: recreated=%d healthy=%d\n", name, recreated, healthy)
	return nil
}
