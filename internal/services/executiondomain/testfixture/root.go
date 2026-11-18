// Package testfixture runs unit fixtures with the production Linux ownership
// and absolute-path checks in a private, disposable filesystem.
package testfixture

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// InRoot re-executes this test in an unprivileged user namespace. Only the
// child may populate the fixture. Its executable is a private, replaceable copy.
func InRoot(t *testing.T, inputs string) bool {
	t.Helper()
	const panel = "/unit/panel.test"
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if exe == panel && os.Getenv("FLUXO_UNIT_ROOT") == t.Name() {
		return true
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("private root filesystem unit fixture requires bubblewrap")
	}
	args := []string{"--unshare-user", "--uid", "0", "--gid", "0", "--unshare-pid", "--proc", "/proc", "--dev", "/dev",
		"--ro-bind", "/usr", "/usr", "--symlink", "usr/lib", "/lib", "--symlink", "usr/lib64", "/lib64",
		"--dir", "/tmp", "--dir", "/var/lib", "--dir", "/etc/cron.d", "--dir", "/unit", "--chdir", "/"}
	if output, err := exec.CommandContext(t.Context(), bwrap, append(args, "/usr/bin/true")...).CombinedOutput(); err != nil {
		t.Skipf("private user namespace unavailable: %v: %s", err, output)
	}
	if inputs != "" {
		path, err := filepath.Abs(inputs)
		if err != nil {
			t.Fatal(err)
		}
		args = append(args, "--ro-bind", path, "/inputs")
	}
	image, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	args = append(args, "--perms", "0700", "--file", "3", panel, "--setenv", "FLUXO_UNIT_ROOT", t.Name(), panel, "-test.run", "^"+t.Name()+"$")
	cmd := exec.CommandContext(t.Context(), bwrap, args...)
	cmd.ExtraFiles = []*os.File{image}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("private root fixture: %v\n%s", err, out)
	}
	return false
}
