package container

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type dockerfileInstruction struct {
	Name string
	Args string
}

type buildState struct {
	Workdir    string
	Env        map[string]string
	Entrypoint []string
	Cmd        []string
}

func BuildImage(ref, dockerfilePath, contextDir string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("image build must be run as root")
	}

	name, tag, err := ParseImageRef(ref)
	if err != nil {
		return err
	}

	absDockerfile, err := filepath.Abs(dockerfilePath)
	if err != nil {
		return fmt.Errorf("resolve Dockerfile path: %w", err)
	}
	absContext, err := filepath.Abs(contextDir)
	if err != nil {
		return fmt.Errorf("resolve context dir: %w", err)
	}

	dfInfo, err := os.Stat(absDockerfile)
	if err != nil {
		return fmt.Errorf("stat Dockerfile: %w", err)
	}
	if dfInfo.IsDir() {
		return fmt.Errorf("Dockerfile path must be a file")
	}

	ctxInfo, err := os.Stat(absContext)
	if err != nil {
		return fmt.Errorf("stat context dir: %w", err)
	}
	if !ctxInfo.IsDir() {
		return fmt.Errorf("context must be a directory")
	}

	instructions, err := parseDockerfile(absDockerfile)
	if err != nil {
		return err
	}
	if len(instructions) == 0 {
		return fmt.Errorf("Dockerfile has no supported instructions")
	}
	if instructions[0].Name != "FROM" {
		return fmt.Errorf("Dockerfile must start with FROM")
	}

	baseRef := strings.TrimSpace(instructions[0].Args)

	baseRootfs, err := ResolveRootfs(baseRef)
	if err != nil {
		if IsImageNotFound(err) {
			if err := PullImage(baseRef); err != nil {
				return fmt.Errorf("pull FROM image %q: %w", baseRef, err)
			}

			baseRootfs, err = ResolveRootfs(baseRef)
			if err != nil {
				return fmt.Errorf("resolve FROM image after pull: %w", err)
			}
		} else {
			return fmt.Errorf("resolve FROM image: %w", err)
		}
	}

	if err := os.MkdirAll(imageStoreRoot, 0o755); err != nil {
		return fmt.Errorf("create image store dir: %w", err)
	}

	finalDir := imageStoreDir(name, tag)
	tmpDir := finalDir + ".tmp"

	_ = os.RemoveAll(tmpDir)
	if err := copyTree(baseRootfs, tmpDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("copy base rootfs: %w", err)
	}

	state := buildState{
		Workdir: "/",
		Env:     make(map[string]string),
	}

	for _, inst := range instructions[1:] {
		switch inst.Name {
		case "MAINTAINER":
			// accepted as a no-op
		case "WORKDIR":
			if err := applyWorkdirInstruction(tmpDir, &state, inst.Args); err != nil {
				_ = os.RemoveAll(tmpDir)
				return err
			}
		case "ENV":
			if err := applyEnvInstruction(&state, inst.Args); err != nil {
				_ = os.RemoveAll(tmpDir)
				return err
			}
		case "COPY":
			if err := applyCopyInstruction(absContext, tmpDir, state.Workdir, inst.Args); err != nil {
				_ = os.RemoveAll(tmpDir)
				return err
			}
		case "RUN":
			if err := applyRunInstruction(tmpDir, state.Workdir, state.Env, inst.Args); err != nil {
				_ = os.RemoveAll(tmpDir)
				return err
			}
		case "ENTRYPOINT":
			entrypoint, err := parseCommandInstruction(inst.Args)
			if err != nil {
				_ = os.RemoveAll(tmpDir)
				return fmt.Errorf("parse ENTRYPOINT: %w", err)
			}
			state.Entrypoint = entrypoint
		case "CMD":
			cmd, err := parseCommandInstruction(inst.Args)
			if err != nil {
				_ = os.RemoveAll(tmpDir)
				return fmt.Errorf("parse CMD: %w", err)
			}
			state.Cmd = cmd
		case "EXPOSE":
			// accepted as a no-op for now
		default:
			_ = os.RemoveAll(tmpDir)
			return fmt.Errorf("unsupported Dockerfile instruction: %s", inst.Name)
		}
	}

	_ = os.RemoveAll(finalDir)
	if err := os.Rename(tmpDir, finalDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("activate built image rootfs: %w", err)
	}

	return writeImageMetadataWithConfig(name+":"+tag, finalDir, "build", state.Entrypoint, state.Cmd)
}

