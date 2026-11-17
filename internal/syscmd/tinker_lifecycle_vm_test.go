package syscmd

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/services/packages"
)

func requireTinkerLifecycleVM(t *testing.T) {
	t.Helper()
	host, _ := os.Hostname()
	switch host {
	case "fluxo-php-patch-test", "fluxo-bridge-jammy-20261101", "fluxo-bridge-prod-20261101", "fluxo-bridge-resolute-20261101":
	default:
		t.Skip("guarded retained or dedicated bridge VM only")
	}
	if os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("root and explicit VM opt-in required")
	}
	if _, err := TinkerOwner(); err != nil {
		t.Fatal(err)
	}
}

// This subprocess role never evaluates PHP or touches an installed application.
func TestVMTinkerLaunchGateChild(t *testing.T) {
	if os.Getenv("FLUXO_TINKER_GATE_CHILD") != "1" {
		t.Skip("private subprocess role")
	}
	requireTinkerLifecycleVM(t)
	dir := os.Getenv("FLUXO_TINKER_GATE_DIR")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	unlock, err := lockTinkerLaunch(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err = tinkerAdmitted(dir); err != nil {
		fmt.Println("DENIED")
		return
	}
	fmt.Println("CHECKED")
	if _, err = io.ReadFull(os.Stdin, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	// Represents the final cmd.Start side effect while still holding the gate.
	if err = os.WriteFile(filepath.Join(dir, "started"), []byte("started"), 0600); err != nil {
		t.Fatal(err)
	}
	fmt.Println("STARTED")
}

func TestVMTinkerCrossProcessLaunchRevocation(t *testing.T) {
	requireTinkerLifecycleVM(t)
	dir, err := os.MkdirTemp("/run", "fluxo-tinker-config-gate-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err = os.WriteFile(filepath.Join(dir, "admission"), []byte("active"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	child := func() (*exec.Cmd, *bufio.Reader, io.WriteCloser) {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestVMTinkerLaunchGateChild$")
		cmd.Env = append(os.Environ(), "FLUXO_TINKER_GATE_CHILD=1", "FLUXO_TINKER_GATE_DIR="+dir)
		stdout, e := cmd.StdoutPipe()
		if e != nil {
			t.Fatal(e)
		}
		stdin, e := cmd.StdinPipe()
		if e != nil {
			t.Fatal(e)
		}
		cmd.Stderr = os.Stderr
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		return cmd, bufio.NewReader(stdout), stdin
	}
	read := func(r *bufio.Reader, want string) {
		t.Helper()
		line, e := r.ReadString('\n')
		if e != nil || strings.TrimSpace(line) != want {
			t.Fatalf("barrier %q: %q %v", want, line, e)
		}
	}
	cmd, r, in := child()
	read(r, "CHECKED")
	// The child's held flock is the barrier: cancellation cannot finish until
	// the admitted launch side effect completes and that same lock is released.
	revoked := make(chan error, 1)
	go func() { revoked <- revokeTinkerAdmission(ctx, dir) }()
	if _, err = in.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	in.Close()
	read(r, "STARTED")
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if err = <-revoked; err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "started")); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(dir, "started")); err != nil {
		t.Fatal(err)
	}
	cmd, r, in = child()
	read(r, "DENIED")
	in.Close()
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "started")); !os.IsNotExist(err) {
		t.Fatalf("revoked process launched: %v", err)
	}
}

// Payload exits only through cooperative release. A stop job deliberately does
// not kill it; this recreates successful stop with a populated cgroup.
func TestVMTinkerNaturalDrainPayload(t *testing.T) {
	if os.Getenv("FLUXO_TINKER_DRAIN_CHILD") != "1" {
		t.Skip("private subprocess role")
	}
	requireTinkerLifecycleVM(t)
	dir := os.Getenv("FLUXO_TINKER_DRAIN_DIR")
	if filepath.Dir(dir) != "/run" || !strings.HasPrefix(filepath.Base(dir), "fluxo-tinker-config-drain-") {
		t.Fatal("invalid private fixture")
	}
	if err := os.WriteFile(filepath.Join(dir, "ready"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("cooperative release timed out")
		case <-tick.C:
			if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
				return
			}
		}
	}
}

func TestVMTinkerStopSuccessRetainsPopulatedDomain(t *testing.T) {
	requireTinkerLifecycleVM(t)
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	id := hex.EncodeToString(nonce[:])
	unit := "fluxo-tinker-" + id + ".service"
	dir, err := os.MkdirTemp("/run", "fluxo-tinker-config-drain-")
	if err != nil {
		t.Fatal(err)
	}
	// Preserve fixture evidence if cooperative cleanup cannot prove emptiness.
	unregister := packages.RegisterTinkerDomain(id)
	defer func() {
		if err := os.WriteFile(filepath.Join(dir, "release"), []byte("release"), 0600); err != nil {
			t.Error(err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := waitTinkerDomainEmpty(ctx, unit); err != nil {
			t.Errorf("retaining fixture after uncertain natural drain: %v", err)
			return
		}
		unregister()
		_, _ = Run(context.Background(), 5*time.Second, "/usr/bin/systemctl", "reset-failed", unit)
		_ = os.RemoveAll(dir)
	}()
	if err = os.WriteFile(filepath.Join(dir, "admission"), []byte("active"), 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	out, err := Run(context.Background(), 10*time.Second, "/usr/bin/systemd-run", "--quiet", "--unit="+unit, "--property=Type=exec", "--property=KillMode=none", "--property=Restart=no", "--setenv=FLUXO_PHP_UPDATE_VM_TEST=1", "--setenv=FLUXO_TINKER_DRAIN_CHILD=1", "--setenv=FLUXO_TINKER_DRAIN_DIR="+dir, exe, "-test.run=^TestVMTinkerNaturalDrainPayload$")
	if err != nil {
		t.Fatalf("private unit: %v %s", err, out)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err = os.Stat(filepath.Join(dir, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("payload readiness barrier timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if out, err = Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "stop", unit); err != nil {
		t.Fatalf("stop must succeed before testing population retention: %v %s", err, out)
	}
	devnull, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	close(done)
	called := false
	p := &TinkerProcess{PTY: devnull, Done: done, cmd: exec.Command("/usr/bin/true"), unit: unit, configDir: dir, unregister: func() { called = true; unregister() }}
	if err = p.Close(); err == nil {
		t.Fatal("Close accepted a populated execution domain")
	}
	if called {
		t.Fatal("Close unregistered before positive emptiness")
	}
	raw, err := os.ReadFile(filepath.Join("/sys/fs/cgroup/system.slice", unit, "cgroup.events"))
	if err != nil || !strings.Contains(string(raw), "populated 1") {
		t.Fatalf("missing populated proof: %q %v", raw, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if release, err := packages.Lifecycle(ctx, false); err == nil {
		release()
		t.Fatal("runtime maintenance admitted populated domain")
	}
	if err = packages.UnownedTinkerDomainsIdle(); err != nil {
		t.Fatalf("failed Close lost registry ownership: %v", err)
	}
	if err = p.Close(); err == nil || called {
		t.Fatal("repeated Close discarded uncertain cleanup")
	}
}
