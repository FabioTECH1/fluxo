package scheduler

import (
	"context"
	"errors"
)

// A certified historical launcher can belong to an earlier boot. Conversion
// must retain that proof, rather than demand that it acknowledge today's cron
// process before schema5 can publish its first loader epoch. This check is used
// only by the stopped-panel, exclusively admitted activation planner. It grants
// no dispatch, receipt, certification or current loader authority.
func requireActivationEntryAcknowledgement(ctx context.Context, j journal, e Entry) error {
	if j.Schema >= loaderEpochSchema {
		return requireEntryLoaderAcknowledgement(ctx, j, e)
	}
	proofs, err := readBackendProofs()
	if err != nil {
		return err
	}
	host, err := entryLoaderHost(j, proofs, e)
	if err != nil {
		return err
	}
	if err = bridgeTrigger(e); err != nil || host == nil {
		return err
	}
	current, err := RequalifyCronBackend(ctx, *host)
	if err != nil {
		return err
	}
	if current == *host {
		return nil
	}
	if err = validatePriorBootActivation(j, proofs, e, *host, current); err != nil {
		return err
	}
	// Earlier-boot processes cannot survive, but current untracked cron work
	// must still drain naturally. Never inspect the obsolete cgroup inode as if
	// it were the current provider, or replace the retained proof with this one.
	return VerifyCronBackendEmpty(ctx, current)
}

func validatePriorBootActivation(j journal, proofs backendProofs, e Entry, saved, current legacyHost) error {
	if j.Schema < 2 || j.Schema >= loaderEpochSchema || j.HistoricalRetirement != nil || j.LoaderEpochEstablished || len(j.Legacy) != 0 || j.Upgrade != nil || len(j.Holds) != 0 || !e.Enabled || !e.Ready {
		return errors.New("finish historical Scheduler retirement before conversion")
	}
	a, ok := proofs.Branches[e.Version]
	if !ok || !a.Certified || a.Preparing || a.Host != saved {
		return errors.New("conversion requires certified historical Scheduler backend evidence")
	}
	// Reuse the exact per-entry historical receipt/binding checks. A Ready flag
	// or whole-branch certificate cannot stand in for retained receipt evidence.
	host, err := entryLoaderHost(j, proofs, e)
	if err != nil || host == nil || *host != saved {
		return errors.New("conversion requires the exact historical Scheduler launcher acknowledgement")
	}
	if err = ValidateBackendIdentity(current); err != nil {
		return err
	}
	if saved.Boot == current.Boot || saved.Backend != current.Backend || saved.Group != current.Group || saved.Configuration != current.Configuration {
		return errors.New("Scheduled-job engine identity changed; conversion only accepts a qualified new boot under the unchanged saved backend contract")
	}
	return nil
}
