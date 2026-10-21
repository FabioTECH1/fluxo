package phpupdate

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoadRunnerExecutableBindsRunningBytes(t *testing.T) {
	path, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d := &OctaneDescriptor{RRBinary: path, RRSHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
	if err := verifyRoadRunnerExecutable(os.Getpid(), d); err != nil {
		t.Fatal(err)
	}
	d.RRSHA256 = strings.Repeat("0", 64)
	if verifyRoadRunnerExecutable(os.Getpid(), d) == nil {
		t.Fatal("running bytes accepted with a different approved hash")
	}
	d.RRSHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
	d.RRBinary = filepath.Join(t.TempDir(), "rr")
	if err := os.WriteFile(d.RRBinary, data, 0700); err != nil {
		t.Fatal(err)
	}
	if verifyRoadRunnerExecutable(os.Getpid(), d) == nil {
		t.Fatal("a separate qualified executable substituted for the running executable")
	}
}

func TestRoadRunnerRejectsReplacedRunningExecutable(t *testing.T) {
	data, err := os.ReadFile("/usr/bin/sleep")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rr")
	if err := os.WriteFile(path, data, 0700); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(path, "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	d := &OctaneDescriptor{RRBinary: path, RRSHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
	if err := verifyRoadRunnerExecutable(child.Process.Pid, d); err != nil {
		t.Fatal(err)
	}
	replacement := path + ".new"
	if err := os.WriteFile(replacement, data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if verifyRoadRunnerExecutable(child.Process.Pid, d) == nil {
		t.Fatal("old running executable accepted after pathname replacement, even with identical approved bytes")
	}
}
