package scheduler

import (
	"context"
	"errors"
	"os"

	"github.com/google/uuid"
)

// entryLoaderHost selects only this entry's acknowledged published authority.
// Ready and whole-branch certification concern retirement of earlier work;
// neither substitutes for the exact loader receipt. In particular the cron
// dispatcher must not demand an empty cron hierarchy while running inside it.
func entryLoaderHost(j journal, proofs backendProofs, e Entry) (*legacyHost, error) {
	current, ok := j.Entries[e.ID]
	if !ok || !current.Enabled || !e.Enabled || current.Identity != e.Identity || identity(current) != identity(e) || current.MigrationNonce != e.MigrationNonce {
		return nil, errors.New("Scheduler loader registration changed")
	}
	if e.MigrationNonce == "" {
		if j.Schema < 4 {
			// Historical nonce-less registrations retain their historical schema
			// semantics only. Current-format activation/materialization must give
			// them a nonce before this binary admits new execution authority.
			return nil, nil
		}
		return nil, errors.New("Refresh this Scheduler registration to obtain a current loader acknowledgement")
	}
	if _, err := uuid.Parse(e.MigrationNonce); err != nil {
		return nil, errors.New("Invalid Scheduler loader nonce")
	}
	m, ok := j.Migrations[e.Version]
	if !ok {
		return nil, errors.New("Scheduler loader migration evidence is missing")
	}
	core, ok := m.Replacements[e.ID]
	want := hashBytes([]byte(CronContent(e)))
	if !ok || core.Nonce != e.MigrationNonce || core.Hash != want || m.Deletions[e.ID] {
		return nil, errors.New("Scheduler loader core evidence changed")
	}
	if a, exists := proofs.Branches[e.Version]; exists {
		target, found := a.Migration.Replacements[e.ID]
		if a.Preparing || a.Bindings[e.ID] != e.Identity || !found || target.Nonce != e.MigrationNonce || target.Hash != want || !target.Received || a.Migration.Deletions[e.ID] {
			return nil, errors.New("Waiting for the exact Scheduler launcher acknowledgement")
		}
		if m.Host != nil || a.Boot != a.Host.Boot {
			return nil, errors.New("Scheduler loader provider evidence conflicts")
		}
		if err := ValidateBackendIdentity(a.Host); err != nil {
			return nil, err
		}
		host := a.Host
		return &host, nil
	}
	if m.Host == nil || !core.Received {
		return nil, errors.New("Waiting for Scheduler loader provider acknowledgement")
	}
	if err := ValidateBackendIdentity(*m.Host); err != nil {
		return nil, err
	}
	host := *m.Host
	return &host, nil
}

// Caller holds the core gate. This is an admission check only: it neither
// creates receipts nor certifies retirement, refreshes helpers or drains jobs.
func requireEntryLoaderAcknowledgement(ctx context.Context, j journal, e Entry) error {
	if j.Schema == loaderEpochSchema {
		_, err := RequireEntryLoaderBinding(ctx, j, e)
		return err
	}
	proofs, err := readBackendProofs()
	if err != nil {
		return err
	}
	host, err := entryLoaderHost(j, proofs, e)
	if err != nil {
		return err
	}
	if err = owned(CronPath(e.ID), false); err != nil {
		return err
	}
	raw, err := os.ReadFile(CronPath(e.ID))
	if err != nil || string(raw) != CronContent(e) {
		return errors.New("Scheduler acknowledged launcher changed")
	}
	if host != nil {
		return VerifyCronBackendCurrent(ctx, *host)
	}
	return nil
}

// Certification does not make an arbitrary old nonce a valid launcher. Keep
// receipt admission per entry so disabling another registration cannot block
// every remaining launcher in an already certified branch.
func verifyCertifiedEntryReceipt(ctx context.Context, j journal, nonce string, id int, host *legacyHost) error {
	e, ok := j.Entries[id]
	if id <= 0 || !ok || host == nil || nonce == "" || nonce != e.MigrationNonce {
		return errors.New("Stale certified Scheduler receipt")
	}
	if err := requireEntryLoaderAcknowledgement(ctx, j, e); err != nil {
		return err
	}
	return cronReceiptOrigin(*host)
}
