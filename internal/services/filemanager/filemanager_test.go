package filemanager

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

type gatedReader struct {
	data    string
	started chan struct{}
	proceed chan struct{}
	once    sync.Once
}

func (r *gatedReader) Read(buffer []byte) (int, error) {
	r.once.Do(func() {
		close(r.started)
		<-r.proceed
	})
	if r.data == "" {
		return 0, io.EOF
	}
	written := copy(buffer, r.data)
	r.data = r.data[written:]
	return written, nil
}

func testManager(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	manager, err := newManager(root, os.Getuid(), os.Getgid())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	return manager, root
}

func TestNormalizePathRejectsEscapes(t *testing.T) {
	invalid := []string{"../outside", "a/../../outside", "/etc/passwd", `dir\file`, "bad\x00name", "line\nbreak"}
	for _, value := range invalid {
		if _, err := NormalizePath(value); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("NormalizePath(%q) error = %v, want ErrInvalidPath", value, err)
		}
	}
	for input, expected := range map[string]string{"": ".", "/": ".", "assets/../index.html": "index.html", "public/css": "public/css"} {
		actual, err := NormalizePath(input)
		if err != nil || actual != expected {
			t.Errorf("NormalizePath(%q) = %q, %v; want %q", input, actual, err, expected)
		}
	}
}

func TestManagerLifecycleAndEditConflict(t *testing.T) {
	manager, root := testManager(t)
	if err := manager.Create("public", "directory"); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := manager.Create("public/index.txt", "file"); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := os.Chmod(filepath.Join(root, "public", "index.txt"), 0755); err != nil {
		t.Fatal(err)
	}
	file, err := manager.ReadText("public/index.txt")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if err := manager.WriteText("public/index.txt", "hello\n", file.SHA256); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := manager.WriteText("public/index.txt", "stale\n", file.SHA256); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale write error = %v, want ErrConflict", err)
	}
	contents, err := os.ReadFile(filepath.Join(root, "public", "index.txt"))
	if err != nil || string(contents) != "hello\n" {
		t.Fatalf("saved contents = %q, %v", contents, err)
	}
	info, err := os.Stat(filepath.Join(root, "public", "index.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("edited file mode = %v; want 0755", info.Mode().Perm())
	}
	if err := manager.Move("public/index.txt", "public/home.txt"); err != nil {
		t.Fatalf("move file: %v", err)
	}
	if err := manager.Delete("public"); !errors.Is(err, ErrDirectoryNotEmpty) {
		t.Fatalf("delete non-empty directory error = %v, want ErrDirectoryNotEmpty", err)
	}
	if err := manager.Delete("public/home.txt"); err != nil {
		t.Fatalf("delete file: %v", err)
	}
	if err := manager.Delete("public"); err != nil {
		t.Fatalf("delete empty directory: %v", err)
	}
}

func TestManagerContainsSymlinks(t *testing.T) {
	manager, root := testManager(t)
	if err := os.WriteFile(filepath.Join(root, "inside.txt"), []byte("safe"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("inside.txt", filepath.Join(root, "internal-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "inside.txt"), filepath.Join(root, "absolute-internal-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "external-link")); err != nil {
		t.Fatal(err)
	}

	text, err := manager.ReadText("internal-link")
	if err != nil || text.Content != "safe" {
		t.Fatalf("read internal symlink = %q, %v", text.Content, err)
	}
	if _, err := manager.ReadText("external-link"); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("read external symlink error = %v, want ErrUnsafePath", err)
	}
	text, err = manager.ReadText("absolute-internal-link")
	if err != nil || text.Content != "safe" {
		t.Fatalf("read absolute internal symlink = %q, %v", text.Content, err)
	}
	listing, err := manager.List(".", true, 0, DefaultPageSize)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	foundUnsafe := false
	foundInternal := false
	for _, entry := range listing.Entries {
		if entry.Name == "external-link" {
			foundUnsafe = entry.IsSymlink && entry.UnsafeSymlink && !entry.Editable
		}
		if entry.Name == "internal-link" {
			foundInternal = entry.IsSymlink && entry.IsFile && !entry.UnsafeSymlink && !entry.Editable
		}
	}
	if !foundUnsafe {
		t.Fatal("external symlink was not marked unsafe")
	}
	if !foundInternal {
		t.Fatal("internal symlink was not identified without enabling edits")
	}
	if err := manager.WriteText("internal-link", "replace", strings.Repeat("0", 64)); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("write through symlink error = %v, want ErrUnsafePath", err)
	}
}

