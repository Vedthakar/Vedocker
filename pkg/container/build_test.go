package container

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyCopyInstructionFileIntoDirectory(t *testing.T) {
	ctx := t.TempDir()
	rootfs := t.TempDir()
	for _, name := range []string{".npmrc", "app.py", "package.json", "package-lock.json"} {
		if err := os.WriteFile(filepath.Join(ctx, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(rootfs, "app"), 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		workdir, args, want string
	}{
		{"/app", ".npmrc .", "app/.npmrc"},                    // "." is the workdir
		{"/app", "app.py ./", "app/app.py"},                   // trailing slash
		{"/", "app.py /opt/", "opt/app.py"},                   // new dir with trailing slash
		{"/", "app.py /app", "app/app.py"},                    // existing directory
		{"/", "app.py /srv/main.py", "srv/main.py"},           // explicit file name
		{"/app", "package*.json ./", "app/package-lock.json"}, // glob still works
	}
	for _, c := range cases {
		if err := applyCopyInstruction(ctx, rootfs, c.workdir, c.args); err != nil {
			t.Errorf("COPY %s (workdir %s): %v", c.args, c.workdir, err)
			continue
		}
		info, err := os.Stat(filepath.Join(rootfs, c.want))
		if err != nil || info.IsDir() {
			t.Errorf("COPY %s (workdir %s): expected file at %s, got %v", c.args, c.workdir, c.want, err)
		}
	}
}

func TestUseHostResolvConfRestoresImageFile(t *testing.T) {
	if _, err := os.Stat("/etc/resolv.conf"); err != nil {
		t.Skip("host has no /etc/resolv.conf")
	}
	host, _ := os.ReadFile("/etc/resolv.conf")

	// Image with its own resolv.conf: host copy during RUN, original after.
	rootfs := t.TempDir()
	os.MkdirAll(filepath.Join(rootfs, "etc"), 0o755)
	conf := filepath.Join(rootfs, "etc", "resolv.conf")
	os.WriteFile(conf, []byte("image"), 0o644)
	restore, err := useHostResolvConf(rootfs)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(conf); string(got) != string(host) {
		t.Fatalf("during RUN got %q, want host resolv.conf", got)
	}
	restore()
	if got, _ := os.ReadFile(conf); string(got) != "image" {
		t.Fatalf("after RUN got %q, want the image's original", got)
	}

	// Image without one: nothing left behind.
	empty := t.TempDir()
	restore, err = useHostResolvConf(empty)
	if err != nil {
		t.Fatal(err)
	}
	restore()
	if _, err := os.Lstat(filepath.Join(empty, "etc", "resolv.conf")); !os.IsNotExist(err) {
		t.Fatalf("resolv.conf should not be baked into the image, got %v", err)
	}

	// Symlink pointing outside the rootfs must not be written through.
	outside := filepath.Join(t.TempDir(), "victim")
	os.WriteFile(outside, []byte("untouched"), 0o644)
	linked := t.TempDir()
	os.MkdirAll(filepath.Join(linked, "etc"), 0o755)
	os.Symlink(outside, filepath.Join(linked, "etc", "resolv.conf"))
	restore, err = useHostResolvConf(linked)
	if err != nil {
		t.Fatal(err)
	}
	restore()
	if got, _ := os.ReadFile(outside); string(got) != "untouched" {
		t.Fatalf("wrote through symlink outside rootfs: %q", got)
	}
	if dst, err := os.Readlink(filepath.Join(linked, "etc", "resolv.conf")); err != nil || dst != outside {
		t.Fatalf("symlink not restored: %q %v", dst, err)
	}
}
