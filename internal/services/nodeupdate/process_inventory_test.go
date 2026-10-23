package nodeupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestLateNodeChildRequiresFreshDomainAndRetainsPriorEvidence(t *testing.T) {
	root := consumerTestRuntime(t)
	pid, key := consumerTestProcess(t, root)
	previous := map[int]string{1: "boot:prior-domain"}
	inspections := 0
	domain := processDomain{pids: previous, refresh: func(context.Context) (map[int]string, error) {
		inspections++
		return map[int]string{pid: key}, nil
	}}
	if err := domain.authenticate(context.Background(), pid, key); err != nil || inspections != 1 || previous[pid] != key || previous[1] != "boot:prior-domain" {
		t.Fatal("late child bypassed inspection or discarded prior drain evidence", err, inspections, previous)
	}
	if err := domain.authenticate(context.Background(), pid, key); err != nil || inspections != 1 {
		t.Fatal("same authenticated execution required another inspection", err, inspections)
	}
}

func TestNodeInventoryPIDReuseCannotUsePriorIntegerMembership(t *testing.T) {
	root := consumerTestRuntime(t)
	pid, key := consumerTestProcess(t, root)
	for _, problem := range []string{"stale observation", "controller changed", "wrong runtime", "permission denied"} {
		t.Run(problem, func(t *testing.T) {
			calls := 0
			old := "prior-boot:prior-start"
			domain := processDomain{pids: map[int]string{pid: old}, refresh: func(context.Context) (map[int]string, error) {
				calls++
				if problem == "stale observation" {
					return map[int]string{pid: old}, nil
				}
				return map[int]string{pid: key}, errors.New(problem)
			}}
			if err := domain.authenticate(context.Background(), pid, key); err == nil || calls != 1 || domain.pids[pid] != old {
				t.Fatal("PID integer or failed refresh authorized new execution", err, calls, domain.pids)
			}
		})
	}
}

func TestNodeInventoryRequiresExactRuntimePathAndIdentity(t *testing.T) {
	root, wrong := consumerTestRuntime(t), consumerTestRuntime(t)
	pid, key := consumerTestProcess(t, wrong)
	if _, _, err := runtimeProcesses(Unit{PIDs: map[int]string{pid: key}}, root); err == nil {
		t.Fatal("wrong retained executable accepted")
	}
	// Same inode alone must not authenticate an executable at an unreviewed path.
	alias := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(alias, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(root, "bin/node"), filepath.Join(alias, "node")); err != nil {
		t.Fatal(err)
	}
	pid, key = consumerTestProcess(t, filepath.Dir(alias))
	if _, _, err := runtimeProcesses(Unit{PIDs: map[int]string{pid: key}}, root); err == nil {
		t.Fatal("unreviewed hardlink path authenticated by inode alone")
	}
	if _, _, err := runtimeProcesses(Unit{PIDs: map[int]string{pid: "reused-pid"}}, filepath.Dir(alias)); err == nil {
		t.Fatal("different start identity accepted")
	}
}

func TestNodeInventoryUnifiedCgroupRequiresCanonicalExactGroup(t *testing.T) {
	group := "/system.slice/fluxo-daemon-42.service"
	if actual, err := unifiedProcessGroup("0::" + group + "\n"); err != nil || actual != group {
		t.Fatal("canonical group failed", actual, err)
	}
	for _, raw := range []string{"1:name=systemd:" + group, "0::" + group + "\n0::/other", "0::/system.slice/../fluxo-daemon-42.service", "0::" + group + "/", "0::"} {
		if _, err := unifiedProcessGroup(raw); err == nil {
			t.Fatal("ambiguous/noncanonical group accepted", raw)
		}
	}
}

func TestNodeInventoryDisappearanceDoesNotSuppressProvenanceOrPermissionErrors(t *testing.T) {
	if !missingNodeProcess(&os.PathError{Op: "read", Path: "/proc/42/stat", Err: syscall.ENOENT}, 42) {
		t.Fatal("exact missing process error not recognized")
	}
	for _, err := range []error{
		&os.PathError{Op: "read", Path: "/proc/420/stat", Err: syscall.ENOENT},
		&os.PathError{Op: "read", Path: "/proc/42/stat", Err: syscall.EACCES},
		&os.PathError{Op: "read", Path: "/opt/fluxo/node/v24.18.0/bin/node", Err: syscall.ENOENT},
		errors.New("PHP controller executable changed"),
		errors.New("unauthenticated runtime"),
	} {
		if missingNodeProcess(err, 42) {
			t.Fatal("unrelated error treated as a vanished observed process", err)
		}
	}
}
