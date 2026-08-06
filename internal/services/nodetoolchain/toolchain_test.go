package nodetoolchain

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestVersionAtLeast(t *testing.T) {
	tests := []struct {
		current string
		minimum string
		want    bool
	}{
		{"v24.1.0", "22.13.0", true},
		{"22.13.0", "22.13.0", true},
		{"22.12.9", "22.13.0", false},
		{"24.1", "22.13.0", false},
		{"24.1.0 unexpected", "22.13.0", false},
	}
	for _, test := range tests {
		if got := versionAtLeast(test.current, test.minimum); got != test.want {
			t.Fatalf("versionAtLeast(%q, %q) = %v, want %v", test.current, test.minimum, got, test.want)
		}
	}
}

func TestSelectChecksumRequiresExactFilename(t *testing.T) {
	checksum := strings.Repeat("a", 64)
	contents := checksum + "  prefix-bun-linux-x64.zip\n" + checksum + "  bun-linux-x64.zip\n"
	got, err := selectChecksum(contents, "bun-linux-x64.zip")
	if err != nil {
		t.Fatal(err)
	}
	if got != checksum {
		t.Fatalf("selectChecksum() = %q, want %q", got, checksum)
	}
	if _, err := selectChecksum(contents, "linux-x64.zip"); err == nil {
		t.Fatal("selectChecksum accepted a partial filename")
	}
}

func TestSelectLatestNodeLTS(t *testing.T) {
	contents := `[
		{"version":"v26.1.0","lts":false,"files":["linux-x64"]},
		{"version":"v24.3.0","lts":"Krypton","files":["linux-x64","linux-arm64"]},
		{"version":"v24.4.1","lts":"Krypton","files":["linux-x64"]},
		{"version":"v22.20.0","lts":"Jod","files":["linux-arm64"]}
	]`
	version, err := selectLatestNodeLTS(contents, "x64")
	if err != nil {
		t.Fatal(err)
	}
	if version != "24.4.1" {
		t.Fatalf("selectLatestNodeLTS() = %q, want 24.4.1", version)
	}
	version, err = selectLatestNodeLTS(contents, "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if version != "24.3.0" {
		t.Fatalf("selectLatestNodeLTS() = %q, want 24.3.0", version)
	}
}

func TestPinnedPackageSpec(t *testing.T) {
	tests := []struct {
		name, pinned, fallback, want string
	}{
		{name: "pnpm", pinned: "10.15.1", fallback: "latest", want: "pnpm@10.15.1"},
		{name: "yarn", pinned: "v4.9.2", fallback: "stable", want: "yarn@4.9.2"},
		{name: "corepack", fallback: "latest", want: "corepack@latest"},
	}
	for _, test := range tests {
		if got := pinnedPackageSpec(test.name, test.pinned, test.fallback); got != test.want {
			t.Fatalf("pinnedPackageSpec(%q, %q, %q) = %q, want %q", test.name, test.pinned, test.fallback, got, test.want)
		}
	}
}

