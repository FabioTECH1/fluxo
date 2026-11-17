package phpupdate

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestRoadRunnerProcessFixture(t *testing.T) {
	if os.Getenv("FLUXO_ROADRUNNER_PROCESS_FIXTURE") != "1" {
		return
	}
	ready, lifetime := os.NewFile(3, "ready"), os.NewFile(4, "lifetime")
	defer ready.Close()
	defer lifetime.Close()
	if _, err := ready.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, lifetime); err != nil {
		t.Fatal(err)
	}
}

func TestRoadRunnerRejectsReplacedRunningExecutable(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rr")
	if err := os.WriteFile(path, data, 0700); err != nil {
		t.Fatal(err)
	}
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { readyRead.Close(); readyWrite.Close() })
	lifetimeRead, lifetimeWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lifetimeRead.Close(); lifetimeWrite.Close() })
	child := exec.Command(path, "-test.run=^TestRoadRunnerProcessFixture$")
	child.Env = append(os.Environ(), "FLUXO_ROADRUNNER_PROCESS_FIXTURE=1")
	child.ExtraFiles = []*os.File{readyWrite, lifetimeRead}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	readyWrite.Close()
	lifetimeRead.Close()
	if err := readyRead.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var ready [1]byte
	if _, err := io.ReadFull(readyRead, ready[:]); err != nil || ready[0] != 1 {
		t.Fatal("fixture process did not acknowledge startup", err)
	}
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
