package container

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	imageRefRoot   = "/var/lib/minicontainer/images/refs"
	imageStoreRoot = "/var/lib/minicontainer/images/store"

	dockerHubRegistry = "https://registry-1.docker.io"
	dockerHubAuth     = "https://auth.docker.io/token"
)

type Image struct {
	Name       string   `json:"name"`
	Tag        string   `json:"tag"`
	Ref        string   `json:"ref"`
	Rootfs     string   `json:"rootfs"`
	Source     string   `json:"source"`
	CreatedAt  string   `json:"created_at"`
	Entrypoint []string `json:"entrypoint,omitempty"`
	Cmd        []string `json:"cmd,omitempty"`
}

type registryDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	Platform  struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
		Variant      string `json:"variant"`
	} `json:"platform"`
}

type registryManifestList struct {
	SchemaVersion int                  `json:"schemaVersion"`
	MediaType     string               `json:"mediaType"`
	Manifests     []registryDescriptor `json:"manifests"`
}

type registryManifest struct {
	SchemaVersion int                  `json:"schemaVersion"`
	MediaType     string               `json:"mediaType"`
	Config        registryDescriptor   `json:"config"`
	Layers        []registryDescriptor `json:"layers"`
}

func ParseImageRef(ref string) (name, tag string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", fmt.Errorf("image ref cannot be empty")
	}

	parts := strings.SplitN(ref, ":", 2)
	name = strings.TrimSpace(parts[0])
	if name == "" {
		return "", "", fmt.Errorf("image name cannot be empty")
	}

	if len(parts) == 1 || strings.TrimSpace(parts[1]) == "" {
		tag = "latest"
	} else {
		tag = strings.TrimSpace(parts[1])
	}

	for _, r := range name {
		isLower := r >= 'a' && r <= 'z'
		isUpper := r >= 'A' && r <= 'Z'
		isDigit := r >= '0' && r <= '9'
		isSafe := r == '-' || r == '_' || r == '.' || r == '/'
		if !isLower && !isUpper && !isDigit && !isSafe {
			return "", "", fmt.Errorf("invalid image name %q", name)
		}
	}

	for _, r := range tag {
		isLower := r >= 'a' && r <= 'z'
		isUpper := r >= 'A' && r <= 'Z'
		isDigit := r >= '0' && r <= '9'
		isSafe := r == '-' || r == '_' || r == '.'
		if !isLower && !isUpper && !isDigit && !isSafe {
			return "", "", fmt.Errorf("invalid image tag %q", tag)
		}
	}

	return name, tag, nil
}

func AddImage(ref, rootfs string) error {
	return writeImageMetadata(ref, rootfs, "add")
}

func ListImages() ([]Image, error) {
	entries, err := os.ReadDir(imageRefRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read image ref dir: %w", err)
	}

	var images []Image
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(imageRefRoot, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read image metadata %s: %w", path, err)
		}

		var img Image
		if err := json.Unmarshal(data, &img); err != nil {
			return nil, fmt.Errorf("parse image metadata %s: %w", path, err)
		}
		images = append(images, img)
	}

	sort.Slice(images, func(i, j int) bool {
		return images[i].Ref < images[j].Ref
	})

	return images, nil
}

func ResolveRootfs(input string) (string, error) {
	absInput, err := filepath.Abs(input)
	if err == nil {
		if info, statErr := os.Stat(absInput); statErr == nil && info.IsDir() {
			return absInput, nil
		}
	}

	img, err := GetImage(input)
	if err != nil {
		return "", err
	}

	absRootfs, err := filepath.Abs(img.Rootfs)
	if err != nil {
		return "", fmt.Errorf("resolve stored rootfs path: %w", err)
	}

	info, err := os.Stat(absRootfs)
	if err != nil {
		return "", fmt.Errorf("stat stored rootfs: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("stored rootfs is not a directory: %s", absRootfs)
	}

	return absRootfs, nil
}

func GetImage(ref string) (*Image, error) {
	name, tag, err := ParseImageRef(ref)
	if err != nil {
		return nil, err
	}

	path := imageRefFile(name, tag)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("image %q not found", name+":"+tag)
		}
		return nil, fmt.Errorf("read image metadata: %w", err)
	}

	var img Image
	if err := json.Unmarshal(data, &img); err != nil {
		return nil, fmt.Errorf("parse image metadata: %w", err)
	}

	return &img, nil
}

func IsImageNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "image ") && strings.Contains(err.Error(), " not found")
}