func TestUploadDoesNotOverwriteByDefault(t *testing.T) {
	manager, root := testManager(t)
	if err := manager.Upload(".", "asset.txt", strings.NewReader("first"), false); err != nil {
		t.Fatalf("first upload: %v", err)
	}
	if err := os.Chmod(filepath.Join(root, "asset.txt"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := manager.Upload(".", "asset.txt", strings.NewReader("second"), false); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate upload error = %v, want ErrConflict", err)
	}
	if err := manager.Upload(".", "asset.txt", strings.NewReader("second"), true); err != nil {
		t.Fatalf("overwrite upload: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(root, "asset.txt"))
	if err != nil || string(contents) != "second" {
		t.Fatalf("uploaded contents = %q, %v", contents, err)
	}
	info, err := os.Stat(filepath.Join(root, "asset.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0750 {
		t.Fatalf("overwritten file mode = %v; want 0750", info.Mode().Perm())
	}
}

func TestAbsoluteInternalDirectorySymlinkSupportsZeroDowntimeLayout(t *testing.T) {
	manager, root := testManager(t)
	release := filepath.Join(root, "releases", "20260721010101")
	if err := os.MkdirAll(release, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(release, filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	if err := manager.Create("current/index.txt", "file"); err != nil {
		t.Fatalf("create through current symlink: %v", err)
	}
	if err := manager.Upload("current", "asset.txt", strings.NewReader("asset"), false); err != nil {
		t.Fatalf("upload through current symlink: %v", err)
	}
	listing, err := manager.List("current", true, 0, DefaultPageSize)
	if err != nil {
		t.Fatalf("list current symlink: %v", err)
	}
	if listing.Total != 2 {
		t.Fatalf("current listing total = %d, want 2", listing.Total)
	}
	contents, err := os.ReadFile(filepath.Join(release, "asset.txt"))
	if err != nil || string(contents) != "asset" {
		t.Fatalf("release asset contents = %q, %v", contents, err)
	}
}

func TestConcurrentTextEditsDoNotBothOverwrite(t *testing.T) {
	manager, root := testManager(t)
	filePath := filepath.Join(root, "shared.txt")
	if err := os.WriteFile(filePath, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	file, err := manager.ReadText("shared.txt")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, content := range []string{"first", "second"} {
		content := content
		go func() {
			<-start
			results <- manager.WriteText("shared.txt", content, file.SHA256)
		}()
	}
	close(start)
	firstResult := <-results
	secondResult := <-results
	successes := 0
	conflicts := 0
	for _, result := range []error{firstResult, secondResult} {
		if result == nil {
			successes++
		} else if errors.Is(result, ErrConflict) {
			conflicts++
		} else {
			t.Fatalf("concurrent edit returned unexpected error: %v", result)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent edit results: successes=%d conflicts=%d; want one each", successes, conflicts)
	}
	mutationLocks.Lock()
	defer mutationLocks.Unlock()
	if len(mutationLocks.items) != 0 {
		t.Fatalf("mutation lock registry retained %d entries", len(mutationLocks.items))
	}
}

func TestConcurrentTextEditsThroughSymlinkAliasesConflict(t *testing.T) {
	manager, root := testManager(t)
	release := filepath.Join(root, "releases", "one")
	if err := os.MkdirAll(release, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, "shared.txt"), []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(release, filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	file, err := manager.ReadText("current/shared.txt")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	paths := []string{"current/shared.txt", "releases/one/shared.txt"}
	for index, relative := range paths {
		go func(index int, relative string) {
			<-start
			results <- manager.WriteText(relative, string(rune('a'+index)), file.SHA256)
		}(index, relative)
	}
	close(start)
	successes := 0
	conflicts := 0
	for range paths {
		result := <-results
		if result == nil {
			successes++
		} else if errors.Is(result, ErrConflict) {
			conflicts++
		} else {
			t.Fatalf("aliased edit returned unexpected error: %v", result)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("aliased edit results: successes=%d conflicts=%d; want one each", successes, conflicts)
	}
}

func TestAtomicWriteRechecksHashAfterPreparingTempFile(t *testing.T) {
	manager, root := testManager(t)
	filePath := filepath.Join(root, "changing.txt")
	if err := os.WriteFile(filePath, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256([]byte("original"))
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFD)
	reader := &gatedReader{data: "replacement", started: make(chan struct{}), proceed: make(chan struct{})}
	result := make(chan error, 1)
	go func() {
		result <- manager.atomicWrite(rootFD, "changing.txt", reader, MaxTextBytes, true, nil, hex.EncodeToString(expected[:]))
	}()
	<-reader.started
	if err := os.WriteFile(filePath, []byte("external change"), 0644); err != nil {
		t.Fatal(err)
	}
	close(reader.proceed)
	if err := <-result; !errors.Is(err, ErrConflict) {
		t.Fatalf("atomic write error = %v, want ErrConflict", err)
	}
	contents, err := os.ReadFile(filePath)
	if err != nil || string(contents) != "external change" {
		t.Fatalf("conflicting write contents = %q, %v", contents, err)
	}
}
