package packages

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The child uses the actual POSIX lock primitive, independently of the Fluxo
// acquisition helper. A flock-based implementation must fail these checks.
func TestNativeDPKGRecordLockChild(t *testing.T) {
	path := os.Getenv("FLUXO_NATIVE_LOCK_CHILD")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lock := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: 0, Len: 0}
	err = syscall.FcntlFlock(f.Fd(), syscall.F_SETLK, &lock)
	if os.Getenv("FLUXO_NATIVE_LOCK_EXPECT_BUSY") == "1" {
		if !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EACCES) {
			t.Fatalf("native record lock was not excluded: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("locked")
	_, _ = bufio.NewReader(os.Stdin).ReadByte()
}

func nativeLockChild(t *testing.T, path string, busy bool) (func(), error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestNativeDPKGRecordLockChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), "FLUXO_NATIVE_LOCK_CHILD="+path)
	if busy {
		cmd.Env = append(cmd.Env, "FLUXO_NATIVE_LOCK_EXPECT_BUSY=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("record lock child: %w: %s", err, out)
		}
		return func() {}, nil
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		input.Close()
		return nil, err
	}
	stopped := false
	stop := func() {
		if !stopped {
			stopped = true
			input.Close()
			_ = cmd.Wait()
		}
	}
	t.Cleanup(stop)
	ready := make(chan error, 1)
	go func() {
		line, e := bufio.NewReader(output).ReadString('\n')
		if e == nil && strings.TrimSpace(line) != "locked" {
			e = fmt.Errorf("unexpected child readiness: %q", line)
		}
		ready <- e
	}()
	select {
	case err = <-ready:
		if err != nil {
			stop()
			return nil, err
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		stop()
		return nil, errors.New("native lock child did not become ready")
	}
	return stop, nil
}

func TestNativeDPKGUnsafePathRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if release, err := nativeDPKGLocks(context.Background(), []string{path}); err == nil {
		release()
		t.Fatal("a native lock under a writable/non-root ancestor was accepted")
	}
}

func TestVMNativeDPKGRecordLocksAndPartialFailure(t *testing.T) {
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || host != "fluxo-php-patch-test" || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("requires explicitly guarded reusable integration VM")
	}
	// Private files exercise native locking without taking the machine's real
	// APT locks or modifying its package database.
	dir, err := os.MkdirTemp("/var/lib", "fluxo-native-lock-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	paths := []string{filepath.Join(dir, "lock-frontend"), filepath.Join(dir, "lock")}
	for _, path := range paths {
		if err = os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	release, err := nativeDPKGLocks(context.Background(), paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if _, err = nativeLockChild(t, path, true); err != nil {
			release()
			t.Fatal(err)
		}
	}
	release()
	stop, err := nativeLockChild(t, paths[1], false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	_, err = nativeDPKGLocks(ctx, paths)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		stop()
		t.Fatalf("native contention did not respect the deadline: %v", err)
	}
	// Failure on the database lock must relinquish the first frontend lock.
	frontendStop, err := nativeLockChild(t, paths[0], false)
	if err != nil {
		stop()
		t.Fatal(err)
	}
	frontendStop()
	stop()
	release, err = nativeDPKGLocks(context.Background(), paths)
	if err != nil {
		t.Fatal(err)
	}
	release()
	// The helper never writes lock contents.
	for _, path := range paths {
		f, e := os.Open(path)
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(f)
		f.Close()
		if e != nil || len(b) != 0 {
			t.Fatal("native lock file contents changed", e)
		}
	}
}
