package container

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type dockerfileInstruction struct {
	Name string
	Args string
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

	baseRootfs, err := ResolveRootfs(strings.TrimSpace(instructions[0].Args))
	if err != nil {
		return fmt.Errorf("resolve FROM image: %w", err)
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

	for _, inst := range instructions[1:] {
		switch inst.Name {
		case "COPY":
			if err := applyCopyInstruction(absContext, tmpDir, inst.Args); err != nil {
				_ = os.RemoveAll(tmpDir)
				return err
			}
		case "RUN":
			if err := applyRunInstruction(tmpDir, inst.Args); err != nil {
				_ = os.RemoveAll(tmpDir)
				return err
			}
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

	return writeImageMetadata(name+":"+tag, finalDir, "build")
}

func parseDockerfile(path string) ([]dockerfileInstruction, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Dockerfile: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var out []dockerfileInstruction

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

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
		case "FROM", "COPY", "RUN":
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

func applyCopyInstruction(contextDir, rootfs, args string) error {
	parts := strings.Fields(args)
	if len(parts) != 2 {
		return fmt.Errorf("COPY requires exactly 2 arguments")
	}

	srcArg := parts[0]
	dstArg := parts[1]

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

	destPath, err := rootfsJoin(rootfs, dstArg)
	if err != nil {
		return err
	}

	if srcInfo.IsDir() {
		return copyTree(absSrc, destPath)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create COPY parent dir: %w", err)
	}
	return copyFile(absSrc, destPath, srcInfo.Mode())
}

func applyRunInstruction(rootfs, cmd string) error {
	command := exec.Command("chroot", rootfs, "/bin/sh", "-c", cmd)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Stdin = nil

	if err := command.Run(); err != nil {
		return fmt.Errorf("RUN failed: %w", err)
	}
	return nil
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
