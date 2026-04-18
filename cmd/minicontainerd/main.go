package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vthecar/minicontainer/pkg/container"
)

const (
	listenAddr         = "127.0.0.1:18080"
	containerStateRoot = "/var/lib/minicontainer/containers"
	cliPathEnvVar      = "MINICONTAINER_CLI_PATH"
)

type createContainerRequest struct {
	ID      string   `json:"id"`
	Image   string   `json:"image"`
	Command []string `json:"command"`
}

func main() {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "minicontainerd must be run as root")
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/images", handleImages)
	mux.HandleFunc("/images/", handleImageByRef)

	mux.HandleFunc("/containers", handleContainers)
	mux.HandleFunc("/containers/create", handleCreateContainer)
	mux.HandleFunc("/containers/", handleContainerRoutes)

	server := &http.Server{
		Addr:    listenAddr,
		Handler: loggingMiddleware(mux),
	}

	fmt.Printf("minicontainerd listening on http://%s\n", listenAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "daemon error: %v\n", err)
		os.Exit(1)
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("%s %s\n", r.Method, r.URL.RequestURI())
		next.ServeHTTP(w, r)
	})
}

func handleImages(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/images" {
		writeNotFound(w, "endpoint not found")
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	images, err := container.ListImages()
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"images": images,
	})
}

func handleImageByRef(w http.ResponseWriter, r *http.Request) {
	ref := strings.TrimPrefix(r.URL.Path, "/images/")
	ref = strings.TrimSpace(ref)
	if ref == "" {
		writeBadRequest(w, "missing image ref")
		return
	}

	switch r.Method {
	case http.MethodGet:
		img, err := container.GetImage(ref)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				writeNotFound(w, err.Error())
				return
			}
			writeInternalError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, img)

	case http.MethodDelete:
		if err := container.RemoveImage(ref); err != nil {
			if strings.Contains(err.Error(), "not found") {
				writeNotFound(w, err.Error())
				return
			}
			writeInternalError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":  true,
			"ref": ref,
		})

	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodDelete)
	}
}

func handleContainers(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/containers" {
		writeNotFound(w, "endpoint not found")
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	containers, err := listContainerStates()
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"containers": containers,
	})
}

func handleCreateContainer(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/containers/create" {
		writeNotFound(w, "endpoint not found")
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}

	var req createContainerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, fmt.Sprintf("invalid JSON body: %v", err))
		return
	}

	req.ID = strings.TrimSpace(req.ID)
	req.Image = strings.TrimSpace(req.Image)

	if req.ID == "" {
		writeBadRequest(w, "id is required")
		return
	}
	if req.Image == "" {
		writeBadRequest(w, "image is required")
		return
	}
	if len(req.Command) == 0 {
		writeBadRequest(w, "command must be a non-empty array")
		return
	}
	for i, part := range req.Command {
		req.Command[i] = strings.TrimSpace(part)
	}
	if req.Command[0] == "" {
		writeBadRequest(w, "command[0] must be non-empty")
		return
	}

	rootfs, err := container.ResolveRootfs(req.Image)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeNotFound(w, err.Error())
			return
		}
		writeInternalError(w, err)
		return
	}

	if err := container.Create(req.ID, rootfs, req.Command, nil, nil, nil); err != nil {
		if strings.Contains(err.Error(), "exists") || strings.Contains(err.Error(), "already") {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": err.Error(),
			})
			return
		}
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"id": req.ID,
	})
}

func handleContainerRoutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/containers/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		writeNotFound(w, "endpoint not found")
		return
	}

	parts := strings.Split(trimmed, "/")
	id := strings.TrimSpace(parts[0])
	if id == "" {
		writeBadRequest(w, "missing container id")
		return
	}

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			state, err := readContainerState(id)
			if err != nil {
				handleContainerReadError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, state)
			return
		case http.MethodDelete:
			if err := container.Remove(id); err != nil {
				handleContainerActionError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"ok": true,
				"id": id,
			})
			return
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodDelete)
			return
		}
	}

	if len(parts) != 2 {
		writeNotFound(w, "endpoint not found")
		return
	}

	action := parts[1]

	switch action {
	case "start":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		if err := startContainerViaCLI(id); err != nil {
			handleContainerActionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"id": id,
		})
	case "stop":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		if err := container.Stop(id); err != nil {
			handleContainerActionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"id": id,
		})
	case "logs":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}

		stream := r.URL.Query().Get("stream")
		if stream == "" {
			stream = "stdout"
		}
		if stream != "stdout" && stream != "stderr" {
			writeBadRequest(w, "stream must be stdout or stderr")
			return
		}

		data, err := readContainerLog(id, stream)
		if err != nil {
			handleContainerReadError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"id":     id,
			"stream": stream,
			"logs":   string(data),
		})
	default:
		writeNotFound(w, "endpoint not found")
	}
}