func parseDockerfile(path string) ([]dockerfileInstruction, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Dockerfile: %w", err)
	}

	rawLines := strings.Split(string(data), "\n")

	var logicalLines []string
	var current strings.Builder

	flushCurrent := func() {
		line := strings.TrimSpace(current.String())
		if line != "" {
			logicalLines = append(logicalLines, line)
		}
		current.Reset()
	}

	for _, raw := range rawLines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasSuffix(line, "\\") {
			line = strings.TrimSpace(strings.TrimSuffix(line, "\\"))
			if current.Len() > 0 {
				current.WriteString(" ")
			}
			current.WriteString(line)
			continue
		}

		if current.Len() > 0 {
			current.WriteString(" ")
			current.WriteString(line)
			flushCurrent()
			continue
		}

		logicalLines = append(logicalLines, line)
	}

	if current.Len() > 0 {
		flushCurrent()
	}

	var out []dockerfileInstruction

	for _, line := range logicalLines {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid Dockerfile line: %s", line)
		}

		name := strings.ToUpper(strings.TrimSpace(parts[0]))
		args := strings.TrimSpace(parts[1])
		if args == "" {
			return nil, fmt.Errorf("missing arguments for %s", name)
		}

		switch name {
		case "FROM", "COPY", "RUN", "MAINTAINER", "WORKDIR", "ENV", "CMD", "EXPOSE", "ENTRYPOINT":
			out = append(out, dockerfileInstruction{Name: name, Args: args})
		default:
			return nil, fmt.Errorf("unsupported Dockerfile instruction: %s", name)
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("Dockerfile is empty")
	}

	return out, nil
}

func applyWorkdirInstruction(rootfs string, state *buildState, args string) error {
	target := strings.TrimSpace(args)
	if target == "" {
		return fmt.Errorf("WORKDIR requires a path")
	}

	resolved, err := resolveContainerPath(state.Workdir, target)
	if err != nil {
		return err
	}

	hostPath, err := rootfsJoin(rootfs, resolved)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(hostPath, 0o755); err != nil {
		return fmt.Errorf("create WORKDIR %s: %w", resolved, err)
	}

	state.Workdir = resolved
	return nil
}

func applyEnvInstruction(state *buildState, args string) error {
	args = strings.TrimSpace(args)
	if args == "" {
		return fmt.Errorf("ENV requires arguments")
	}

	if strings.Contains(args, "=") && len(strings.Fields(args)) == 1 {
		parts := strings.SplitN(args, "=", 2)
		key := strings.TrimSpace(parts[0])
		val := parts[1]
		if key == "" {
			return fmt.Errorf("ENV key cannot be empty")
		}
		state.Env[key] = val
		return nil
	}

	parts := strings.Fields(args)
	if len(parts) < 2 {
		return fmt.Errorf("ENV requires KEY value")
	}

	key := strings.TrimSpace(parts[0])
	if key == "" {
		return fmt.Errorf("ENV key cannot be empty")
	}

	value := strings.Join(parts[1:], " ")
	state.Env[key] = value
	return nil
}

