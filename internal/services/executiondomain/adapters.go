package executiondomain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

const (
	CurrentSchedulerSchema  = 5
	CurrentSystemTaskSchema = 3
	// Existing schema5/backend authority keeps its immutable reader6 minimum.
	SchedulerReaderSupport = 6
	// Historical retirement adds a separate reader7 capability and floor role.
	SchedulerCompiledReaderSupport = 7
	SystemTaskReaderSupport        = 3
	// PersistentV1 is the immutable retained-domain lifetime contract supported
	// by systemd249. It never means ExitType=cgroup, including on newer hosts.
	PersistentV1 = "systemd-persistent-v1"
)

func InvocationAdapter(id AdapterID) (AdapterID, error) {
	if id == PersistentV1 {
		return id, nil
	}
	return LegacyAdapter(id)
}

// SelectAdapter remains deliberately conservative. Support for reading and
// executing a recorded contract is independent of production host selection.
// The reviewed persistent implementation is selected only on stock Jammy amd64/arm64;
// caller-specific provider and execution profiles remain independent gates.
func SelectAdapter(ctx context.Context) (AdapterID, error) {
	if err := RequireCgroupExitV1(ctx); err == nil {
		return CgroupExitV1, nil
	} else if !errors.Is(err, ErrUnsupportedHost) {
		return "", err
	}
	if err := requireQualifiedPersistentSelection(ctx); err != nil {
		return "", err
	}
	return PersistentV1, nil
}

func RequireAdapterHost(ctx context.Context, id AdapterID) error {
	adapter, err := InvocationAdapter(id)
	if err != nil {
		return err
	}
	if adapter == CgroupExitV1 {
		return RequireCgroupExitV1(ctx)
	}
	if _, err = os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		return errors.New("persistent execution contract requires cgroup v2")
	}
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", "--property=Version", "--value")
	if err != nil {
		return err
	}
	value := strings.TrimSpace(out)
	end := 0
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	version, err := strconv.Atoi(value[:end])
	if err != nil || version < 249 {
		return errors.New("persistent execution contract requires systemd249 or newer")
	}
	return nil
}

// SubmissionArguments owns lifetime/isolation only. Caller-specific user,
// capability, namespace, environment and command policies remain independent.
func SubmissionArguments(id AdapterID, unit string) ([]string, error) {
	adapter, err := InvocationAdapter(id)
	if err != nil {
		return nil, err
	}
	args := []string{"--quiet", "--unit=" + unit}
	if adapter == CgroupExitV1 {
		args = append(args, "--collect")
		return append(args, CgroupExitV1Arguments()...), nil
	}
	return append(args,
		"--property=Type=exec", "--property=RemainAfterExit=yes", "--property=KillMode=none", "--property=Restart=no", "--property=CollectMode=inactive",
		"--property=NoNewPrivileges=yes", "--property=ProtectControlGroups=yes", "--property=Delegate=no",
		"--property=RuntimeMaxSec=infinity", "--property=WatchdogSec=0", "--property=TimeoutStopSec=infinity", "--property=SendSIGKILL=no"), nil
}

func InvocationProperties(id AdapterID) (map[string]string, error) {
	adapter, err := InvocationAdapter(id)
	if err != nil {
		return nil, err
	}
	if adapter == CgroupExitV1 {
		return CgroupExitV1Properties(), nil
	}
	return map[string]string{"Type": "exec", "RemainAfterExit": "yes", "KillMode": "none", "Restart": "no", "CollectMode": "inactive", "NoNewPrivileges": "yes", "ProtectControlGroups": "yes", "Delegate": "no", "Transient": "yes", "RuntimeMaxUSec": "infinity", "WatchdogUSec": "0", "TimeoutStopUSec": "infinity", "SendSIGKILL": "no", "DropInPaths": "", "ExecStartPre": "", "ExecStartPost": "", "ExecStop": "", "ExecStopPost": ""}, nil
}

