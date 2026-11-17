package scheduler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"fluxo/internal/syscmd"
)

// Application-owned acceptance paths are written as the application identity.
// Root-owned control/evidence paths keep their separate protected writer.
func makeAcceptanceDirectoryAsFluxo(path string) error {
	return runAcceptanceAsFluxo("/usr/bin/mkdir", "--mode=0755", "--", path)
}

func writeAcceptanceFileAsFluxo(path string, data []byte, mode os.FileMode) error {
	const writer = `import base64,os,sys,tempfile
path,mode,data=sys.argv[1],int(sys.argv[2],8),base64.b64decode(sys.argv[3],validate=True)
fd,temporary=tempfile.mkstemp(prefix='.fluxo-acceptance-',dir=os.path.dirname(path))
try:
 with os.fdopen(fd,'wb') as out:
  os.fchmod(out.fileno(),mode)
  out.write(data);out.flush();os.fsync(out.fileno())
 os.replace(temporary,path)
finally:
 if os.path.exists(temporary):os.unlink(temporary)
`
	return runAcceptanceAsFluxo("/usr/bin/python3", "-I", "-S", "-c", writer, path, strconv.FormatUint(uint64(mode.Perm()), 8), base64.StdEncoding.EncodeToString(data))
}

func runAcceptanceAsFluxo(name string, args ...string) error {
	credential, err := syscmd.ResolveCredential("fluxo")
	if err != nil || credential.Uid == 0 {
		return errors.New("nonroot acceptance identity required")
	}
	if os.Geteuid() == 0 {
		_, err = syscmd.RunApplicationProbe(context.Background(), 10*time.Second, "fluxo", "/", name, args...)
		return err
	}
	if uint32(os.Geteuid()) != credential.Uid || uint32(os.Getegid()) != credential.Gid {
		return errors.New("unexpected acceptance writer identity")
	}
	// Already-unprivileged payloads must not try to replace supplementary groups.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
	return cmd.Run()
}

func distributionUserAtomic(path string, value any, mode os.FileMode) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeAcceptanceFileAsFluxo(path, raw, mode)
}

func TestVMAcceptanceFilesUseApplicationIdentity(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || distributionVMHost() == "" {
		t.Skip("explicit root opt-in on dedicated acceptance VM only")
	}
	credential, err := syscmd.ResolveCredential("fluxo")
	if err != nil || credential.Uid == 0 {
		t.Fatal("nonroot application identity required", err)
	}
	root, err := os.MkdirTemp("/var/lib", "fluxo-acceptance-writer-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err = os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "application")
	if err = os.Mkdir(parent, 0755); err != nil {
		t.Fatal(err)
	}
	// This parent is beneath a root-owned directory, so only root can replace it.
	if err = os.Chown(parent, int(credential.Uid), int(credential.Gid)); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(parent, "fixture")
	if err = makeAcceptanceDirectoryAsFluxo(dir); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "release")
	if err = distributionUserAtomic(file, map[string]bool{"released": true}, 0644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(file)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != credential.Uid || st.Gid != credential.Gid || info.Mode().Perm() != 0644 {
		t.Fatal("writer did not create the fixture as the application identity")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "writer.test")
	if err = os.WriteFile(child, image, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err = syscmd.RunApplicationProbeEnv(context.Background(), 20*time.Second, "fluxo", "/", []string{"FLUXO_PHP_UPDATE_VM_TEST=1", "FLUXO_ACCEPTANCE_WRITER_CHILD=" + file}, child, "-test.run=^TestVMAcceptanceWriterChild$"); err != nil {
		t.Fatal("already-unprivileged writer", err)
	}

	var value map[string]bool
	if err = distributionRead(file, &value); err != nil || !value["released"] {
		t.Fatal("application release data differs", err)
	}
}

func TestVMAcceptanceWriterChild(t *testing.T) {
	path := os.Getenv("FLUXO_ACCEPTANCE_WRITER_CHILD")
	if path == "" {
		t.Skip("launched by guarded identity test only")
	}
	if os.Geteuid() == 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || distributionVMHost() == "" {
		t.Fatal("unprivileged guarded child required")
	}
	if err := distributionUserAtomic(path, map[string]bool{"released": true}, 0644); err != nil {
		t.Fatal(err)
	}
}