func startContainerViaCLI(id string) error {
	cliPath, err := resolveCLIPath()
	if err != nil {
		return err
	}

	cmd := exec.Command(cliPath, "start", id)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("start via CLI: %w", err)
	}

	return nil
}

func resolveCLIPath() (string, error) {
	if configured := strings.TrimSpace(os.Getenv(cliPathEnvVar)); configured != "" {
		abs, err := filepath.Abs(configured)
		if err == nil {
			configured = abs
		}
		info, err := os.Stat(configured)
		if err != nil {
			return "", fmt.Errorf("%s points to invalid path %q: %w", cliPathEnvVar, configured, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s points to a directory, expected binary: %q", cliPathEnvVar, configured)
		}
		return configured, nil
	}

	path, err := exec.LookPath("minicontainer")
	if err != nil {
		return "", fmt.Errorf("could not find minicontainer CLI binary; set %s to its full path", cliPathEnvVar)
	}
	return path, nil
}

func listContainerStates() ([]map[string]any, error) {
	entries, err := os.ReadDir(containerStateRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return []map[string]any{}, nil
		}
		return nil, fmt.Errorf("read container root: %w", err)
	}

	var out []map[string]any
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		id := entry.Name()
		state, err := readContainerState(id)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		out = append(out, state)
	}

	sort.Slice(out, func(i, j int) bool {
		return stringValue(out[i], "id") < stringValue(out[j], "id")
	})

	return out, nil
}

func readContainerState(id string) (map[string]any, error) {
	path := filepath.Join(containerStateRoot, id, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse state for %q: %w", id, err)
	}

	if _, ok := state["id"]; !ok {
		state["id"] = id
	}
	return state, nil
}

func readContainerLog(id, stream string) ([]byte, error) {
	state, err := readContainerState(id)
	if err != nil {
		return nil, err
	}

	candidates := candidateLogPaths(id, stream, state)
	for _, path := range candidates {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err == nil {
			return data, nil
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read %s log %q: %w", stream, path, err)
		}
	}

	return nil, fmt.Errorf("log file for container %q stream %q not found", id, stream)
}

func candidateLogPaths(id, stream string, state map[string]any) []string {
	keys := []string{
		stream + "_path",
		stream + "Path",
		stream,
	}

	var paths []string
	for _, key := range keys {
		if value := stringValue(state, key); value != "" {
			paths = append(paths, value)
		}
	}

	paths = append(paths,
		filepath.Join(containerStateRoot, id, stream+".log"),
		filepath.Join(containerStateRoot, id, "logs", stream+".log"),
	)

	return uniqueNonEmpty(paths)
}

func stringValue(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func handleContainerReadError(w http.ResponseWriter, err error) {
	if os.IsNotExist(err) || strings.Contains(err.Error(), "not found") {
		writeNotFound(w, err.Error())
		return
	}
	writeInternalError(w, err)
}

func handleContainerActionError(w http.ResponseWriter, err error) {
	if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "not found") {
		writeNotFound(w, err.Error())
		return
	}
	writeInternalError(w, err)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeBadRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"error": msg,
	})
}

func writeNotFound(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusNotFound, map[string]any{
		"error": msg,
	})
}

func writeInternalError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, map[string]any{
		"error": err.Error(),
	})
}

func writeMethodNotAllowed(w http.ResponseWriter, allowed ...string) {
	if len(allowed) > 0 {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
	}
	writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
		"error": "method not allowed",
	})
}