// ValidateLifetime supplements exact property maps for properties absent on249.
func ValidateLifetime(id AdapterID, p map[string]string) error {
	adapter, err := InvocationAdapter(id)
	if err != nil {
		return err
	}
	if adapter == PersistentV1 && p["ExitType"] != "" && p["ExitType"] != "main" {
		return errors.New("persistent service unexpectedly changed ExitType contract")
	}
	return nil
}

// StopNaturallyEmpty never infers emptiness from inactive/failed state. The
// caller owns its journal gate and supplies a hierarchy proof taken immediately
// before stop; authority must already be consumed or durably revoked. KillMode
// none/SendSIGKILL=no make stop administrative retirement, never payload cleanup.
func StopNaturallyEmpty(ctx context.Context, id AdapterID, unit string, proveEmpty func() error) error {
	return stopNaturallyEmpty(ctx, id, unit, proveEmpty, syscmd.Run)
}

func stopNaturallyEmpty(ctx context.Context, id AdapterID, unit string, proveEmpty func() error, run func(context.Context, time.Duration, string, ...string) (string, error)) error {
	adapter, err := InvocationAdapter(id)
	if err != nil {
		return err
	}
	if proveEmpty == nil {
		return errors.New("execution retirement requires a natural hierarchy-empty proof")
	}
	if err = proveEmpty(); err != nil {
		return err
	}
	if adapter != PersistentV1 {
		return nil
	}
	if !ownedExecutionUnit(unit) {
		return errors.New("persistent retirement requires an exact Fluxo invocation unit")
	}
	if _, err = run(ctx, 10*time.Second, "/usr/bin/systemctl", "stop", unit); err != nil {
		// PID1 may already have collected this terminal invocation, including
		// between the caller's metadata observation and stop. Only independently
		// confirmed absence plus a fresh hierarchy proof completes retirement.
		out, inspectErr := run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", unit, "--property=LoadState", "--value")
		if inspectErr == nil && strings.TrimSpace(out) == "not-found" {
			return proveEmpty()
		}
		return fmt.Errorf("retire naturally empty execution unit: %w", err)
	}
	if err = proveEmpty(); err != nil {
		return err
	}
	// CollectMode=inactive deliberately retains failed units. Clear only this
	// already-empty owned invocation's failed metadata so PID1 may collect it.
	if _, err = run(ctx, 5*time.Second, "/usr/bin/systemctl", "reset-failed", unit); err != nil {
		out, e := run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", unit, "--property=LoadState", "--value")
		if e != nil || strings.TrimSpace(out) != "not-found" {
			return fmt.Errorf("retire empty failed-unit metadata: %w", err)
		}
	}
	return proveEmpty()
}

// UnitQuiescent is only the service metadata half of a proof. Callers MUST also
// read the expected hierarchical cgroup count; unit absence is not emptiness.
func UnitQuiescent(id AdapterID, p map[string]string) bool {
	adapter, err := InvocationAdapter(id)
	if err != nil {
		return false
	}
	if p["LoadState"] == "not-found" {
		return true
	}
	if p["MainPID"] != "0" || p["ControlPID"] != "0" {
		return false
	}
	if p["ActiveState"] == "inactive" || p["ActiveState"] == "failed" {
		return true
	}
	return adapter == PersistentV1 && p["ActiveState"] == "active" && p["SubState"] == "exited"
}

func ownedExecutionUnit(unit string) bool {
	if !strings.HasSuffix(unit, ".service") {
		return false
	}
	for _, prefix := range []string{"fluxo-scheduler-", "fluxo-system-task-"} {
		if strings.HasPrefix(unit, prefix) {
			id, err := uuid.Parse(strings.TrimSuffix(strings.TrimPrefix(unit, prefix), ".service"))
			return err == nil && prefix+id.String()+".service" == unit
		}
	}
	return false
}
