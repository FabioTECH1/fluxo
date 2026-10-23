package phpupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/syscmd"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

// Retained with the generation, beyond barrier cleanup. Dispatch intent is
// durable before systemd start; recovery never guesses whether to replay an
// arbitrary command. A successful short-lived execution may complete normally.
type naturalResume struct {
	Boot       string `json:"boot"`
	After      uint64 `json:"after_monotonic_usec"`
	Invocation string `json:"invocation,omitempty"`
	Completed  bool   `json:"completed,omitempty"`
}

func validateNaturalResume(r naturalResume) error {
	boot, err := uuid.Parse(r.Boot)
	if err != nil || boot == uuid.Nil || boot.String() != r.Boot || r.After == 0 || r.Completed && r.Invocation == "" {
		return errors.New("Invalid custom Artisan activation journal; operator review is required")
	}
	if r.Invocation != "" {
		parts := strings.Split(r.Invocation, ":")
		if len(parts) != 3 || parts[0] != r.Boot {
			return errors.New("Invalid custom Artisan execution identity")
		}
		started, startErr := strconv.ParseUint(parts[1], 10, 64)
		pid, pidErr := strconv.Atoi(parts[2])
		if startErr != nil || pidErr != nil || started <= r.After || pid <= 1 {
			return errors.New("Invalid custom Artisan execution identity")
		}
	}
	return nil
}

func naturalBoundary() (naturalResume, error) {
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return naturalResume{}, err
	}
	var clock unix.Timespec
	if err = unix.ClockGettime(unix.CLOCK_MONOTONIC, &clock); err != nil {
		return naturalResume{}, err
	}
	return naturalResume{Boot: strings.TrimSpace(string(boot)), After: uint64(clock.Nano() / 1000)}, nil
}

func (m *Manager) resumeNaturalConsumer(ctx context.Context, g *recoveryGeneration, u unitSnapshot) error {
	current, err := observeUnit(ctx, u.Name, u.CGroup)
	if err != nil {
		return err
	}
	if !u.Active {
		drained, err := fullyDrained(u, current, processKey)
		if err != nil || !drained {
			return fmt.Errorf("Previously stopped custom Artisan process changed: %s (%v)", u.Name, err)
		}
		return nil
	}
	if _, attempted := g.NaturalResumes[u.Name]; attempted {
		// Includes a crash after intent but before dispatch. Missing execution
		// evidence is uncertain, not permission to launch a potentially repeated
		// report, import, billing operation or other non-idempotent action.
		_, _, err := naturalActivationReady(ctx, g, u)
		return err
	}
	b, err := getBarrier(u.Name)
	if err != nil {
		return err
	}
	if b.GenerationID != g.ID || !b.Natural {
		return ErrPlanChanged
	}
	if err = verifyHeldBarrier(ctx, b); err != nil {
		return err
	}
	drained, err := fullyDrained(u, current, processKey)
	if err != nil {
		return err
	}
	if !drained {
		return errors.New("Custom Artisan processes are still draining; no restart was attempted")
	}
	boundary, err := naturalBoundary()
	if err != nil {
		return err
	}
	if g.NaturalResumes == nil {
		g.NaturalResumes = map[string]naturalResume{}
	}
	g.NaturalResumes[u.Name] = boundary
	if err = saveGeneration(*g); err != nil {
		return err
	}
	b.State = "starting"
	if err = saveBarrier(b); err != nil {
		return err
	}
	if err = verifyBarrierFile(b.Marker, b.GenerationID+"\n"); err != nil {
		return err
	}
	if err = os.Remove(b.Marker); err != nil {
		return err
	}
	if err = syncDirectory(filepath.Dir(b.Marker)); err != nil {
		return err
	}
	if _, err = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "start", "--no-block", u.Name); err != nil {
		return fmt.Errorf("Custom Artisan start is unconfirmed; inspect application state before any manual retry: %w", err)
	}
	b.State = "started"
	return saveBarrier(b)
}

// Read-only systemd execution evidence plus the existing process checks. A
// missing, failed, rebooted or replaced invocation never causes an automatic
// second dispatch. Completion evidence is persisted before barriers disappear.
func naturalActivationReady(ctx context.Context, g *recoveryGeneration, old unitSnapshot) (ready, completed bool, err error) {
	r, ok := g.NaturalResumes[old.Name]
	if !ok {
		return false, false, errors.New("Custom Artisan activation has no durable dispatch intent")
	}
	if err := validateNaturalResume(r); err != nil {
		return false, false, err
	}
	current, err := observeUnit(ctx, old.Name, old.CGroup)
	if err != nil {
		return false, false, err
	}
	if r.Completed {
		drained, err := fullyDrained(old, current, processKey)
		if err != nil || !drained {
			return false, false, fmt.Errorf("Completed custom Artisan process changed outside maintenance: %s (%v)", old.Name, err)
		}
		return true, true, nil
	}
	boundary, err := naturalBoundary()
	if err != nil {
		return false, false, err
	}
	if boundary.Boot != r.Boot {
		return false, false, errors.New("Custom Artisan activation crossed a reboot; review its application outcome before any manual retry")
	}
	if boundary.After < r.After {
		return false, false, errors.New("Custom Artisan activation clock evidence is invalid; no automatic replay was attempted")
	}
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", old.Name, "--property=ExecMainPID,ExecMainStartTimestampMonotonic,ExecMainExitTimestampMonotonic,ExecMainCode,ExecMainStatus")
	if err != nil {
		return false, false, err
	}
	p := unitProperties(out)
	started, parseErr := strconv.ParseUint(p["ExecMainStartTimestampMonotonic"], 10, 64)
	if parseErr != nil {
		return false, false, errors.New("Custom Artisan start is unconfirmed; no automatic replay was attempted. Review its application outcome before any manual retry")
	}
	if started <= r.After {
		return false, false, nil // A queued start is still allowed to initialize.
	}
	// InvocationID may be cleared when a successful short-lived service exits.
	// ExecMain's retained PID and monotonic start, bound to this boot, identify
	// the execution even after MainPID and the live cgroup disappear.
	pid, err := strconv.Atoi(p["ExecMainPID"])
	identity := fmt.Sprintf("%s:%d:%d", r.Boot, started, pid)
	if err != nil || pid <= 1 || current.Active && current.MainPID != pid || r.Invocation != "" && r.Invocation != identity {
		return false, false, errors.New("Custom Artisan execution identity is missing or changed; no automatic replay was attempted")
	}
	if r.Invocation == "" {
		r.Invocation = identity
		g.NaturalResumes[old.Name] = r
		if err = saveGeneration(*g); err != nil {
			return false, false, err
		}
	}
	if current.Active {
		return runtimeAdopted(old, current, false, processCurrent, processKey), false, nil
	}
	drained, err := fullyDrained(old, current, processKey)
	if err != nil || !drained {
		return false, false, err
	}
	exited, parseErr := strconv.ParseUint(p["ExecMainExitTimestampMonotonic"], 10, 64)
	if parseErr != nil || exited < started || p["ExecMainCode"] != "1" || p["ExecMainStatus"] != "0" || current.State != "inactive" {
		return false, false, errors.New("Custom Artisan command did not verify a successful completion; inspect its logs and application outcome before any manual retry")
	}
	r.Completed = true
	g.NaturalResumes[old.Name] = r
	if err = saveGeneration(*g); err != nil {
		return false, false, err
	}
	return true, true, nil
}