func RemoveImage(ref string) error {
	name, tag, err := ParseImageRef(ref)
	if err != nil {
		return err
	}

	path := imageRefFile(name, tag)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("image %q not found", name+":"+tag)
		}
		return fmt.Errorf("remove image metadata: %w", err)
	}

	return nil
}

func ImportImage(ref, tarPath string) error {
	name, tag, err := ParseImageRef(ref)
	if err != nil {
		return err
	}

	absTarPath, err := filepath.Abs(tarPath)
	if err != nil {
		return fmt.Errorf("resolve tar path: %w", err)
	}

	info, err := os.Stat(absTarPath)
	if err != nil {
		return fmt.Errorf("stat tar file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("tar path must be a file: %s", absTarPath)
	}

	if err := os.MkdirAll(imageStoreRoot, 0o755); err != nil {
		return fmt.Errorf("create image store dir: %w", err)
	}
	if err := os.MkdirAll(imageRefRoot, 0o755); err != nil {
		return fmt.Errorf("create image ref dir: %w", err)
	}

	targetDir := imageStoreDir(name, tag)
	tmpDir := targetDir + ".tmp"

	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create temp image dir: %w", err)
	}

	if err := unpackRootfsTar(absTarPath, tmpDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return err
	}

	_ = os.RemoveAll(targetDir)
	if err := os.Rename(tmpDir, targetDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("activate imported rootfs: %w", err)
	}

	return writeImageMetadata(name+":"+tag, targetDir, "import")
}

func ExportImage(ref, outputTarPath string) error {
	img, err := GetImage(ref)
	if err != nil {
		return err
	}

	absRootfs, err := filepath.Abs(img.Rootfs)
	if err != nil {
		return fmt.Errorf("resolve image rootfs path: %w", err)
	}

	info, err := os.Stat(absRootfs)
	if err != nil {
		return fmt.Errorf("stat image rootfs: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("image rootfs is not a directory: %s", absRootfs)
	}

	absOutput, err := filepath.Abs(outputTarPath)
	if err != nil {
		return fmt.Errorf("resolve output tar path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(absOutput), 0o755); err != nil {
		return fmt.Errorf("create output parent dir: %w", err)
	}

	out, err := os.Create(absOutput)
	if err != nil {
		return fmt.Errorf("create output tar: %w", err)
	}
	defer out.Close()

	tw := tar.NewWriter(out)
	defer tw.Close()

	err = filepath.Walk(absRootfs, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == absRootfs {
			return nil
		}

		relPath, err := filepath.Rel(absRootfs, path)
		if err != nil {
			return fmt.Errorf("compute relative path: %w", err)
		}
		relPath = filepath.ToSlash(relPath)

		linkTarget := ""
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err = os.Readlink(path)
			if err != nil {
				return fmt.Errorf("read symlink %s: %w", path, err)
			}
		}

		hdr, err := tar.FileInfoHeader(info, linkTarget)
		if err != nil {
			return fmt.Errorf("build tar header for %s: %w", path, err)
		}
		hdr.Name = relPath

		if info.IsDir() && !strings.HasSuffix(hdr.Name, "/") {
			hdr.Name += "/"
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("write tar header for %s: %w", path, err)
		}

		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("open file %s: %w", path, err)
			}

			if _, err := io.Copy(tw, f); err != nil {
				_ = f.Close()
				return fmt.Errorf("write tar content for %s: %w", path, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("close file %s: %w", path, err)
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar writer: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close output tar: %w", err)
	}

	return nil
}

func PullImage(ref string) error {
	name, tag, err := ParseImageRef(ref)
	if err != nil {
		return err
	}

	repo := normalizeDockerHubRepo(name)
	token, err := dockerHubToken(repo)
	if err != nil {
		return err
	}

	manifest, err := fetchDockerHubManifest(repo, tag, token)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(imageStoreRoot, 0o755); err != nil {
		return fmt.Errorf("create image store dir: %w", err)
	}
	if err := os.MkdirAll(imageRefRoot, 0o755); err != nil {
		return fmt.Errorf("create image ref dir: %w", err)
	}

	targetDir := imageStoreDir(name, tag)
	tmpDir := targetDir + ".tmp"

	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create temp image dir: %w", err)
	}

	for _, layer := range manifest.Layers {
		rc, err := fetchDockerHubBlob(repo, layer.Digest, token)
		if err != nil {
			_ = os.RemoveAll(tmpDir)
			return err
		}
		if err := unpackLayerTar(rc, tmpDir); err != nil {
			_ = rc.Close()
			_ = os.RemoveAll(tmpDir)
			return err
		}
		if err := rc.Close(); err != nil {
			_ = os.RemoveAll(tmpDir)
			return fmt.Errorf("close layer stream: %w", err)
		}
	}

	_ = os.RemoveAll(targetDir)
	if err := os.Rename(tmpDir, targetDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("activate pulled rootfs: %w", err)
	}

	return writeImageMetadata(name+":"+tag, targetDir, "pull")
}

func fetchDockerHubManifest(repo, ref, token string) (*registryManifest, error) {
	body, mediaType, err := dockerHubManifestRequest(repo, ref, token)
	if err != nil {
		return nil, err
	}

	switch mediaType {
	case "application/vnd.docker.distribution.manifest.list.v2+json", "application/vnd.oci.image.index.v1+json":
		var idx registryManifestList
		if err := json.Unmarshal(body, &idx); err != nil {
			return nil, fmt.Errorf("parse manifest list: %w", err)
		}

		digest, err := chooseARM64Manifest(idx)
		if err != nil {
			return nil, err
		}

		body, _, err = dockerHubManifestRequest(repo, digest, token)
		if err != nil {
			return nil, err
		}
	}

	var manifest registryManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("parse image manifest: %w", err)
	}
	if len(manifest.Layers) == 0 {
		return nil, fmt.Errorf("image manifest has no layers")
	}

	return &manifest, nil
}

