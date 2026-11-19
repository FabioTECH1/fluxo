package nodetoolchain

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestVerificationCacheIsPrivateImmutableAndIgnoresProjectGrowth(t *testing.T) {
	oldP, oldY := PinnedPNPMVersion, PinnedYarnVersion
	PinnedPNPMVersion, PinnedYarnVersion = "11.20.0", "4.18.0"
	t.Cleanup(func() { PinnedPNPMVersion, PinnedYarnVersion = oldP, oldY })
	source := t.TempDir()
	paths, err := activeCachePaths()
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range paths {
		path := filepath.Join(source, relative)
		if relative != "lastKnownGood.json" {
			path = filepath.Join(path, "manager.js")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(relative), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cache, cleanup, err := verificationCache(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	info, err := os.Stat(cache)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("unverified copy exposed: %v %v", info, err)
	}
	for _, relative := range paths {
		p := filepath.Join(cache, relative)
		if relative != "lastKnownGood.json" {
			p = filepath.Join(p, "manager.js")
		}
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm()&0222 != 0 {
			t.Fatalf("writable verification file: %v %v", info, err)
		}
	}
	before, err := preservationDigest(filepath.Join(cache, "v1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "v1/pnpm/project-version"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "v1/pnpm/project-version/unrelated"), []byte("project cache"), 0644); err != nil {
		t.Fatal(err)
	}
	other, cleanupOther, err := verificationCache(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupOther()
	after, err := preservationDigest(filepath.Join(other, "v1"))
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("unrelated cache growth changed preservation evidence")
	}
	// A later source mutation cannot change the copy used for execution.
	path := filepath.Join(source, paths[1], "manager.js")
	if err := os.WriteFile(path, []byte("changed live cache"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := preservationDigest(filepath.Join(cache, "v1"))
	if err != nil || got != before {
		t.Fatal("live cache mutation changed immutable probe", err)
	}
}
