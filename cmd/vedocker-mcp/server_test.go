package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mockDaemon implements the subset of the minicontainerd HTTP API that the
// MCP server uses, with the same paths, methods and JSON shapes as
// cmd/minicontainerd/main.go.
type mockDaemon struct {
	t *testing.T

	mu         sync.Mutex
	containers map[string]containerInfo
	logs       map[string]map[string]string
	images     []imageInfo
	deployReqs []string
	// release, when set, blocks deploys until it is closed.
	release chan struct{}
}

func newMockDaemon(t *testing.T) (*mockDaemon, *httptest.Server) {
	m := &mockDaemon{
		t: t,
		containers: map[string]containerInfo{
			"web": {ID: "web", Status: "running", PID: 42, IP: "10.0.0.2", Ports: []portMapping{{HostPort: 8080, ContainerPort: 8080}}, Command: []string{"python3", "-m", "http.server"}},
			"old": {ID: "old", Status: "stopped"},
		},
		logs: map[string]map[string]string{
			"web": {"stdout": "line1\nline2\nline3\nline4\n", "stderr": "warn: something\n"},
		},
		images: []imageInfo{
			{Ref: "alpine:latest", Name: "alpine", Tag: "latest", Source: "registry"},
			{Ref: "hello:123", Name: "hello", Tag: "123", Source: "build"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(srv.Close)
	return m, srv
}

func (m *mockDaemon) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (m *mockDaemon) serve(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/containers" && r.Method == http.MethodGet:
		m.mu.Lock()
		list := []containerInfo{}
		for _, c := range m.containers {
			list = append(list, c)
		}
		m.mu.Unlock()
		m.writeJSON(w, 200, map[string]any{"containers": list})

	case path == "/images" && r.Method == http.MethodGet:
		m.writeJSON(w, 200, map[string]any{"images": m.images})

	case path == "/deployments/repo" && r.Method == http.MethodPost:
		var req struct {
			RepoURL      string `json:"repo_url"`
			GeminiAPIKey string `json:"gemini_api_key"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.GeminiAPIKey != "" {
			m.t.Errorf("MCP server must not send a Gemini key, got %q", req.GeminiAPIKey)
		}
		m.mu.Lock()
		m.deployReqs = append(m.deployReqs, req.RepoURL)
		release := m.release
		m.mu.Unlock()
		if release != nil {
			<-release
		}
		switch {
		case strings.HasSuffix(req.RepoURL, "/nodocker"):
			m.writeJSON(w, 200, deployRepoResult{OK: false, RepoURL: req.RepoURL, NeedsAI: true, Reason: "no Dockerfile found and no Gemini API key provided"})
		case strings.HasSuffix(req.RepoURL, "/broken"):
			m.writeJSON(w, 500, deployRepoResult{OK: false, RepoURL: req.RepoURL, Error: "build image: RUN exited 1"})
		default:
			id := "repo-1"
			m.mu.Lock()
			m.containers[id] = containerInfo{ID: id, Status: "running", IP: "10.0.0.9", Ports: []portMapping{{HostPort: 3000, ContainerPort: 3000}}}
			m.mu.Unlock()
			m.writeJSON(w, 200, deployRepoResult{OK: true, RepoURL: req.RepoURL, ImageRef: "repo:1", ContainerID: id, DockerfilePath: "/x/Dockerfile"})
		}

	case strings.HasPrefix(path, "/containers/"):
		parts := strings.Split(strings.Trim(strings.TrimPrefix(path, "/containers/"), "/"), "/")
		id := parts[0]
		m.mu.Lock()
		c, ok := m.containers[id]
		m.mu.Unlock()
		if !ok {
			m.writeJSON(w, 404, map[string]any{"error": "open /var/lib/minicontainer/containers/" + id + "/state.json: no such file or directory"})
			return
		}
		switch {
		case len(parts) == 1 && r.Method == http.MethodGet:
			m.writeJSON(w, 200, c)
		case len(parts) == 1 && r.Method == http.MethodDelete:
			m.mu.Lock()
			delete(m.containers, id)
			m.mu.Unlock()
			m.writeJSON(w, 200, map[string]any{"ok": true, "id": id})
		case len(parts) == 2 && parts[1] == "stop" && r.Method == http.MethodPost:
			m.mu.Lock()
			c.Status = "stopped"
			m.containers[id] = c
			m.mu.Unlock()
			m.writeJSON(w, 200, map[string]any{"ok": true, "id": id})
		case len(parts) == 2 && parts[1] == "logs" && r.Method == http.MethodGet:
			stream := r.URL.Query().Get("stream")
			logs, ok := m.logs[id][stream]
			if !ok {
				m.writeJSON(w, 404, map[string]any{"error": "log file not found"})
				return
			}
			m.writeJSON(w, 200, map[string]any{"id": id, "stream": stream, "logs": logs})
		default:
			m.writeJSON(w, 405, map[string]any{"error": "method not allowed"})
		}

	default:
		m.writeJSON(w, 404, map[string]any{"error": "endpoint not found"})
	}
}

func connect(t *testing.T, daemonURL string, deployWait time.Duration) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	daemon, err := newDaemonClient(daemonURL)
	if err != nil {
		t.Fatal(err)
	}
	server := newServer(ctx, serverConfig{daemon: daemon, deployWait: deployWait})

	serverT, clientT := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func call(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return res
}

func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func decode[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	var out T
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode structured content %s: %v", data, err)
	}
	return out
}

func TestListTools(t *testing.T) {
	_, srv := newMockDaemon(t)
	s := connect(t, srv.URL, time.Second)

	res, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"deploy_repo": false, "deploy_status": false, "list_containers": false, "get_logs": false,
		"stop_container": false, "remove_container": false, "list_images": false,
	}
	for _, tool := range res.Tools {
		if _, ok := want[tool.Name]; !ok {
			t.Errorf("unexpected tool %q", tool.Name)
			continue
		}
		want[tool.Name] = true
		if len(tool.Description) < 40 {
			t.Errorf("tool %q needs a fuller description, got %q", tool.Name, tool.Description)
		}
		if tool.Name == "deploy_repo" && !strings.Contains(tool.Description, "explicit yes") {
			t.Errorf("deploy_repo description must tell the AI to confirm with the user")
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func TestDeployRepoSucceeds(t *testing.T) {
	m, srv := newMockDaemon(t)
	s := connect(t, srv.URL, 5*time.Second)

	res := call(t, s, "deploy_repo", map[string]any{"github_url": "github.com/owner/app/tree/main"})
	if res.IsError {
		t.Fatalf("deploy_repo failed: %s", text(res))
	}
	st := decode[deployStatus](t, res)
	if st.State != deploySucceeded || st.ContainerID != "repo-1" || st.RepoURL != "https://github.com/owner/app" {
		t.Fatalf("unexpected status %+v", st)
	}
	if st.Container == nil || st.Container.Status != "running" || len(st.Container.Ports) != 1 {
		t.Fatalf("expected live container info, got %+v", st.Container)
	}
	if got := m.deployReqs; len(got) != 1 || got[0] != "https://github.com/owner/app" {
		t.Fatalf("daemon got deploy requests %v", got)
	}
}

func TestDeployRepoRejectsNonGitHub(t *testing.T) {
	m, srv := newMockDaemon(t)
	s := connect(t, srv.URL, time.Second)

	for _, u := range []string{"https://gitlab.com/owner/repo", "https://github.com.evil.com/a/b", "owner/repo"} {
		res := call(t, s, "deploy_repo", map[string]any{"github_url": u})
		if !res.IsError {
			t.Errorf("deploy_repo(%q) should fail", u)
		}
	}
	if len(m.deployReqs) != 0 {
		t.Fatalf("daemon must not be called for rejected URLs, got %v", m.deployReqs)
	}
}

func TestDeployRepoAsyncAndStatus(t *testing.T) {
	m, srv := newMockDaemon(t)
	m.release = make(chan struct{})
	s := connect(t, srv.URL, 50*time.Millisecond)

	res := call(t, s, "deploy_repo", map[string]any{"github_url": "https://github.com/owner/slow"})
	st := decode[deployStatus](t, res)
	if res.IsError || st.State != deployRunning || st.DeployID == "" {
		t.Fatalf("expected a running deploy, got %+v (%s)", st, text(res))
	}

	// A second call for the same repo while it builds reuses the deploy.
	again := decode[deployStatus](t, call(t, s, "deploy_repo", map[string]any{"github_url": "https://github.com/owner/slow.git"}))
	if again.DeployID != st.DeployID {
		t.Fatalf("expected the in-flight deploy %q to be reused, got %q", st.DeployID, again.DeployID)
	}

	close(m.release)

	deadline := time.Now().Add(5 * time.Second)
	for {
		out := decode[deployStatusOutput](t, call(t, s, "deploy_status", map[string]any{"deploy_id": st.DeployID}))
		if len(out.Deploys) != 1 {
			t.Fatalf("expected one deploy, got %+v", out)
		}
		if out.Deploys[0].State == deploySucceeded {
			if out.Deploys[0].ContainerID != "repo-1" {
				t.Fatalf("unexpected status %+v", out.Deploys[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("deploy never finished: %+v", out.Deploys[0])
		}
		time.Sleep(20 * time.Millisecond)
	}

	all := decode[deployStatusOutput](t, call(t, s, "deploy_status", nil))
	if len(all.Deploys) != 1 {
		t.Fatalf("expected one tracked deploy, got %+v", all)
	}
	if len(m.deployReqs) != 1 {
		t.Fatalf("expected exactly one daemon deploy, got %v", m.deployReqs)
	}
}

func TestDeployStatusUnknownID(t *testing.T) {
	_, srv := newMockDaemon(t)
	s := connect(t, srv.URL, time.Second)

	if res := call(t, s, "deploy_status", map[string]any{"deploy_id": "deploy-99"}); !res.IsError {
		t.Fatal("expected an error for an unknown deploy id")
	}
	out := decode[deployStatusOutput](t, call(t, s, "deploy_status", nil))
	if out.Deploys == nil || len(out.Deploys) != 0 {
		t.Fatalf("expected an empty list, got %+v", out)
	}
}

func TestDeployRepoNeedsAIAndFailure(t *testing.T) {
	_, srv := newMockDaemon(t)
	s := connect(t, srv.URL, 5*time.Second)

	res := call(t, s, "deploy_repo", map[string]any{"github_url": "https://github.com/owner/nodocker"})
	if !res.IsError || !strings.Contains(text(res), "GEMINI_API_KEY") {
		t.Fatalf("expected needs_ai guidance, got %s", text(res))
	}

	res = call(t, s, "deploy_repo", map[string]any{"github_url": "https://github.com/owner/broken"})
	if !res.IsError || !strings.Contains(text(res), "RUN exited 1") {
		t.Fatalf("expected the daemon's build error, got %s", text(res))
	}
}

func TestListContainers(t *testing.T) {
	_, srv := newMockDaemon(t)
	s := connect(t, srv.URL, time.Second)

	out := decode[listContainersOutput](t, call(t, s, "list_containers", nil))
	if len(out.Containers) != 2 {
		t.Fatalf("expected 2 containers, got %+v", out)
	}
	for _, c := range out.Containers {
		if c.ID == "web" && (c.Status != "running" || c.IP != "10.0.0.2" || c.Ports[0].HostPort != 8080) {
			t.Fatalf("unexpected web container %+v", c)
		}
	}
}

func TestGetLogs(t *testing.T) {
	_, srv := newMockDaemon(t)
	s := connect(t, srv.URL, time.Second)

	res := call(t, s, "get_logs", map[string]any{"container": "web", "tail": 2})
	if res.IsError {
		t.Fatal(text(res))
	}
	out := decode[getLogsOutput](t, res)
	if out.Stdout != "line3\nline4\n" || out.Stderr != "warn: something\n" {
		t.Fatalf("unexpected logs %+v", out)
	}
	if !strings.Contains(text(res), "=== stdout") || !strings.Contains(text(res), "line4") {
		t.Fatalf("unexpected text %q", text(res))
	}

	out = decode[getLogsOutput](t, call(t, s, "get_logs", map[string]any{"container": "web", "stream": "stderr"}))
	if out.Stdout != "" || out.Stderr == "" || out.Tail != defaultLogTail {
		t.Fatalf("unexpected stderr-only logs %+v", out)
	}

	for _, bad := range []map[string]any{
		{"container": "../../etc"},
		{"container": "web", "stream": "all"},
		{"container": "missing"},
	} {
		if res := call(t, s, "get_logs", bad); !res.IsError {
			t.Errorf("get_logs(%v) should fail", bad)
		}
	}
}

func TestStopContainer(t *testing.T) {
	m, srv := newMockDaemon(t)
	s := connect(t, srv.URL, time.Second)

	res := call(t, s, "stop_container", map[string]any{"container": "web"})
	if res.IsError {
		t.Fatal(text(res))
	}
	if m.containers["web"].Status != "stopped" {
		t.Fatal("container was not stopped")
	}
	if res := call(t, s, "stop_container", map[string]any{"container": "nope"}); !res.IsError || !strings.Contains(text(res), "404") {
		t.Fatalf("expected a 404 error, got %s", text(res))
	}
}

func TestRemoveContainer(t *testing.T) {
	m, srv := newMockDaemon(t)
	s := connect(t, srv.URL, time.Second)

	if res := call(t, s, "remove_container", map[string]any{"container": "old"}); res.IsError {
		t.Fatal(text(res))
	}
	if _, ok := m.containers["old"]; ok {
		t.Fatal("container was not removed")
	}
	if res := call(t, s, "remove_container", map[string]any{"container": "a/b"}); !res.IsError {
		t.Fatal("expected invalid id to be rejected")
	}
}

func TestListImages(t *testing.T) {
	_, srv := newMockDaemon(t)
	s := connect(t, srv.URL, time.Second)

	out := decode[listImagesOutput](t, call(t, s, "list_images", nil))
	if len(out.Images) != 2 || out.Images[0].Ref != "alpine:latest" {
		t.Fatalf("unexpected images %+v", out)
	}
}

func TestDaemonDown(t *testing.T) {
	_, srv := newMockDaemon(t)
	url := srv.URL
	srv.Close()
	s := connect(t, url, time.Second)

	res := call(t, s, "list_containers", nil)
	if !res.IsError || !strings.Contains(text(res), "minicontainerd") {
		t.Fatalf("expected a helpful connection error, got %s", text(res))
	}
}
