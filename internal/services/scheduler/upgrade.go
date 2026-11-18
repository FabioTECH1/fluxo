package scheduler

import (
	"context"
	"errors"
	"fluxo/internal/services/executiondomain"
	"fluxo/internal/syscmd"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type upgradeLease struct {
	ID    string `json:"id"`
	Boot  string `json:"boot"`
	PID   int    `json:"pid"`
	Start string `json:"start"`
}

func startKey(pid int) (string, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	i := strings.LastIndex(string(raw), ")")
	if i < 0 {
		return "", errors.New("Invalid installer identity")
	}
	f := strings.Fields(string(raw)[i+1:])
	if len(f) < 20 {
		return "", errors.New("Invalid installer identity")
	}
	return f[19], nil
}

// The caller holds the exclusive PHP lifecycle lease. The durable marker also
// prevents newly started panel processes from queuing work during health checks.
func BeginUpgrade(ctx context.Context) (string, error) {
	if err := initialize(ctx); err != nil {
		return "", err
	}
	unlock, err := lock(ctx)
	if err != nil {
		return "", err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return "", err
	}
	if err = AuditLoaderArchiveHashesLocked(); err != nil {
		return "", err
	}
	boot, err := bootID()
	if err != nil {
		return "", err
	}
	if err = checkUpgradeAdmission(ctx, j, boot, startKey); err != nil {
		return "", err
	}
	key, err := startKey(os.Getpid())
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	setUpgradeHolds(&j, id)
	j.Upgrade = &upgradeLease{ID: id, Boot: boot, PID: os.Getpid(), Start: key}
	return id, save(j)
}

// The caller retains the journal lock and exclusive lifecycle admission. Old
// dispatchers do not understand Upgrade, so check for a last racing submission
// before atomically publishing the old-reader-compatible branch holds.
// No invocation is reconciled, retired or rewritten by installer admission.
func checkUpgradeAdmission(ctx context.Context, j journal, boot string, processStart func(int) (string, error)) error {
	if err := checkUpgradeOwnership(j, boot, processStart); err != nil {
		return err
	}
	for _, inv := range j.Invocations {
		if err := invocationQuiescent(ctx, inv); err != nil {
			return err
		}
	}
	return nil
}

func checkUpgradeOwnership(j journal, boot string, processStart func(int) (string, error)) error {
	old := j.Upgrade
	if old != nil && old.Boot == boot {
		key, err := processStart(old.PID)
		if err == nil && key == old.Start {
			return errors.New("Another installer is active")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for _, owner := range j.Holds {
		if old == nil || owner != old.ID {
			return errors.New("Resolve PHP maintenance before upgrading Fluxo")
		}
	}
	return nil
}

func setUpgradeHolds(j *journal, id string) {
	holds := make(map[string]string)
	for _, e := range j.Entries {
		holds[e.Version] = id
	}
	if len(holds) == 0 {
		// Legacy registration rejects any nonempty hold map. This well-formed
		// denial marker also fences the first registration; it does not select
		// a PHP runtime or grant any execution authority.
		holds["8.4"] = id
	}
	j.Holds = holds
}

func releaseUpgradeHolds(j *journal, id string) {
	for branch, owner := range j.Holds {
		if owner == id {
			delete(j.Holds, branch)
		}
	}
}

func EndUpgrade(ctx context.Context, id string) error {
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	if j.Upgrade == nil || j.Upgrade.ID != id || j.Upgrade.PID != os.Getpid() {
		return errors.New("Installer lease ownership changed")
	}
	if j.PanelProof != nil {
		out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", "fluxo.service", "--property=MainPID", "--value")
		pid, e := strconv.Atoi(strings.TrimSpace(out))
		if err != nil || e != nil || pid <= 1 {
			j.PanelProof = nil
		} else if hash, e := fileDigest(fmt.Sprintf("/proc/%d/exe", pid)); e != nil || hash != j.PanelProof.Binary {
			j.PanelProof = nil
		}
	}
	releaseUpgradeHolds(&j, id)
	j.Upgrade = nil
	return save(j)
}
func UpgradeBusy() (bool, error) {
	if absent() {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlock, err := lock(ctx)
	if err != nil {
		return false, err
	}
	defer unlock()
	j, err := read()
	return j.Upgrade != nil, err
}

// VerifyInstallerLease lets the installer's Node child reuse its already-held
// exclusive lifecycle lease without attempting a recursive flock acquisition.
func VerifyInstallerLease(ctx context.Context, id string) error {
	if os.Geteuid() != 0 {
		return errors.New("Installer lease requires root")
	}
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	if j.Upgrade == nil || j.Upgrade.ID != id {
		return errors.New("Installer lease is unavailable")
	}
	boot, err := bootID()
	if err != nil || boot != j.Upgrade.Boot {
		return errors.New("Installer lease belongs to another boot")
	}
	key, err := startKey(j.Upgrade.PID)
	if err != nil || key != j.Upgrade.Start {
		return errors.New("Installer lease owner is no longer active")
	}
	return nil
}

func validateUpgrade(u *upgradeLease) error {
	if u == nil {
		return nil
	}
	if _, err := uuid.Parse(u.ID); err != nil {
		return err
	}
	if _, err := uuid.Parse(u.Boot); err != nil {
		return err
	}
	if u.PID <= 0 {
		return errors.New("Invalid installer PID")
	}
	if _, err := strconv.ParseUint(u.Start, 10, 64); err != nil {
		return err
	}
	return nil
}

func CompatibilityRequired() (bool, error) {
	if required, err := RequiredSupport(); err != nil || required >= 3 {
		return required >= 3, err
	}
	if absent() {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlock, err := lock(ctx)
	if err != nil {
		return false, err
	}
	defer unlock()
	j, err := read()
	return len(j.Entries) > 0 || len(j.Invocations) > 0 || len(j.Holds) > 0 || len(j.Legacy) > 0 || len(j.Migrations) > 0, err
}

// RequiredSupport is negotiated before installer rollback, independently of
// whether the schema-2 core can still be read by an older dispatcher.
func RequiredSupport() (int, error) {
	floor, floorErr := executiondomain.ReadFloor(filepath.Dir(databasePath))
	if floorErr != nil {
		return executiondomain.SchedulerCompiledReaderSupport, floorErr
	}
	if floor != nil && floor.SchedulerSupport >= 4 {
		return floor.SchedulerSupport, nil
	}
	if !absent() {
		j, err := read()
		if err != nil {
			return executiondomain.SchedulerCompiledReaderSupport, err
		}
		if j.Schema == executiondomain.CurrentSchedulerSchema {
			return executiondomain.SchedulerCompiledReaderSupport, nil
		}
		if j.Schema >= 3 {
			return executiondomain.MinimumSchedulerReaderSupport(j.Schema), nil
		}
	}

	if _, err := os.Lstat(proofInitialization()); !os.IsNotExist(err) {
		return 3, errors.New("Scheduler backend initialization requires reconciliation")
	}
	if _, err := os.Lstat(proofAnchor()); err == nil {
		if _, err = readBackendProofs(); err != nil {
			return 0, err
		}
		return 3, nil
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	if _, err := os.Lstat(proofPath()); !os.IsNotExist(err) {
		return 0, errors.New("Scheduler backend evidence needs reconciliation")
	}
	return 2, nil
}

// SignalUpgrade releases only the journaled, identity-verified root helper.
func SignalUpgrade(ctx context.Context, id string) error {
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	u := j.Upgrade
	if u == nil || u.ID != id {
		return errors.New("Installer lease changed")
	}
	fd, err := unix.PidfdOpen(u.PID, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	boot, err := bootID()
	if err != nil {
		return err
	}
	key, err := startKey(u.PID)
	if err != nil {
		return err
	}
	if u.Boot != boot || u.Start != key {
		return errors.New("Installer lease process changed")
	}
	return unix.PidfdSendSignal(fd, unix.SIGUSR1, nil, 0)
}
