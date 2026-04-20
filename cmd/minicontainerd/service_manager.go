package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DaemonServiceState struct {
	Name       string `json:"name"`
	Deployment string `json:"deployment"`
	Port       int    `json:"port"`
	TargetPort int    `json:"target_port"`
	UpdatedAt  string `json:"updated_at"`
	CreatedAt  string `json:"created_at"`
}

type DaemonDeploymentState struct {
	Name         string `json:"name"`
	Replicas     int    `json:"replicas"`
	UpdatedAt    string `json:"updated_at"`
	CreatedAt    string `json:"created_at"`
	ManifestPath string `json:"manifest_path"`
}

type DaemonPodState struct {
	Name          string `json:"name"`
	ContainerName string `json:"container_name"`
	ManifestPath  string `json:"manifest_path"`
	UpdatedAt     string `json:"updated_at"`
	CreatedAt     string `json:"created_at"`
}

type DaemonContainerState struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	IP     string `json:"ip"`
}

type ProxyBackend struct {
	Replica string `json:"replica"`
	IP      string `json:"ip"`
	Port    int    `json:"port"`
}

type backendRoute struct {
	Replica     string
	ContainerIP string
	TargetPort  int
	BackendPort int
}

type ServiceManager struct {
	mu        sync.Mutex
	listeners map[string]*ServiceListener
}

type ServiceListener struct {
	state         DaemonServiceState
	server        *http.Server
	cancel        context.CancelFunc
	next          int
	mu            sync.Mutex
	backendRoutes map[string]*backendRoute
}

const (
	serviceBackendPortStart = 20000
	serviceBackendPortEnd   = 29999
)

func NewServiceManager(_ string) *ServiceManager {
	return &ServiceManager{
		listeners: map[string]*ServiceListener{},
	}
}

func (m *ServiceManager) Sync() error {
	services, err := daemonListServices()
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	wanted := map[string]DaemonServiceState{}
	for _, svc := range services {
		wanted[svc.Name] = svc
	}

	for name, lst := range m.listeners {
		svc, ok := wanted[name]
		if !ok || svc.Port != lst.state.Port || svc.Deployment != lst.state.Deployment || svc.TargetPort != lst.state.TargetPort {
			lst.stop()
			delete(m.listeners, name)
		}
	}

	for _, svc := range services {
		if _, ok := m.listeners[svc.Name]; ok {
			continue
		}
		lst, err := m.startServiceListener(svc)
		if err != nil {
			return fmt.Errorf("start service %q: %w", svc.Name, err)
		}
		m.listeners[svc.Name] = lst
		fmt.Printf("service/%s listening on :%d\n", svc.Name, svc.Port)
	}

	for _, lst := range m.listeners {
		if err := m.syncServiceBackendsLocked(lst); err != nil {
			return fmt.Errorf("sync service backends for %q: %w", lst.state.Name, err)
		}
	}

	return nil
}