func chooseARM64Manifest(idx registryManifestList) (string, error) {
	for _, m := range idx.Manifests {
		if m.Platform.OS == "linux" && m.Platform.Architecture == "arm64" {
			return m.Digest, nil
		}
	}
	return "", fmt.Errorf("no linux/arm64 manifest found")
}

func dockerHubManifestRequest(repo, ref, token string) ([]byte, string, error) {
	u := fmt.Sprintf("%s/v2/%s/manifests/%s", dockerHubRegistry, repo, ref)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build manifest request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
	}, ", "))
	req.Header.Set("User-Agent", "minicontainer/0.1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("manifest request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read manifest response: %w", err)
	}

	mediaType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	return body, mediaType, nil
}

func fetchDockerHubBlob(repo, digest, token string) (io.ReadCloser, error) {
	u := fmt.Sprintf("%s/v2/%s/blobs/%s", dockerHubRegistry, repo, digest)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build blob request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "minicontainer/0.1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request blob %s: %w", digest, err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("blob request failed for %s: %s: %s", digest, resp.Status, strings.TrimSpace(string(body)))
	}

	return resp.Body, nil
}

func dockerHubToken(repo string) (string, error) {
	u, err := url.Parse(dockerHubAuth)
	if err != nil {
		return "", fmt.Errorf("parse auth URL: %w", err)
	}

	q := u.Query()
	q.Set("service", "registry.docker.io")
	q.Set("scope", "repository:"+repo+":pull")
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("User-Agent", "minicontainer/0.1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request auth token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("auth token request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("parse auth token response: %w", err)
	}
	if payload.Token == "" {
		return "", fmt.Errorf("empty auth token")
	}

	return payload.Token, nil
}

func normalizeDockerHubRepo(name string) string {
	if strings.Contains(name, "/") {
		return name
	}
	return "library/" + name
}

