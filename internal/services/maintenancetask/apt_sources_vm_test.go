package maintenancetask

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/syscmd"
	"golang.org/x/sys/unix"
)

func TestVMAPTSourceBackupSelectionAndProtectedInventory(t *testing.T) {
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || host != "fluxo-bridge-noble-stale-proof-20261117" {
		t.Skip("named root fixture and explicit opt-in required")
	}
	self, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := os.Readlink("/proc/1/ns/mnt")
	if err != nil || self == manager {
		t.Fatal("private mount namespace required before fixture writes/mounts")
	}
	if err = unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp("/root", "fluxo-apt-source-selection-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	parts := filepath.Join(root, "parts")
	for _, dir := range []string{parts, filepath.Join(root, "lists"), filepath.Join(root, "cache")} {
		if err = os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(name, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "main"), "")
	write(filepath.Join(root, "status"), "")
	write(filepath.Join(parts, "fixture.sources"), "Types: deb\nURIs: https://example.invalid/ubuntu\nSuites: noble\nComponents: main\n")
	const backup = "ubuntu.sources.curtin.orig"
	write(filepath.Join(parts, backup), "this is deliberately not valid source syntax\n")
	if err = unix.Mount(parts, "/etc/apt/sources.list.d", "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	defer unix.Unmount("/etc/apt/sources.list.d", 0)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	inspect := func() (profile, error) {
		p := newProfile(PackageCleanup)
		err := inspectAPTSourceInputs(&p)
		return p, err
	}
	p, err := inspect()
	if err != nil {
		t.Fatal(err)
	}
	key := "/etc/apt/sources.list.d/" + backup
	if p.Files[key] != digest([]byte("this is deliberately not valid source syntax\n")) || !strings.Contains(strings.Join(p.Directories["/etc/apt/sources.list.d"], ","), backup) {
		t.Fatal("ignored backup lost protected inventory evidence")
	}
	// Native APT's read-only index query is an independent filename-selection
	// oracle. All mutable cache/status/list paths are private test paths.
	args := []string{"-o", "Dir::Etc::sourcelist=" + filepath.Join(root, "main"), "-o", "Dir::State::status=" + filepath.Join(root, "status"), "-o", "Dir::State::lists=" + filepath.Join(root, "lists"), "-o", "Dir::Cache=" + filepath.Join(root, "cache"), "-o", "Dir::Cache::pkgcache=", "-o", "Dir::Cache::srcpkgcache=", "indextargets", "--no-release-info"}
	out, err := syscmd.Run(ctx, 30*time.Second, "/usr/bin/apt-get", args...)
	if err != nil || !strings.Contains(out, "example.invalid/ubuntu") {
		t.Fatalf("native APT did not accept the inert backup: %v", err)
	}
	if _, err = qualifyProfile(ctx, PackageCleanup); err != nil {
		t.Fatalf("full current cleanup qualification with inert backup: %v", err)
	}
	write(filepath.Join(parts, backup), "changed inert backup\n")
	changed, err := inspect()
	if err != nil || changed.Files[key] == p.Files[key] {
		t.Fatal("backup content drift was not recorded")
	}
	if err = os.Rename(filepath.Join(parts, backup), filepath.Join(parts, "backup.orig.sources")); err != nil {
		t.Fatal(err)
	}
	if _, err = inspect(); err == nil {
		t.Fatal("active source bypassed validation through backup-looking prefix")
	}
	if _, err = syscmd.Run(ctx, 30*time.Second, "/usr/bin/apt-get", args...); err == nil {
		t.Fatal("native APT did not parse the renamed active source")
	}
	if err = os.Remove(filepath.Join(parts, "backup.orig.sources")); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"symlink", "directory", "fifo", "writable", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(parts, "unsafe.orig")
			switch kind {
			case "symlink":
				err = os.Symlink(filepath.Join(parts, "fixture.sources"), path)
			case "directory":
				err = os.Mkdir(path, 0700)
			case "fifo":
				err = unix.Mkfifo(path, 0600)
			case "writable":
				write(path, "backup")
				err = os.Chmod(path, 0666)
			case "hardlink":
				err = os.Link(filepath.Join(parts, "fixture.sources"), path)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(path)
			if _, err = inspect(); err == nil {
				t.Fatal("unsafe ignored entry was accepted")
			}
		})
	}
	t.Log("native APT final-extension selection, current cleanup qualification, retained backup drift evidence and unsafe-file refusals passed; no package transaction or authority writes")
}