func TestValidatedPinnedVersion(t *testing.T) {
	for _, test := range []struct {
		value, want string
		wantErr     bool
	}{
		{value: "", want: ""},
		{value: "v24.19.0", want: "24.19.0"},
		{value: " 1.3.14 ", want: "1.3.14"},
		{value: "24", wantErr: true},
		{value: "latest", wantErr: true},
	} {
		got, err := validatedPinnedVersion(test.value, "test tool")
		if test.wantErr {
			if err == nil {
				t.Fatalf("validatedPinnedVersion(%q) error = nil, want error", test.value)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Fatalf("validatedPinnedVersion(%q) = %q, %v; want %q, nil", test.value, got, err, test.want)
		}
	}
}

func TestValidatedPinnedChecksum(t *testing.T) {
	checksum := strings.Repeat("a", 64)
	if got, err := validatedPinnedChecksum(checksum, "test"); err != nil || got != checksum {
		t.Fatalf("validatedPinnedChecksum() = %q, %v", got, err)
	}
	for _, invalid := range []string{"", "latest", strings.Repeat("a", 62)} {
		if _, err := validatedPinnedChecksum(invalid, "test"); err == nil {
			t.Fatalf("validatedPinnedChecksum(%q) accepted invalid metadata", invalid)
		}
	}
}

func TestVersionsEqual(t *testing.T) {
	if !versionsEqual("v24.19.0", "24.19.0") {
		t.Fatal("equivalent versions did not match")
	}
	if versionsEqual("24.19.1", "24.19.0") || versionsEqual("latest", "24.19.0") {
		t.Fatal("different or invalid versions matched")
	}
}

func TestManagedPathBoundary(t *testing.T) {
	if !isManagedPath("/opt/fluxo/node/current/bin/node") {
		t.Fatal("expected Fluxo Node path to be managed")
	}
	if isManagedPath("/opt/fluxo/node-backup/bin/node") {
		t.Fatal("managed path check crossed a directory boundary")
	}
}

func TestWriteInstallSnapshotManifest(t *testing.T) {
	root := t.TempDir()
	manifest := []byte(`{"node":{"existed":true,"managed":true,"target":"/opt/fluxo/node/current/bin/node"}}`)
	if err := writeInstallSnapshotManifest(root, manifest); err != nil {
		t.Fatalf("writeInstallSnapshotManifest() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "links.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(manifest) {
		t.Fatalf("manifest = %q, want %q", data, manifest)
	}
	info, err := os.Stat(filepath.Join(root, "links.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("manifest mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestParseManagedStateRejectsCorruptMarker(t *testing.T) {
	state, valid := parseManagedState([]byte(`{"official_node":true,"bun":true}`))
	if !valid {
		t.Fatal("valid state marker was rejected")
	}
	if !state.OfficialNode || !state.Bun || state.Corepack {
		t.Fatalf("unexpected decoded state: %+v", state)
	}
	if _, valid := parseManagedState([]byte(`{"official_node":`)); valid {
		t.Fatal("corrupt state marker was accepted")
	}
}

func TestNodeReleaseUsableRejectsMissingNPMFiles(t *testing.T) {
	root := t.TempDir()
	nodePath := filepath.Join(root, "bin", "node")
	if err := os.MkdirAll(filepath.Dir(nodePath), 0755); err != nil {
		t.Fatal(err)
	}
	fakeNode := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo v24.19.0; else echo 11.0.0; fi\n"
	if err := os.WriteFile(nodePath, []byte(fakeNode), 0755); err != nil {
		t.Fatal(err)
	}
	npmBin := filepath.Join(root, "lib", "node_modules", "npm", "bin")
	if err := os.MkdirAll(npmBin, 0755); err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{"npm-cli.js", "npx-cli.js"} {
		if err := os.WriteFile(filepath.Join(npmBin, script), []byte("placeholder"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if !nodeReleaseUsable(context.Background(), root, "24.19.0") {
		t.Fatal("expected complete Node.js release to be usable")
	}
	if err := os.Remove(filepath.Join(npmBin, "npm-cli.js")); err != nil {
		t.Fatal(err)
	}
	if nodeReleaseUsable(context.Background(), root, "24.19.0") {
		t.Fatal("release with missing npm CLI was accepted")
	}
}

func TestDownloadProgressReaderReportsLargeDownloads(t *testing.T) {
	const size = 5 << 20
	lastActivity := &atomic.Int64{}
	lastActivity.Store(time.Now().Add(-time.Minute).UnixNano())
	var messages []string
	reader := &downloadProgressReader{
		reader:        strings.NewReader(strings.Repeat("x", size)),
		contentLength: size,
		lastActivity:  lastActivity,
		progress: func(message string) {
			messages = append(messages, message)
		},
		label:       "Node.js",
		nextPercent: 25,
	}
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(messages, "\n")
	for _, checkpoint := range []string{"25%", "50%", "75%", "100%"} {
		if !strings.Contains(joined, checkpoint) {
			t.Fatalf("progress did not include %s: %q", checkpoint, joined)
		}
	}
	if lastActivity.Load() <= time.Now().Add(-time.Minute).UnixNano() {
		t.Fatal("download activity timestamp was not updated")
	}
}

func TestDownloadRejectsPlainHTTP(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "download")
	err := downloadFileWithProgress(context.Background(), "http://example.com/archive", destination, 1024, nil, "test")
	if err == nil || !strings.Contains(err.Error(), "non-HTTPS") {
		t.Fatalf("downloadFile() error = %v, want non-HTTPS rejection", err)
	}
}

func TestDescribeNetworkError(t *testing.T) {
	dnsErr := &net.DNSError{Name: "nodejs.org", Err: "temporary failure"}
	if message := describeNetworkError("nodejs.org", dnsErr).Error(); !strings.Contains(message, "DNS lookup for nodejs.org failed") {
		t.Fatalf("DNS error = %q", message)
	}
	if message := describeNetworkError("nodejs.org", context.DeadlineExceeded).Error(); !strings.Contains(message, "timed out") {
		t.Fatalf("timeout error = %q", message)
	}
}

func TestRetryableNetworkErrors(t *testing.T) {
	for _, err := range []error{
		context.DeadlineExceeded,
		io.ErrUnexpectedEOF,
		fmt.Errorf("wrapped: %w", syscall.ECONNRESET),
	} {
		if !isRetryableNetworkError(err) {
			t.Fatalf("expected %v to be retryable", err)
		}
	}
	if isRetryableNetworkError(context.Canceled) || isRetryableNetworkError(errors.New("checksum mismatch")) {
		t.Fatal("non-network error was considered retryable")
	}
}

func TestValidateOwnedTree(t *testing.T) {
	makeTree := func(t *testing.T) (string, int, int) {
		t.Helper()
		root := t.TempDir()
		if err := os.Chmod(root, 0755); err != nil {
			t.Fatal(err)
		}
		child := filepath.Join(root, "bin")
		if err := os.Mkdir(child, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(child, "node"), []byte("binary"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(child, "node"), filepath.Join(root, "current")); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(root)
		if err != nil {
			t.Fatal(err)
		}
		owner := info.Sys().(*syscall.Stat_t)
		return root, int(owner.Uid), int(owner.Gid)
	}

	t.Run("accepts secure accessible tree", func(t *testing.T) {
		root, uid, gid := makeTree(t)
		if err := validateOwnedTree(root, uid, gid, true); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("rejects writable files", func(t *testing.T) {
		root, uid, gid := makeTree(t)
		if err := os.Chmod(filepath.Join(root, "bin", "node"), 0775); err != nil {
			t.Fatal(err)
		}
		if err := validateOwnedTree(root, uid, gid, true); err == nil || !strings.Contains(err.Error(), "group or world writable") {
			t.Fatalf("validateOwnedTree() error = %v, want writable-file rejection", err)
		}
	})

	t.Run("rejects unexpected ownership", func(t *testing.T) {
		root, uid, gid := makeTree(t)
		if err := validateOwnedTree(root, uid+1, gid, true); err == nil || !strings.Contains(err.Error(), "expected") {
			t.Fatalf("validateOwnedTree() error = %v, want ownership rejection", err)
		}
	})

	t.Run("rejects inaccessible directories", func(t *testing.T) {
		root, uid, gid := makeTree(t)
		if err := os.Chmod(filepath.Join(root, "bin"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := validateOwnedTree(root, uid, gid, true); err == nil || !strings.Contains(err.Error(), "cannot be traversed") {
			t.Fatalf("validateOwnedTree() error = %v, want traversal rejection", err)
		}
	})

	t.Run("rejects symlinks outside root", func(t *testing.T) {
		root, uid, gid := makeTree(t)
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(outside, []byte("external"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(root, "current")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "current")); err != nil {
			t.Fatal(err)
		}
		if err := validateOwnedTree(root, uid, gid, true); err == nil || !strings.Contains(err.Error(), "points outside") {
			t.Fatalf("validateOwnedTree() error = %v, want external-symlink rejection", err)
		}
	})
}

func TestManagedLinksForState(t *testing.T) {
	got := strings.Join(managedLinksForState(managedState{OfficialNode: true, Corepack: true, Bun: true}), ",")
	want := "node,npm,npx,corepack,pnpm,pnpx,yarn,yarnpkg,bun,bunx"
	if got != want {
		t.Fatalf("managedLinksForState() = %q, want %q", got, want)
	}
}

func TestValidateExecutableTarget(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(binDir, "node")
	if err := os.WriteFile(executable, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "node")
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	if err := validateExecutableTarget(link, root); err != nil {
		t.Fatalf("validateExecutableTarget() error = %v, want success", err)
	}

	if err := os.Chmod(executable, 0644); err != nil {
		t.Fatal(err)
	}
	if err := validateExecutableTarget(link, root); err == nil || !strings.Contains(err.Error(), "executable file") {
		t.Fatalf("validateExecutableTarget() error = %v, want executable rejection", err)
	}

	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := validateExecutableTarget(link, root); err == nil || !strings.Contains(err.Error(), "resolves outside") {
		t.Fatalf("validateExecutableTarget() error = %v, want external-target rejection", err)
	}
}