func applyCopyInstruction(contextDir, rootfs, workdir, args string) error {
	parts := strings.Fields(args)
	if len(parts) != 2 {
		return fmt.Errorf("COPY requires exactly 2 arguments")
	}

	srcArg := parts[0]
	dstArg := parts[1]

	resolvedDst, err := resolveContainerPath(workdir, dstArg)
	if err != nil {
		return err
	}

	destPath, err := rootfsJoin(rootfs, resolvedDst)
	if err != nil {
		return err
	}

	hasGlob := strings.ContainsAny(srcArg, "*?[")
	if hasGlob {
		pattern := filepath.Join(contextDir, srcArg)
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("invalid COPY glob: %w", err)
		}
		if len(matches) == 0 {
			return fmt.Errorf("stat COPY source: stat %s: no such file or directory", pattern)
		}

		if err := os.MkdirAll(destPath, 0o755); err != nil {
			return fmt.Errorf("create COPY destination dir: %w", err)
		}

		for _, match := range matches {
			absSrc, err := filepath.Abs(match)
			if err != nil {
				return fmt.Errorf("resolve COPY source: %w", err)
			}
			if !isWithinBase(absSrc, contextDir) {
				return fmt.Errorf("COPY source escapes build context: %s", srcArg)
			}

			srcInfo, err := os.Stat(absSrc)
			if err != nil {
				return fmt.Errorf("stat COPY source: %w", err)
			}

			target := filepath.Join(destPath, filepath.Base(absSrc))

			if srcInfo.IsDir() {
				if err := copyTree(absSrc, target); err != nil {
					return err
				}
				continue
			}

			if err := copyFile(absSrc, target, srcInfo.Mode()); err != nil {
				return err
			}
		}
		return nil
	}

	srcPath := filepath.Join(contextDir, srcArg)
	absSrc, err := filepath.Abs(srcPath)
	if err != nil {
		return fmt.Errorf("resolve COPY source: %w", err)
	}
	if !isWithinBase(absSrc, contextDir) {
		return fmt.Errorf("COPY source escapes build context: %s", srcArg)
	}

	srcInfo, err := os.Stat(absSrc)
	if err != nil {
		return fmt.Errorf("stat COPY source: %w", err)
	}

	if srcInfo.IsDir() {
		return copyTree(absSrc, destPath)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create COPY parent dir: %w", err)
	}
	return copyFile(absSrc, destPath, srcInfo.Mode())
}

func applyRunInstruction(rootfs, workdir string, env map[string]string, cmd string) error {
	script := cmd
	if strings.TrimSpace(workdir) != "" && workdir != "/" {
		script = "cd " + shellQuote(workdir) + " && " + cmd
	}

	command := exec.Command("chroot", rootfs, "/bin/sh", "-c", script)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Stdin = nil
	command.Env = append(os.Environ(), buildEnvSlice(env)...)

	if err := command.Run(); err != nil {
		return fmt.Errorf("RUN failed: %w", err)
	}
	return nil
}

func parseCommandInstruction(args string) ([]string, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return nil, fmt.Errorf("instruction requires arguments")
	}

	if strings.HasPrefix(args, "[") {
		var arr []string
		if err := json.Unmarshal([]byte(args), &arr); err != nil {
			return nil, fmt.Errorf("invalid JSON array form: %w", err)
		}
		if len(arr) == 0 {
			return nil, fmt.Errorf("instruction cannot be an empty array")
		}
		for i := range arr {
			arr[i] = strings.TrimSpace(arr[i])
			if arr[i] == "" {
				return nil, fmt.Errorf("instruction contains an empty argument")
			}
		}
		return arr, nil
	}

	return []string{"/bin/sh", "-c", args}, nil
}

func buildEnvSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

func resolveContainerPath(currentWorkdir, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	if strings.HasPrefix(target, "/") {
		return filepath.Clean(target), nil
	}

	base := currentWorkdir
	if base == "" {
		base = "/"
	}
	return filepath.Clean(filepath.Join(base, target)), nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func rootfsJoin(rootfs, target string) (string, error) {
	cleanTarget := filepath.Clean("/" + target)
	joined := filepath.Join(rootfs, cleanTarget)
	absRootfs, err := filepath.Abs(rootfs)
	if err != nil {
		return "", fmt.Errorf("resolve build rootfs: %w", err)
	}
	absJoined, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("resolve build target: %w", err)
	}
	if !isWithinBase(absJoined, absRootfs) && absJoined != absRootfs {
		return "", fmt.Errorf("destination escapes image rootfs: %s", target)
	}
	return absJoined, nil
}

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return copyFile(src, dst, info.Mode())
	}

	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}

	return filepath.Walk(src, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == src {
			return nil
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		switch {
		case fi.IsDir():
			return os.MkdirAll(target, fi.Mode().Perm())
		case fi.Mode()&os.ModeSymlink != 0:
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_ = os.RemoveAll(target)
			return os.Symlink(linkTarget, target)
		case fi.Mode().IsRegular():
			return copyFile(path, target, fi.Mode())
		default:
			return nil
		}
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func isWithinBase(path, base string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != "..")
}
