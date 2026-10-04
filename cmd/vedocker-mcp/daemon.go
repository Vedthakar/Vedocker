package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const defaultDaemonURL = "http://127.0.0.1:18080"

// Container IDs end up in file paths on the daemon side, so only allow the
// characters the daemon itself generates.
var containerIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// daemonClient talks to the minicontainerd HTTP API. Every method maps to an
// endpoint registered in cmd/minicontainerd/main.go.
type daemonClient struct {
	base *url.URL
	http *http.Client
}

// newDaemonClient refuses any daemon address that is not loopback. The daemon
// runs repos as root and has no auth, so it must never be reached over a
// network. For a remote Linux box, forward the port with an SSH tunnel.
func newDaemonClient(rawURL string) (*daemonClient, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(rawURL), "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid daemon URL %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("daemon URL must be http://127.0.0.1:<port>, got %q", rawURL)
	}
	if !isLoopbackHost(u.Hostname()) {
		return nil, fmt.Errorf("daemon URL %q is not loopback; Vedocker only talks to a daemon on localhost (use an SSH tunnel for a remote machine)", rawURL)
	}
	if u.Path != "" || u.RawQuery != "" || u.User != nil {
		return nil, fmt.Errorf("daemon URL must be just scheme, host and port, got %q", rawURL)
	}
	return &daemonClient{
		base: u,
		// Repo deploys clone and build synchronously, so they can take a while.
		http: &http.Client{
			Timeout: 30 * time.Minute,
			// Never follow a redirect off localhost.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validContainerID(id string) error {
	if !containerIDPattern.MatchString(id) || strings.Contains(id, "..") {
		return fmt.Errorf("invalid container id %q", id)
	}
	return nil
}

// daemonError is a non-2xx response from the daemon.
type daemonError struct {
	Status  int
	Message string
}

func (e *daemonError) Error() string {
	return fmt.Sprintf("daemon returned HTTP %d: %s", e.Status, e.Message)
}

func isNotFound(err error) bool {
	var de *daemonError
	return errors.As(err, &de) && de.Status == http.StatusNotFound
}

// do sends a request and decodes a JSON response into out. When the daemon
// replies with an error status, the body is still decoded into out (the
// deploy endpoint returns details on failure) and a *daemonError is returned.
func (c *daemonClient) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base.String()+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach the Vedocker daemon at %s (is `sudo ./minicontainerd` running?): %w", c.base, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("read daemon response: %w", err)
	}

	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil && resp.StatusCode < 300 {
			return fmt.Errorf("decode daemon response: %w", err)
		}
	}

	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		msg := e.Error
		if msg == "" {
			msg = strings.TrimSpace(string(data))
		}
		return &daemonError{Status: resp.StatusCode, Message: msg}
	}
	return nil
}

type portMapping struct {
	HostPort      int `json:"host_port"`
	ContainerPort int `json:"container_port"`
}

type containerInfo struct {
	ID        string        `json:"id"`
	Status    string        `json:"status"`
	PID       int           `json:"pid,omitempty"`
	IP        string        `json:"ip,omitempty"`
	Ports     []portMapping `json:"ports,omitempty"`
	Command   []string      `json:"command,omitempty"`
	CreatedAt string        `json:"created_at,omitempty"`
	UpdatedAt string        `json:"updated_at,omitempty"`
}

type imageInfo struct {
	Ref        string   `json:"ref"`
	Name       string   `json:"name,omitempty"`
	Tag        string   `json:"tag,omitempty"`
	Source     string   `json:"source,omitempty"`
	CreatedAt  string   `json:"created_at,omitempty"`
	Entrypoint []string `json:"entrypoint,omitempty"`
	Cmd        []string `json:"cmd,omitempty"`
}

// deployRepoResult mirrors deployRepoResponse in cmd/minicontainerd.
type deployRepoResult struct {
	OK             bool   `json:"ok"`
	RepoURL        string `json:"repo_url"`
	ImageRef       string `json:"image_ref,omitempty"`
	ContainerID    string `json:"container_id,omitempty"`
	DockerfilePath string `json:"dockerfile_path,omitempty"`
	NeedsAI        bool   `json:"needs_ai"`
	AIGenerated    bool   `json:"ai_generated,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Error          string `json:"error,omitempty"`
}

// GET /containers
func (c *daemonClient) listContainers(ctx context.Context) ([]containerInfo, error) {
	var resp struct {
		Containers []containerInfo `json:"containers"`
	}
	if err := c.do(ctx, http.MethodGet, "/containers", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Containers, nil
}

// GET /containers/{id}
func (c *daemonClient) getContainer(ctx context.Context, id string) (*containerInfo, error) {
	var info containerInfo
	if err := c.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(id), nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// GET /containers/{id}/logs?stream=stdout|stderr
func (c *daemonClient) containerLogs(ctx context.Context, id, stream string) (string, error) {
	var resp struct {
		Logs string `json:"logs"`
	}
	path := "/containers/" + url.PathEscape(id) + "/logs?stream=" + url.QueryEscape(stream)
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return "", err
	}
	return resp.Logs, nil
}

// POST /containers/{id}/stop
func (c *daemonClient) stopContainer(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/stop", nil, nil)
}

// DELETE /containers/{id}
func (c *daemonClient) removeContainer(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/containers/"+url.PathEscape(id), nil, nil)
}

// GET /images
func (c *daemonClient) listImages(ctx context.Context) ([]imageInfo, error) {
	var resp struct {
		Images []imageInfo `json:"images"`
	}
	if err := c.do(ctx, http.MethodGet, "/images", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Images, nil
}

// POST /deployments/repo. The daemon clones, builds and starts the container
// before it responds, so this blocks for the whole deploy. The Gemini key is
// never sent from here; the daemon reads GEMINI_API_KEY from its own env.
func (c *daemonClient) deployRepo(ctx context.Context, repoURL string) (*deployRepoResult, error) {
	var res deployRepoResult
	err := c.do(ctx, http.MethodPost, "/deployments/repo", map[string]string{"repo_url": repoURL}, &res)
	return &res, err
}