func (m *ServiceManager) startServiceListener(state DaemonServiceState) (*ServiceListener, error) {
	addr := fmt.Sprintf("0.0.0.0:%d", state.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	lst := &ServiceListener{
		state:         state,
		cancel:        cancel,
		backendRoutes: map[string]*backendRoute{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		route, err := lst.pickRoute()
		if err != nil {
			http.Error(w, "no healthy backends", http.StatusServiceUnavailable)
			return
		}

		targetURL := fmt.Sprintf("http://127.0.0.1:%d%s", route.BackendPort, r.URL.RequestURI())

		req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
		if err != nil {
			http.Error(w, "proxy request build failed", http.StatusBadGateway)
			return
		}
		req.Header = r.Header.Clone()

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, "backend request failed", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for k, vals := range resp.Header {
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})

	server := &http.Server{
		Handler: mux,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	lst.server = server

	go func() {
		_ = server.Serve(ln)
	}()

	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()

	return lst, nil
}

func (l *ServiceListener) pickRoute() (*backendRoute, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.backendRoutes) == 0 {
		return nil, errors.New("no healthy backends")
	}

	keys := make([]string, 0, len(l.backendRoutes))
	for replica := range l.backendRoutes {
		keys = append(keys, replica)
	}
	sort.Strings(keys)

	idx := l.next % len(keys)
	l.next++

	return l.backendRoutes[keys[idx]], nil
}

func (l *ServiceListener) stop() {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, route := range l.backendRoutes {
		_ = removeBackendPortRules(route.ContainerIP, route.TargetPort, route.BackendPort)
	}
	l.backendRoutes = map[string]*backendRoute{}

	if l.cancel != nil {
		l.cancel()
	}
	if l.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = l.server.Shutdown(ctx)
	}
}

func (m *ServiceManager) syncServiceBackendsLocked(lst *ServiceListener) error {
	healthy, err := daemonResolveServiceBackends(lst.state)
	if err != nil {
		return err
	}

	want := map[string]ProxyBackend{}
	for _, b := range healthy {
		want[b.Replica] = b
	}

	lst.mu.Lock()
	defer lst.mu.Unlock()

	for replica, route := range lst.backendRoutes {
		b, ok := want[replica]
		if !ok || b.IP != route.ContainerIP || b.Port != route.TargetPort {
			_ = removeBackendPortRules(route.ContainerIP, route.TargetPort, route.BackendPort)
			delete(lst.backendRoutes, replica)
		}
	}

	for _, b := range healthy {
		if _, ok := lst.backendRoutes[b.Replica]; ok {
			continue
		}

		backendPort, err := m.allocateBackendPortLocked()
		if err != nil {
			return err
		}

		if err := addBackendPortRules(b.IP, b.Port, backendPort); err != nil {
			return fmt.Errorf("add backend rules for %s: %w", b.Replica, err)
		}

		lst.backendRoutes[b.Replica] = &backendRoute{
			Replica:     b.Replica,
			ContainerIP: b.IP,
			TargetPort:  b.Port,
			BackendPort: backendPort,
		}
	}

	return nil
}

func (m *ServiceManager) allocateBackendPortLocked() (int, error) {
	used := map[int]bool{}

	existing, err := listUsedBackendPortsFromIPTables()
	if err != nil {
		return 0, err
	}
	for _, p := range existing {
		used[p] = true
	}

	for _, lst := range m.listeners {
		for _, route := range lst.backendRoutes {
			used[route.BackendPort] = true
		}
	}

	for p := serviceBackendPortStart; p <= serviceBackendPortEnd; p++ {
		if used[p] {
			continue
		}
		return p, nil
	}

	return 0, fmt.Errorf("no free service backend ports available")
}

func daemonListServices() ([]DaemonServiceState, error) {
	dir := "/var/lib/minicontainer/services"
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []DaemonServiceState
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var st DaemonServiceState
		if err := json.Unmarshal(data, &st); err != nil {
			return nil, err
		}
		out = append(out, st)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func daemonLoadDeployment(name string) (*DaemonDeploymentState, error) {
	data, err := os.ReadFile(filepath.Join("/var/lib/minicontainer/deployments", name+".json"))
	if err != nil {
		return nil, err
	}
	var st DaemonDeploymentState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func daemonLoadPod(name string) (*DaemonPodState, error) {
	data, err := os.ReadFile(filepath.Join("/var/lib/minicontainer/pods", name+".json"))
	if err != nil {
		return nil, err
	}
	var st DaemonPodState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func daemonLoadContainer(name string) (*DaemonContainerState, error) {
	data, err := os.ReadFile(filepath.Join("/var/lib/minicontainer/containers", name, "state.json"))
	if err != nil {
		return nil, err
	}
	var st DaemonContainerState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func daemonResolveServiceBackends(service DaemonServiceState) ([]ProxyBackend, error) {
	deploy, err := daemonLoadDeployment(service.Deployment)
	if err != nil {
		return nil, err
	}

	var out []ProxyBackend
	for i := 0; i < deploy.Replicas; i++ {
		replica := fmt.Sprintf("%s-%d", deploy.Name, i)

		pod, err := daemonLoadPod(replica)
		if err != nil {
			continue
		}
		containerName := pod.ContainerName
		if containerName == "" {
			containerName = replica
		}

		cs, err := daemonLoadContainer(containerName)
		if err != nil {
			continue
		}
		if cs.Status != "running" || cs.IP == "" {
			continue
		}

		out = append(out, ProxyBackend{
			Replica: replica,
			IP:      cs.IP,
			Port:    service.TargetPort,
		})
	}

	return out, nil
}

func syncServicesPeriodically(sm *ServiceManager, interval time.Duration, logger func(string, ...any)) {
	t := time.NewTicker(interval)
	defer t.Stop()

	for range t.C {
		if err := sm.Sync(); err != nil && logger != nil {
			logger("service sync failed: %v", err)
		}
	}
}

func findServiceByName(name string) (*DaemonServiceState, error) {
	services, err := daemonListServices()
	if err != nil {
		return nil, err
	}
	for _, s := range services {
		if s.Name == name {
			return &s, nil
		}
	}
	return nil, errors.New("service not found")
}

func runIptables(args ...string) error {
	cmd := exec.Command("iptables", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables %v failed: %v: %s", args, err, string(out))
	}
	return nil
}

func runIptablesIgnoreMissing(args ...string) error {
	cmd := exec.Command("iptables", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		s := string(out)
		if strings.Contains(s, "Bad rule") ||
			strings.Contains(s, "No chain/target/match by that name") ||
			strings.Contains(s, "does a matching rule exist") {
			return nil
		}
		return fmt.Errorf("iptables %v failed: %v: %s", args, err, s)
	}
	return nil
}

func addBackendPortRules(containerIP string, targetPort int, backendPort int) error {
	targetPortStr := strconv.Itoa(targetPort)
	backendPortStr := strconv.Itoa(backendPort)
	target := fmt.Sprintf("%s:%d", containerIP, targetPort)
	if err := clearOutputRulesForBackendPort(backendPort); err != nil {
		return err
	}
	if err := runIptables(
		"-t", "nat",
		"-A", "OUTPUT",
		"-p", "tcp",
		"-d", "127.0.0.1",
		"--dport", backendPortStr,
		"-j", "DNAT",
		"--to-destination", target,
	); err != nil {
		return err
	}

	if err := runIptables(
		"-A", "FORWARD",
		"-p", "tcp",
		"-d", containerIP,
		"--dport", targetPortStr,
		"-j", "ACCEPT",
	); err != nil {
		_ = removeBackendPortRules(containerIP, targetPort, backendPort)
		return err
	}

	if err := runIptables(
		"-t", "nat",
		"-A", "POSTROUTING",
		"-p", "tcp",
		"-d", containerIP,
		"--dport", targetPortStr,
		"-j", "MASQUERADE",
	); err != nil {
		_ = removeBackendPortRules(containerIP, targetPort, backendPort)
		return err
	}

	return nil
}

func removeBackendPortRules(containerIP string, targetPort int, backendPort int) error {
	var firstErr error

	targetPortStr := strconv.Itoa(targetPort)
	backendPortStr := strconv.Itoa(backendPort)
	target := fmt.Sprintf("%s:%d", containerIP, targetPort)

	if err := runIptablesIgnoreMissing(
		"-t", "nat",
		"-D", "POSTROUTING",
		"-p", "tcp",
		"-d", containerIP,
		"--dport", targetPortStr,
		"-j", "MASQUERADE",
	); err != nil && firstErr == nil {
		firstErr = err
	}

	if err := runIptablesIgnoreMissing(
		"-D", "FORWARD",
		"-p", "tcp",
		"-d", containerIP,
		"--dport", targetPortStr,
		"-j", "ACCEPT",
	); err != nil && firstErr == nil {
		firstErr = err
	}

	if err := runIptablesIgnoreMissing(
		"-t", "nat",
		"-D", "OUTPUT",
		"-p", "tcp",
		"-d", "127.0.0.1",
		"--dport", backendPortStr,
		"-j", "DNAT",
		"--to-destination", target,
	); err != nil && firstErr == nil {
		firstErr = err
	}

	return firstErr
}

func listUsedBackendPortsFromIPTables() ([]int, error) {
	cmd := exec.Command("iptables", "-t", "nat", "-S", "OUTPUT")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("iptables -t nat -S OUTPUT failed: %v: %s", err, string(out))
	}

	var ports []int
	seen := map[int]bool{}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.Contains(line, "-A OUTPUT ") {
			continue
		}
		if !strings.Contains(line, "-d 127.0.0.1/32") {
			continue
		}
		if !strings.Contains(line, "-j DNAT") {
			continue
		}

		fields := strings.Fields(line)
		for i := 0; i < len(fields)-1; i++ {
			if fields[i] == "--dport" {
				p, err := strconv.Atoi(fields[i+1])
				if err != nil {
					continue
				}
				if p >= serviceBackendPortStart && p <= serviceBackendPortEnd && !seen[p] {
					seen[p] = true
					ports = append(ports, p)
				}
			}
		}
	}

	sort.Ints(ports)
	return ports, nil
}

func clearOutputRulesForBackendPort(backendPort int) error {
	cmd := exec.Command("iptables", "-t", "nat", "-S", "OUTPUT")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables -t nat -S OUTPUT failed: %v: %s", err, string(out))
	}

	backendPortStr := strconv.Itoa(backendPort)
	lines := strings.Split(string(out), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.Contains(line, "-A OUTPUT ") {
			continue
		}
		if !strings.Contains(line, "-d 127.0.0.1/32") {
			continue
		}
		if !strings.Contains(line, "--dport "+backendPortStr) {
			continue
		}
		if !strings.Contains(line, "-j DNAT") {
			continue
		}

		deleteRule := strings.Replace(line, "-A OUTPUT", "-D OUTPUT", 1)
		args := strings.Fields(deleteRule)
		args = append([]string{"-t", "nat"}, args...)
		if err := runIptablesIgnoreMissing(args...); err != nil {
			return err
		}
	}

	return nil
}
