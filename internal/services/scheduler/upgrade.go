package scheduler

import (
	"context"
	"errors"
	"fluxo/internal/syscmd"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
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
	if len(j.Holds) > 0 {
		return "", errors.New("Resolve PHP maintenance before upgrading Fluxo")
	}
	boot, err := bootID()
	if err != nil {
		return "", err
	}
	if old := j.Upgrade; old != nil && old.Boot == boot {
		key, e := startKey(old.PID)
		if e == nil && key == old.Start {
			return "", errors.New("Another installer is active")
		}
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return "", e
		}
	}
	key, err := startKey(os.Getpid())
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	j.Upgrade = &upgradeLease{ID: id, Boot: boot, PID: os.Getpid(), Start: key}
	return id, save(j)
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
