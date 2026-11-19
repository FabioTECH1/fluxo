package scheduler

import (
	"bytes"
	"context"
	"errors"
	"fluxo/internal/services/executiondomain"
	"fluxo/internal/syscmd"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BridgeReady inspects current authority without initializing or reconciling
// journals. Callers retain historical readers; this does not retire old proofs.
func BridgeReady(ctx context.Context, dbPath string) error {
	return bridgeReady(ctx, dbPath, requireEntryLoaderAcknowledgement)
}

// Activation supplies its separate conversion-only entry check. Certification,
// execution and ordinary readiness always retain current loader admission.
func bridgeReady(ctx context.Context, dbPath string, entryReady func(context.Context, journal, Entry) error) error {
	if absent() {
		return nil
	}
	j, err := read()
	if err != nil {
		return err
	}
	return bridgeReadyState(ctx, dbPath, j, entryReady)
}

// BridgeReadyForInstaller treats only a provably stale installer hold as
// resumable. It changes no journal bytes; BeginUpgrade still owns recovery.
func BridgeReadyForInstaller(ctx context.Context, dbPath string) error {
	if absent() {
		return nil
	}
	j, err := read()
	if err != nil {
		return err
	}
	boot, err := bootID()
	if err != nil {
		return err
	}
	j, err = withoutStaleInstallerHold(j, boot, startKey)
	if err != nil {
		return err
	}
	return bridgeReadyState(ctx, dbPath, j, requireEntryLoaderAcknowledgement)
}

func withoutStaleInstallerHold(j journal, boot string, processStart func(int) (string, error)) (journal, error) {
	if j.Upgrade == nil {
		return j, nil
	}
	if err := checkUpgradeOwnership(j, boot, processStart); err != nil {
		return j, err
	}
	holds := make(map[string]string, len(j.Holds))
	for k, v := range j.Holds {
		holds[k] = v
	}
	j.Holds = holds
	releaseUpgradeHolds(&j, j.Upgrade.ID)
	j.Upgrade = nil
	return j, nil
}

func bridgeReadyState(ctx context.Context, dbPath string, j journal, entryReady func(context.Context, journal, Entry) error) error {
	var err error
	if historicalRetirementPending(j) {
		return errors.New("finish fenced historical Scheduler retirement before certification")
	}
	if err = AuditLoaderArchiveHashesLocked(); err != nil {
		return err
	}
	if j.Schema == executiondomain.SchedulerLoaderSchema {
		if err = RequireLoaderEpochReadyLocked(ctx, j); err != nil {
			return err
		}
	}
	if (j.Schema != 2 && j.Schema != 3 && j.Schema != 4 && j.Schema != executiondomain.CurrentSchedulerSchema) || j.Upgrade != nil || len(j.Holds) > 0 {
		return errors.New("finish Scheduler initialization, installer or maintenance holds")
	}
	for _, boot := range j.Legacy {
		if boot != "" {
			return errors.New("complete tracked Scheduler receipts and legacy execution drainage")
		}
	}
	p, err := readBackendProofs()
	if err != nil {
		return err
	}
	for _, a := range p.Branches {
		if !a.Certified || a.Preparing {
			return errors.New("complete Scheduler backend proof qualification")
		}
	}
	for _, e := range j.Entries {
		if !e.Enabled {
			continue
		}
		if !e.Ready || e.Database != dbPath {
			return errors.New("Scheduler registration is not ready for this database")
		}
		if err = entryReady(ctx, j, e); err != nil {
			return err
		}
		if err = bridgeTrigger(e); err != nil {
			return err
		}
	}
	for _, v := range j.Invocations {
		if err = invocationQuiescent(ctx, v); err != nil {
			return err
		}
	}
	if len(j.Entries) > 0 {
		path := filepath.Join(stateDir, "dispatcher")
		if err := trustedFile(path); err != nil {
			return err
		}
		out, err := syscmd.Run(ctx, 5*time.Second, path, "--scheduler-support")
		support := strings.TrimSpace(out)
		if err != nil || (support != "3" && support != "4" && support != "5" && support != "6" && support != "7") || (j.Schema == 3 && support != "4" && support != "5" && support != "6" && support != "7") || (j.Schema == 4 && support != "5" && support != "6" && support != "7") || (j.Schema == executiondomain.SchedulerLoaderSchema && support != "6" && support != "7") {
			return errors.New("retained Scheduler dispatcher lacks the required execution reader protocol")
		}
	}
	return nil
}

// BridgeStateExists distinguishes a truly absent subsystem from a partial one.
func BridgeStateExists() bool {
	for _, p := range []string{stateDir, stateDir + ".initialized", filepath.Join(stateDir, "backend-proofs.initializing")} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			return true
		}
	}
	return false
}

// BridgeRegistration checks an exact row against its current guarded trigger.
func BridgeRegistration(ctx context.Context, expected Entry, dbPath string) error {
	j, err := read()
	if err != nil {
		return err
	}
	e, ok := j.Entries[expected.ID]
	if !ok || !e.Enabled || !e.Ready || e.Database != dbPath || e.SiteID != expected.SiteID || e.Directory != expected.Directory || e.Command != expected.Command || e.Version != expected.Version {
		return errors.New("Scheduler row lacks a matching guarded registration")
	}
	if err = requireEntryLoaderAcknowledgement(ctx, j, e); err != nil {
		return err
	}
	return bridgeTrigger(e)
}

func bridgeTrigger(e Entry) error {
	if err := owned(CronPath(e.ID), false); err != nil {
		return err
	}
	b, err := os.ReadFile(CronPath(e.ID))
	if err != nil {
		return err
	}
	if !bytes.Equal(b, []byte(CronContent(e))) {
		return errors.New("Scheduler trigger differs from its current registration")
	}
	return nil
}

func BridgeRegisteredIDs() ([]int, error) {
	if absent() {
		return nil, nil
	}
	j, err := read()
	if err != nil {
		return nil, err
	}
	var ids []int
	for id, e := range j.Entries {
		if e.Enabled {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
