package phpupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"fluxo/internal/database"
	"golang.org/x/sys/unix"
)

// A pidfd binds delivery to this process, including across concurrent PID reuse.
// Recheck its identity and managed cgroup after acquiring the descriptor.
func signalRecordedProcess(ctx context.Context, u unitSnapshot, pid int, signal unix.Signal) error {
	identity := u.PIDs[pid]
	if identity == "" {
		return errors.New("Signal target has no recorded process identity")
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	current, err := observeUnit(ctx, u.Name, u.CGroup)
	if err != nil {
		return err
	}
	key, err := processKey(pid)
	if err != nil {
		return err
	}
	if key != identity || current.PIDs[pid] != identity {
		return errors.New("Signal target changed before delivery")
	}
	return unix.PidfdSendSignal(fd, signal, nil, 0)
}

func retryUnconfirmedFPM(approved bool, old, current unitSnapshot, delivery, attempt, head string, check func(int) error) bool {
	return approved && attempt != head && delivery != "sent" && sameOriginalMain(old, current) && check(current.MainPID) != nil
}

func (m *Manager) saveActivation(o Operation, before runtimeSnapshot, generation []*recoveryGeneration) error {
	if len(generation) == 1 {
		g := generation[0]
		g.Attempt.Signalled = before.Signalled
		g.Attempt.SignalDelivery = before.SignalDelivery
		g.Attempt.SignalAttempts = before.SignalAttempts
		if err := saveGeneration(*g); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(before)
	if err != nil {
		return err
	}
	_, err = database.DB.Exec("UPDATE php_update_operations SET result=? WHERE id=?", string(raw), o.ID)
	return err
}

func (m *Manager) fpmRetryNeeded(ctx context.Context, g recoveryGeneration) (bool, error) {
	name := "php" + g.Version + "-fpm.service"
	if !g.Attempt.Signalled[name] || g.Attempt.SignalDelivery[name] == "sent" {
		return false, nil
	}
	for _, old := range g.Attempt.Units {
		if old.Name != name {
			continue
		}
		current, err := observeUnit(ctx, name, old.CGroup)
		if err != nil {
			return false, err
		}
		return retryUnconfirmedFPM(true, old, current, g.Attempt.SignalDelivery[name], "", g.Head, processCurrent), nil
	}
	return false, fmt.Errorf("Recorded FPM state is incomplete for PHP %s", strings.TrimSpace(g.Version))
}
