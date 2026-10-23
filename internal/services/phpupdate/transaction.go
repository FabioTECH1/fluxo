package phpupdate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/scheduler"
	"fluxo/internal/syscmd"
)

func (m *Manager) packageTransaction(ctx context.Context, o Operation, g *recoveryGeneration) error {
	if !g.SchedulerManaged {
		return errors.New("Scheduler coordination is missing")
	}
	if err := scheduler.AssertDrained(ctx, g.Version, g.ID, g.Schedulers); err != nil {
		return err
	}
	for _, u := range g.Attempt.Units {
		if !managedUnitPattern.MatchString(u.Name) {
			continue
		}
		b, err := getBarrier(u.Name)
		if err != nil {
			return err
		}
		if err = verifyHeldBarrier(ctx, b); err != nil {
			return err
		}
		state, err := observeUnit(ctx, u.Name, u.CGroup)
		if err != nil {
			return err
		}
		drained, err := fullyDrained(u, state, processKey)
		if err != nil {
			return err
		}
		if !drained {
			return errors.New("Consumers are no longer quiescent; no package transaction was started")
		}
	}
	cleanup, err := preparePolicyTarget()
	if err != nil {
		return err
	}
	defer cleanup()
	exe, err := trustedExecutable()
	if err != nil {
		return err
	}
	env, err := m.workerEnvironment()
	if err != nil {
		return err
	}
	g.State = "package_transaction"
	if err = saveGeneration(*g); err != nil {
		return err
	}
	if _, err = database.DB.Exec("UPDATE php_update_operations SET checkpoint='package_transaction' WHERE id=?", o.ID); err != nil {
		return err
	}
	_ = progress(o.ID, "applying", "Applying the verified retained PHP package set; this continues if the panel closes")
	err = syscmd.RunMaintenance(ctx, "", &bridgeWriter{id: o.ID}, env, "/usr/bin/unshare", "--mount", "--propagation", "private", "--", exe, "php-update-transaction", o.ID)
	if err != nil {
		_ = finish(o.ID, "partial", "The package transaction did not verify completely. Review Repair or Roll back PHP packages; no automatic rollback was attempted. Operator repair may still be required.")
		return err
	}
	g.State = "packages_updated"
	if err = saveGeneration(*g); err != nil {
		return err
	}
	_, err = database.DB.Exec("UPDATE php_update_operations SET checkpoint='packages_updated' WHERE id=?", o.ID)
	return err
}

func (m *Manager) activateGeneration(ctx context.Context, o Operation, p Plan, g *recoveryGeneration) error {
	if err := validateConsumerProtocols(p, *g); err != nil {
		return err
	}
	ob, err := m.observePackages(ctx, o.ID, g.Version)
	if err != nil {
		return err
	}
	matched, err := validateRecoveryObservation(*g, ob, g.Goal)
	if err != nil {
		return err
	}
	if !matched {
		return errors.New("Recorded package revisions are not fully configured; no services were started")
	}
	if _, err = syscmd.Run(ctx, 30*time.Second, "/usr/sbin/php-fpm"+p.Version, "-t"); err != nil {
		return fmt.Errorf("FPM configuration needs repair before activation: %w", err)
	}
	g.State = "activating"
	if err = saveGeneration(*g); err != nil {
		return err
	}
	if err = m.startConsumers(ctx, g); err != nil {
		return err
	}
	// Only the original active FPM may be started. Package-maintainer scripts
	// cannot perform this activation in the private transaction namespace.
	for _, u := range g.Original.Units {
		if u.Name != "php"+p.Version+"-fpm.service" || !u.Active {
			continue
		}
		s, e := observeUnit(ctx, u.Name, u.CGroup)
		if e != nil {
			return e
		}
		if !s.Active && s.State != "activating" {
			if len(s.PIDs) > 0 {
				return errors.New("FPM still has draining processes")
			}
			if _, e = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "start", "--no-block", u.Name); e != nil {
				return e
			}
		}
	}
	deadline := time.NewTimer(45 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		ready := true
		for _, u := range g.Original.Units {
			if !u.Active {
				continue
			}
			if u.Natural {
				ok, _, err := naturalActivationReady(ctx, g, u)
				if err != nil {
					return err
				}
				ready = ready && ok
				continue
			}
			s, e := observeUnit(ctx, u.Name, u.CGroup)
			if e != nil {
				return e
			}
			if !s.Active {
				ready = false
			}
		}
		if ready {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Affected services did not start within the verification window; Repair can retry activation")
		case <-tick.C:
		}
	}
	before := g.Attempt
	for i := range before.Units {
		for _, original := range g.Original.Units {
			if before.Units[i].Name == original.Name {
				before.Units[i].Active = original.Active
			}
		}
	}
	return m.activate(ctx, o, p, before, g)
}