func unpackRootfsTar(tarPath, dest string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("open tar file: %w", err)
	}
	defer f.Close()

	tr := tar.NewReader(f)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		cleanName := filepath.Clean(hdr.Name)
		if cleanName == "." || cleanName == "/" {
			continue
		}
		if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			return fmt.Errorf("unsafe tar entry path: %s", hdr.Name)
		}

		targetPath := filepath.Join(dest, cleanName)
		if !strings.HasPrefix(targetPath, dest+string(os.PathSeparator)) && targetPath != dest {
			return fmt.Errorf("unsafe tar entry target: %s", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(hdr.Mode)&0o777); err != nil {
				return fmt.Errorf("create dir %s: %w", targetPath, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create parent dir for %s: %w", targetPath, err)
			}
			out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return fmt.Errorf("create file %s: %w", targetPath, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return fmt.Errorf("write file %s: %w", targetPath, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close file %s: %w", targetPath, err)
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create parent dir for symlink %s: %w", targetPath, err)
			}
			if err := os.Symlink(hdr.Linkname, targetPath); err != nil {
				return fmt.Errorf("create symlink %s: %w", targetPath, err)
			}
		case tar.TypeLink:
			linkTarget := filepath.Join(dest, filepath.Clean(hdr.Linkname))
			if !strings.HasPrefix(linkTarget, dest+string(os.PathSeparator)) && linkTarget != dest {
				return fmt.Errorf("unsafe hard link target: %s", hdr.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create parent dir for hard link %s: %w", targetPath, err)
			}
			if err := os.Link(linkTarget, targetPath); err != nil {
				return fmt.Errorf("create hard link %s: %w", targetPath, err)
			}
		default:
		}
	}

	return nil
}

func unpackLayerTar(r io.Reader, dest string) error {
	br := bufio.NewReader(r)
	magic, _ := br.Peek(2)

	var layerReader io.Reader = br
	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gzr, err := gzip.NewReader(br)
		if err != nil {
			return fmt.Errorf("open gzip layer: %w", err)
		}
		defer gzr.Close()
		layerReader = gzr
	}

	tr := tar.NewReader(layerReader)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read layer tar entry: %w", err)
		}

		cleanName := filepath.Clean(hdr.Name)
		if cleanName == "." || cleanName == "/" {
			continue
		}
		if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			return fmt.Errorf("unsafe layer entry path: %s", hdr.Name)
		}

		targetPath := filepath.Join(dest, cleanName)
		if !strings.HasPrefix(targetPath, dest+string(os.PathSeparator)) && targetPath != dest {
			return fmt.Errorf("unsafe layer entry target: %s", hdr.Name)
		}

		base := filepath.Base(cleanName)
		parent := filepath.Dir(targetPath)

		if base == ".wh..wh..opq" {
			entries, err := os.ReadDir(parent)
			if err == nil {
				for _, entry := range entries {
					_ = os.RemoveAll(filepath.Join(parent, entry.Name()))
				}
			}
			continue
		}
		if strings.HasPrefix(base, ".wh.") {
			whiteoutTarget := filepath.Join(parent, strings.TrimPrefix(base, ".wh."))
			_ = os.RemoveAll(whiteoutTarget)
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(hdr.Mode)&0o777); err != nil {
				return fmt.Errorf("create dir %s: %w", targetPath, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create parent dir for %s: %w", targetPath, err)
			}
			out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return fmt.Errorf("create file %s: %w", targetPath, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return fmt.Errorf("write file %s: %w", targetPath, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close file %s: %w", targetPath, err)
			}
		case tar.TypeSymlink:
			_ = os.RemoveAll(targetPath)
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create parent dir for symlink %s: %w", targetPath, err)
			}
			if err := os.Symlink(hdr.Linkname, targetPath); err != nil {
				return fmt.Errorf("create symlink %s: %w", targetPath, err)
			}
		case tar.TypeLink:
			linkTarget := filepath.Join(dest, filepath.Clean(hdr.Linkname))
			if !strings.HasPrefix(linkTarget, dest+string(os.PathSeparator)) && linkTarget != dest {
				return fmt.Errorf("unsafe hard link target: %s", hdr.Linkname)
			}
			_ = os.RemoveAll(targetPath)
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create parent dir for hard link %s: %w", targetPath, err)
			}
			if err := os.Link(linkTarget, targetPath); err != nil {
				return fmt.Errorf("create hard link %s: %w", targetPath, err)
			}
		default:
		}
	}

	return nil
}

func writeImageMetadata(ref, rootfs, source string) error {
	return writeImageMetadataWithConfig(ref, rootfs, source, nil, nil)
}

func writeImageMetadataWithConfig(ref, rootfs, source string, entrypoint, cmd []string) error {
	name, tag, err := ParseImageRef(ref)
	if err != nil {
		return err
	}

	absRootfs, err := filepath.Abs(rootfs)
	if err != nil {
		return fmt.Errorf("resolve rootfs path: %w", err)
	}

	info, err := os.Stat(absRootfs)
	if err != nil {
		return fmt.Errorf("stat rootfs: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("rootfs must be a directory: %s", absRootfs)
	}

	if err := os.MkdirAll(imageRefRoot, 0o755); err != nil {
		return fmt.Errorf("create image ref dir: %w", err)
	}

	image := Image{
		Name:       name,
		Tag:        tag,
		Ref:        name + ":" + tag,
		Rootfs:     absRootfs,
		Source:     source,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		Entrypoint: copyStringSlice(entrypoint),
		Cmd:        copyStringSlice(cmd),
	}

	data, err := json.MarshalIndent(image, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal image metadata: %w", err)
	}
	data = append(data, '\n')

	path := imageRefFile(name, tag)
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp image metadata: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace image metadata: %w", err)
	}

	return nil
}

func copyStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func imageRefFile(name, tag string) string {
	filename := sanitizeRef(name+"_"+tag) + ".json"
	return filepath.Join(imageRefRoot, filename)
}

func imageStoreDir(name, tag string) string {
	dirname := sanitizeRef(name + "_" + tag)
	return filepath.Join(imageStoreRoot, dirname)
}

func sanitizeRef(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, ":", "_")
	return s
}
