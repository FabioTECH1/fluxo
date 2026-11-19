package maintenancetask

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/services/executiondomain/testfixture"
	"golang.org/x/sys/unix"
)

// Also runnable against v0.4.40. Observe the waiter's open lock descriptor
// so the test actually interrupts between its pre-lock and post-lock reads.
func TestInstallerGuardRejectsAlreadyWaitingLegacyGate(t *testing.T) {
	if !testfixture.InRoot(t, "") {
		return
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.Mkdir(stateRoot, 0700))
	must(os.WriteFile(filepath.Join(stateRoot, "state.json"), []byte("{}"), 0600))
	anchor, lock := stateRoot+".initialized", stateRoot+".lock"
	must(os.WriteFile(anchor, []byte("1\n"), 0600))
	must(os.WriteFile(lock, []byte(readyGate), 0600))
	f, err := os.OpenFile(lock, os.O_RDWR, 0)
	must(err)
	defer f.Close()
	must(unix.Flock(int(f.Fd()), unix.LOCK_EX))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		release, e := gate(ctx, false)
		if e == nil {
			release()
		}
		result <- e
	}()
	for {
		fds, e := os.ReadDir("/proc/self/fd")
		must(e)
		count := 0
		for _, fd := range fds {
			target, _ := os.Readlink(filepath.Join("/proc/self/fd", fd.Name()))
			if target == lock {
				count++
			}
		}
		if count == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("helper did not reach the flock")
		case <-time.After(time.Millisecond):
		}
	}
	must(atomicFile(anchor, []byte(strings.Repeat("x", 33)), 0600))
	must(unix.Flock(int(f.Fd()), unix.LOCK_UN))
	if err := <-result; err == nil || !strings.Contains(err.Error(), anchor) {
		t.Fatalf("queued gate did not refuse the oversized anchor: %v", err)
	}
	if release, e := gate(ctx, false); e == nil {
		release()
		t.Fatal("new helper passed guarded anchor")
	}
	must(atomicFile(anchor, []byte("1\n"), 0600))
	release, e := gate(ctx, false)
	must(e)
	release()
	raw, e := os.ReadFile(lock)
	must(e)
	if string(raw) != readyGate {
		t.Fatal("changed flock phase")
	}
	opened, e := f.Stat()
	must(e)
	named, e := os.Stat(lock)
	must(e)
	if !os.SameFile(opened, named) {
		t.Fatal("replaced flock inode")
	}
}
