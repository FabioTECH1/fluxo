package nodeupdate

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Use a real, harmless executable with Node's filename so the production
// /proc identity checks run. No Node download or host service is needed.
func consumerTestRuntime(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open("/bin/sleep")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	target, err := os.OpenFile(filepath.Join(root, "bin", "node"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(target, source)
	closeErr := target.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	return root
}

func consumerTestProcess(t *testing.T, root string) (int, string) {
	t.Helper()
	cmd := exec.Command(filepath.Join(root, "bin", "node"), "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	key, err := processKey(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid, key
}

func consumerTestPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func TestNodeRecoveryFailedAndTransitionalConsumersPreserveIntent(t *testing.T) {
	root := consumerTestRuntime(t)
	ctx := context.Background()
	for _, active := range []bool{true, false} {
		original := Unit{ID: 1, Name: "fluxo-daemon-1.service", Domain: "app.example", Active: active, Enabled: "enabled", Port: consumerTestPort(t)}
		for _, state := range []string{"failed", "activating", "deactivating", "inactive", "active"} {
			p := map[string]string{"ActiveState": state, "SubState": "dead", "MainPID": "0", "UnitFileState": "enabled"}
			observed, err := observeUnitState(original, p, map[int]string{}, true)
			if err != nil {
				t.Fatalf("recovery rejected %s: %v", state, err)
			}
			expected := Impact{Units: []Unit{original}}
			u, err := inspectConsumer(ctx, observed, []string{root}, &expected)
			if err != nil || u.Active != active || u.State != state || u.ListenerPresent == nil || *u.ListenerPresent {
				t.Fatalf("lost recoverable state or original intent: %+v, %v", u, err)
			}
			if !sameConsumers(Impact{Units: []Unit{u}}, expected) {
				t.Fatalf("health-only change invalidated recovery: %s", state)
			}
			if state != "inactive" {
				if _, err = observeUnitState(original, p, map[int]string{}, false); err == nil {
					t.Fatalf("ordinary apply accepted unhealthy %s", state)
				}
			}
			u.Command = "changed command"
			if sameConsumers(Impact{Units: []Unit{u}}, expected) {
				t.Fatal("recovery accepted changed service contract")
			}
		}
	}
}

func TestNodeRecoveryNoListenerAndDrainingProcesses(t *testing.T) {
	before, after := consumerTestRuntime(t), consumerTestRuntime(t)
	pid, key := consumerTestProcess(t, after)
	original := Unit{ID: 1, Name: "fluxo-daemon-1.service", Active: true, Enabled: "enabled", Port: consumerTestPort(t)}
	expected := Impact{Units: []Unit{original}}
	for _, state := range []string{"active", "activating", "deactivating", "failed"} {
		p := map[string]string{"ActiveState": state, "SubState": "running", "MainPID": strconv.Itoa(pid), "UnitFileState": "enabled"}
		u, err := observeUnitState(original, p, map[int]string{pid: key}, true)
		if err != nil {
			t.Fatal(err)
		}
		u, err = inspectConsumer(context.Background(), u, []string{before, after}, &expected)
		if err != nil || !u.Active || u.ListenerPresent == nil || *u.ListenerPresent {
			t.Fatalf("known candidate process without listener blocked recovery: %+v, %v", u, err)
		}
		if _, err = inspectConsumer(context.Background(), u, []string{after}, nil); err == nil || !strings.Contains(err.Error(), "no listener") {
			t.Fatal("normal apply did not retain health admission", err)
		}
		if _, err = inspectConsumer(context.Background(), u, []string{before}, &expected); err == nil {
			t.Fatal("recovery accepted a process outside the retained runtimes")
		}
		u.PIDs[pid] = "reused PID"
		if _, err = inspectConsumer(context.Background(), u, []string{after}, &expected); err == nil {
			t.Fatal("recovery accepted changed process start identity")
		}
	}
}

func TestNodeRecoveryRejectsForeignListenerEvenWhenAppFailed(t *testing.T) {
	root := consumerTestRuntime(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	u := Unit{ID: 1, Name: "fluxo-daemon-1.service", Active: false, State: "failed", Port: l.Addr().(*net.TCPAddr).Port, PIDs: map[int]string{}}
	expected := Impact{Units: []Unit{u}}
	if _, err = inspectConsumer(context.Background(), u, []string{root}, &expected); err == nil {
		t.Fatal("recovery adopted a foreign listener", err)
	}
	// Non-root host runs cannot read root process FDs; the production inspector
	// correctly fails closed in that case. Exercise the exact ownership path in
	// an isolated proc view containing this real listener and its real owner.
	proc := t.TempDir()
	if err = os.MkdirAll(filepath.Join(proc, "net"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"tcp", "tcp6"} {
		b, e := os.ReadFile(filepath.Join("/proc/net", file))
		if e != nil {
			t.Fatal(e)
		}
		if err = os.WriteFile(filepath.Join(proc, "net", file), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	fdDir := filepath.Join(proc, strconv.Itoa(os.Getpid()), "fd")
	if err = os.MkdirAll(fdDir, 0755); err != nil {
		t.Fatal(err)
	}
	fds, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range fds {
		link, e := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", os.Getpid(), fd.Name()))
		if e == nil && strings.HasPrefix(link, "socket:[") {
			if err = os.Symlink(link, filepath.Join(fdDir, fd.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err = inspectListenerOwnershipAt(context.Background(), u, proc); err == nil || !strings.Contains(err.Error(), "outside its managed cgroup") {
		t.Fatal("foreign listener ownership check did not reject its real owner", err)
	}
}
