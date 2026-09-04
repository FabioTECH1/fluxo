package pythontoolchain

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestVersionAtLeast(t *testing.T) {
	tests := []struct {
		current string
		minimum string
		want    bool
	}{
		{"3.10.0", "3.10.0", true},
		{"3.12.4", "3.10.0", true},
		{"3.9.18", "3.10.0", false},
		{"Python 3.12.0", "3.10.0", false},
	}
	for _, test := range tests {
		if got := versionAtLeast(test.current, test.minimum); got != test.want {
			t.Fatalf("versionAtLeast(%q, %q) = %v, want %v", test.current, test.minimum, got, test.want)
		}
	}
}

func TestExtractUVArchiveOnlyExtractsExpectedExecutables(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "uv.tar.gz")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	for name, content := range map[string]string{
		"uv-x86_64-unknown-linux-gnu/uv":  "uv binary",
		"uv-x86_64-unknown-linux-gnu/uvx": "uvx binary",
		"../../outside":                   "must not escape",
	} {
		header := &tar.Header{Name: name, Mode: 0755, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(dir, "extract")
	if err := os.Mkdir(destination, 0755); err != nil {
		t.Fatal(err)
	}
	if err := extractUVArchive(archivePath, destination); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"uv", "uvx"} {
		if _, err := os.Stat(filepath.Join(destination, name)); err != nil {
			t.Fatalf("expected %s to be extracted: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "outside")); !os.IsNotExist(err) {
		t.Fatalf("unexpected archive path escaped destination: %v", err)
	}
}

func TestExtractUVArchiveRequiresBothBinaries(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "uv.tar.gz")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	content := "uv binary"
	if err := tw.WriteHeader(&tar.Header{Name: "release/uv", Mode: 0755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(dir, "extract")
	if err := os.Mkdir(destination, 0755); err != nil {
		t.Fatal(err)
	}
	if err := extractUVArchive(archivePath, destination); err == nil {
		t.Fatal("expected an archive missing uvx to be rejected")
	}
}

func TestPathWithin(t *testing.T) {
	if !pathWithin("/opt/fluxo/python-toolchain/uv-1/uv", managedRoot) {
		t.Fatal("managed path was not recognized")
	}
	if pathWithin("/opt/fluxo/python-toolchain-old/uv", managedRoot) {
		t.Fatal("sibling path was treated as managed")
	}
}

func TestCommandVersionSelectsSemanticVersion(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "uv-test")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nprintf '%s\\n' 'uv 0.12.9 (abc123 2026-09-01)'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got := commandVersion(context.Background(), "uv-test"); got != "0.12.9" {
		t.Fatalf("commandVersion() = %q, want 0.12.9", got)
	}
}

func TestCommandVersionRejectsUnparseableOutput(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "uv-test")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nprintf '%s\\n' 'uv version unavailable'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got := commandVersion(context.Background(), "uv-test"); got != "" {
		t.Fatalf("commandVersion() = %q, want empty", got)
	}
}
