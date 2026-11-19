package nodetoolchain

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPreservationDigestSeparatesLegacyFramingCollision(t *testing.T) {
	original, merged := t.TempDir(), t.TempDir()
	for name, body := range map[string]string{"a": "first", "b": "second"} {
		if err := os.WriteFile(filepath.Join(original, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Lstat(filepath.Join(original, "b"))
	if err != nil {
		t.Fatal(err)
	}
	// Absorb b's complete encoded entry into a's raw bytes, then remove b.
	body := fmt.Sprintf("first\x00b\x00%d\x00second", info.Mode())
	if err := os.WriteFile(filepath.Join(merged, "a"), []byte(body), info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	oldA, err := TreeDigest(original)
	if err != nil {
		t.Fatal(err)
	}
	oldB, err := TreeDigest(merged)
	if err != nil {
		t.Fatal(err)
	}
	if oldA != oldB {
		t.Fatal("fixture does not demonstrate the legacy framing collision")
	}
	if digestPreservationTest(t, original) == digestPreservationTest(t, merged) {
		t.Fatal("framed digest accepted the legacy collision")
	}
}

func TestPreservationDigestBindsEntries(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(string) error
	}{
		{"bytes", func(root string) error {
			return os.WriteFile(filepath.Join(root, "bin/node"), []byte("tampered"), 0755)
		}},
		{"file permissions", func(root string) error {
			return os.Chmod(filepath.Join(root, "bin/node"), 0644)
		}},
		{"directory permissions", func(root string) error {
			return os.Chmod(filepath.Join(root, "bin"), 0700)
		}},
		{"path", func(root string) error {
			return os.Rename(filepath.Join(root, "bin/node"), filepath.Join(root, "bin/other"))
		}},
		{"type", func(root string) error {
			if err := os.Remove(filepath.Join(root, "empty")); err != nil {
				return err
			}
			return os.Mkdir(filepath.Join(root, "empty"), 0644)
		}},
		{"symlink target", func(root string) error {
			if err := os.Remove(filepath.Join(root, "alias")); err != nil {
				return err
			}
			return os.Symlink("empty", filepath.Join(root, "alias"))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "bin"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "bin/node"), []byte("original"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "empty"), nil, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("bin/node", filepath.Join(root, "alias")); err != nil {
				t.Fatal(err)
			}
			before := digestPreservationTest(t, root)
			if err := test.change(root); err != nil {
				t.Fatal(err)
			}
			if before == digestPreservationTest(t, root) {
				t.Fatal("entry change was not bound")
			}
		})
	}
}

func TestPreservationDigestFileRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "lastKnownGood.json")
	if err := os.WriteFile(root, []byte(`{"pnpm":"10.0.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	before := digestPreservationTest(t, root)
	if len(before) != 64 || before != digestPreservationTest(t, root) {
		t.Fatal("file-root digest is not a stable SHA-256")
	}
	if err := os.WriteFile(root, []byte(`{"pnpm":"10.0.1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if before == digestPreservationTest(t, root) {
		t.Fatal("file-root content change was not bound")
	}
}

func TestPreservationDigestDeterministicOrdering(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	names := []string{"z", "a\nline", "invalid-\xff"}
	for i, name := range names {
		for root, entry := range map[string]string{a: name, b: names[len(names)-1-i]} {
			if err := os.WriteFile(filepath.Join(root, entry), []byte(entry), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if digestPreservationTest(t, a) != digestPreservationTest(t, b) {
		t.Fatal("digest depends on root path or entry creation order")
	}
}

func TestPreservationDigestPreservesNonUTF8Bytes(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(fmt.Sprintf("symlink=%t", symlink), func(t *testing.T) {
			a, b := t.TempDir(), t.TempDir()
			for root, raw := range map[string]string{a: "\xff", b: "\xfe"} {
				var err error
				if symlink {
					err = os.Symlink(raw, filepath.Join(root, "link"))
				} else {
					err = os.WriteFile(filepath.Join(root, raw), []byte("same"), 0644)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if digestPreservationTest(t, a) == digestPreservationTest(t, b) {
				t.Fatal("distinct non-UTF-8 bytes collapsed")
			}
		})
	}
}

func TestPreservationDigestDoesNotFollowSymlinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	file := filepath.Join(outside, "file")
	if err := os.WriteFile(file, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"directory": outside, "file": file, "dangling": filepath.Join(outside, "missing")} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	before := digestPreservationTest(t, root)
	linkRoot := filepath.Join(root, "directory")
	linkBefore := digestPreservationTest(t, linkRoot)
	if err := os.WriteFile(file, []byte("changed outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(outside, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	if before != digestPreservationTest(t, root) || linkBefore != digestPreservationTest(t, linkRoot+"/") {
		t.Fatal("digest followed a symlink")
	}
}

func TestPreservationDigestRejectsSpecialFiles(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, fifo, filepath.Join(root, "missing")} {
		if digest, err := preservationDigest(path); err == nil || digest != "" {
			t.Fatalf("unsupported path %s returned %q, %v", path, digest, err)
		}
	}
}

func digestPreservationTest(t *testing.T, root string) string {
	t.Helper()
	digest, err := preservationDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}
