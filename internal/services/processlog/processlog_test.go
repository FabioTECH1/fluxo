package processlog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareFileRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "process.log")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := prepareFile(link, os.Getuid(), os.Getgid()); err == nil {
		t.Fatal("prepareFile() accepted a symlink")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "unchanged" {
		t.Fatalf("symlink target was modified: %q", data)
	}
}

func TestPrepareFileCreatesRestrictedRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "process.log")
	if err := prepareFile(path, os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("prepareFile() error = %v", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0640 {
		t.Fatalf("process log mode = %v, want regular 0640", info.Mode())
	}
}

func TestRepairFileReplacesSymlinkWithoutTouchingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "process.log")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := repairFile(path, os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("repairFile() error = %v", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0640 {
		t.Fatalf("repaired log mode = %v, want regular 0640", info.Mode())
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "unchanged" {
		t.Fatalf("symlink target was modified: %q", data)
	}
}
