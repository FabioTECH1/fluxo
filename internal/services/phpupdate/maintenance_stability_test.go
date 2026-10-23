package phpupdate

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"fluxo/internal/syscmd"
)

func TestMaintenanceStabilityAllowsJobsButDetectsControllerAndNativeReplacement(t *testing.T) {
	c := MaintenanceConsumer{Consumer: Consumer{Kind: "laravel_horizon"}}
	u := MaintenanceRuntime{Name: "fluxo-daemon-1.service", Active: true, MainPID: 100, Restarts: "0", PIDs: map[int]string{100: "boot:controller", 101: "boot:job"}}
	identity, err := maintenanceStabilityIdentity(c, u)
	if err != nil {
		t.Fatal(err)
	}
	for _, pids := range []map[int]string{{100: "boot:controller"}, {100: "boot:controller", 102: "boot:newjob", 103: "boot:node"}} {
		changed := u
		changed.PIDs = pids
		if next, e := maintenanceStabilityIdentity(c, changed); e != nil || next != identity {
			t.Fatal("job turnover reset controller stability", next, e)
		}
	}
	for _, changed := range []MaintenanceRuntime{
		{Name: u.Name, Active: true, MainPID: 200, Restarts: "0", PIDs: map[int]string{200: "boot:controller"}},
		{Name: u.Name, Active: true, MainPID: 100, Restarts: "0", PIDs: map[int]string{100: "boot:reused-pid"}},
		{Name: u.Name, Active: true, MainPID: 100, Restarts: "1", PIDs: u.PIDs},
	} {
		if next, e := maintenanceStabilityIdentity(c, changed); e != nil || next == identity {
			t.Fatal("controller replacement/restart did not reset stability", next, e)
		}
	}
	c.Consumer.Kind = "laravel_octane"
	u.Octane = &octaneProcesses{Backend: "swoole", MasterPID: 201, ManagerPID: 202}
	u.PIDs = map[int]string{100: "boot:controller", 201: "boot:master", 202: "boot:manager", 301: "boot:worker"}
	identity, err = maintenanceStabilityIdentity(c, u)
	if err != nil {
		t.Fatal(err)
	}
	changed := u
	changed.PIDs = map[int]string{100: "boot:controller", 201: "boot:master", 202: "boot:manager", 302: "boot:newworker"}
	if next, e := maintenanceStabilityIdentity(c, changed); e != nil || next != identity {
		t.Fatal("native job/worker turnover reset serving stability", next, e)
	}
	for _, native := range []*octaneProcesses{
		{Backend: "swoole", MasterPID: 202, ManagerPID: 201},
		{Backend: "swoole", MasterPID: 301, ManagerPID: 202},
		{Backend: "swoole", MasterPID: 201, ManagerPID: 301},
	} {
		changed.Octane = native
		changed.PIDs = u.PIDs
		if next, e := maintenanceStabilityIdentity(c, changed); e != nil || next == identity {
			t.Fatal("native serving replacement did not reset stability", next, e)
		}
	}
	for _, missing := range []MaintenanceRuntime{
		{Name: u.Name, Active: true, MainPID: 100, Restarts: "0", PIDs: u.PIDs},
		{Name: u.Name, Active: true, MainPID: 100, PIDs: u.PIDs, Octane: u.Octane},
		{Name: u.Name, Active: true, MainPID: 100, Restarts: "0", PIDs: map[int]string{100: "boot:controller"}, Octane: u.Octane},
	} {
		if _, e := maintenanceStabilityIdentity(c, missing); e == nil {
			t.Fatal("missing serving/restart evidence accepted", missing)
		}
	}
}

func TestMaintenanceObservationRetryRequiresRecordedChildProvenGone(t *testing.T) {
	// Capture an actual child's boot/start evidence through the command runner;
	// Run returns only after it has exited and been reaped.
	out, err := syscmd.Run(context.Background(), 5*time.Second, "/usr/bin/cat", "/proc/self/stat", "/proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || strings.LastIndex(lines[0], ")") < 0 {
		t.Fatal("missing child process evidence", out)
	}
	pid, err := strconv.Atoi(strings.Fields(lines[0])[0])
	fields := strings.Fields(lines[0][strings.LastIndex(lines[0], ")")+1:])
	if err != nil || len(fields) < 20 {
		t.Fatal("invalid child process evidence", out, err)
	}
	key := lines[1] + ":" + fields[19]
	u := MaintenanceRuntime{MainPID: os.Getpid(), PIDs: map[int]string{pid: key, os.Getpid(): "live-controller"}}
	missing := &os.PathError{Op: "readlink", Path: fmt.Sprintf("/proc/%d/exe", pid), Err: syscall.ENOENT}
	if !maintenanceChildDisappeared(u, missing) {
		t.Fatal("proven exited recorded child could not refresh observation")
	}
	live := u
	live.MainPID = pid
	if maintenanceChildDisappeared(live, &os.PathError{Op: "read", Path: fmt.Sprintf("/proc/%d/exe", os.Getpid()), Err: syscall.ENOENT}) {
		t.Fatal("a live non-controller inspection error was swallowed")
	}
	for _, issue := range []error{
		&os.PathError{Op: "read", Path: fmt.Sprintf("/proc/%d/exe", os.Getpid()), Err: syscall.ENOENT},
		&os.PathError{Op: "read", Path: missing.Path, Err: syscall.EACCES},
		&os.PathError{Op: "stat", Path: "/opt/fluxo/node/v999/bin/node", Err: syscall.ENOENT},
		fmt.Errorf("untrusted executable"),
	} {
		if maintenanceChildDisappeared(u, issue) {
			t.Fatal("controller/provenance/permission error was swallowed", issue)
		}
	}
	u.MainPID = pid
	if maintenanceChildDisappeared(u, missing) {
		t.Fatal("controller death was treated as a transient job")
	}
	delete(u.PIDs, pid)
	u.MainPID = os.Getpid()
	if maintenanceChildDisappeared(u, missing) {
		t.Fatal("unrecorded process disappearance was ignored")
	}
}
