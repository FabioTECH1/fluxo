package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
)

type legacyTrigger struct {
	Nonce    string `json:"nonce"`
	Hash     string `json:"hash"`
	Received bool   `json:"received"`
}
type legacyMigration struct {
	Host           *legacyHost           `json:"host"`
	Replacements   map[int]legacyTrigger `json:"replacements"`
	Deletions      map[int]bool          `json:"deletions"`
	Marker         string                `json:"marker"`
	MarkerPhase    int                   `json:"marker_phase"`
	MarkerReceived bool                  `json:"marker_received"`
	PreviousMarker string                `json:"previous_marker"`
	Certified      bool                  `json:"certified"`
	Reason         string                `json:"reason"`
}

type panelProof struct {
	Boot   string `json:"boot"`
	Binary string `json:"binary"`
}

func legacyEpoch(ctx context.Context, j *journal, branch string) legacyMigration {
	m, ok := j.Migrations[branch]
	if !ok {
		m = legacyMigration{Replacements: map[int]legacyTrigger{}, Deletions: map[int]bool{}}
		host, err := qualifyBackendHost(ctx)
		if err != nil {
			m.Reason = err.Error()
		} else if host.Backend == "" {
			m.Host = &host
		}
	}
	m.Certified = false
	return m
}

func addLegacyReplacement(ctx context.Context, j *journal, e *Entry) error {
	boot, err := bootID()
	if err != nil {
		return err
	}
	e.Ready, e.LegacyBoot, e.MigrationNonce = false, boot, uuid.NewString()
	m := legacyEpoch(ctx, j, e.Version)
	m.Replacements[e.ID] = legacyTrigger{Nonce: e.MigrationNonce, Hash: hashBytes([]byte(CronContent(*e)))}
	delete(m.Deletions, e.ID)
	j.Migrations[e.Version], j.Legacy[e.Version] = m, boot
	return nil
}

func addLegacyDeletion(ctx context.Context, j *journal, branch string, id int) error {
	boot, err := bootID()
	if err != nil {
		return err
	}
	m := legacyEpoch(ctx, j, branch)
	delete(m.Replacements, id)
	m.Deletions[id] = true
	m.PreviousMarker = m.Marker
	m.Marker, m.MarkerPhase, m.MarkerReceived = uuid.NewString(), 1, false
	j.Migrations[branch], j.Legacy[branch] = m, boot
	if err = save(*j); err != nil {
		return err
	}
	if err = installDispatcher(); err != nil {
		return err
	}
	return publishMigrationMarker(branch, m)
}

func markerPath(branch string) string {
	return filepath.Join(cronDir, "fluxo-scheduler-migration-"+strings.ReplaceAll(branch, ".", "-"))
}
func markerContent(branch string, m legacyMigration) string {
	return fmt.Sprintf("# Fluxo Scheduler migration receipt; no application task\n* * * * * root %s scheduler-migration %s %s\n", filepath.Join(stateDir, "dispatcher"), branch, m.Marker)
}

// Ubuntu cron caches files using whole-second mtimes. A rename alone does not
// establish reload. Use a distinct timestamp and still require the nonce receipt.
func publishCron(path string, content []byte) error {
	var old int64
	if s, err := os.Stat(path); err == nil {
		old = s.ModTime().Unix()
	}
	if err := atomicFile(path, content, 0644); err != nil {
		return err
	}
	at := time.Now()
	if at.Unix() == old {
		at = time.Unix(old+1, 0)
	}
	return os.Chtimes(path, at, at)
}
func publishMigrationMarker(branch string, m legacyMigration) error {
	if raw, err := os.ReadFile(markerPath(branch)); err == nil {
		if err = owned(markerPath(branch), false); err != nil {
			return err
		}
		previous := m
		previous.Marker = m.PreviousMarker
		if string(raw) != markerContent(branch, m) && (m.PreviousMarker == "" || string(raw) != markerContent(branch, previous)) {
			// A mutation can follow an interrupted companion projection whose
			// marker file still contains its exact durable predecessor. Publishing
			// this harmless current marker grants no drain/readiness authority.
			proofs, err := readBackendProofs()
			a, ok := proofs.Branches[branch]
			if err != nil || !ok || !a.Preparing || (string(raw) != markerContent(branch, a.Previous) && string(raw) != markerContent(branch, a.Migration)) {
				return errors.New("Scheduler migration receipt file was modified")
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return publishCron(markerPath(branch), []byte(markerContent(branch, m)))
}

func hashBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func installDispatcher() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	if err = trustedFile(exe); err != nil {
		return err
	}
	b, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	return atomicFile(filepath.Join(stateDir, "dispatcher"), b, 0700)
}

func EntryReceipt(ctx context.Context, id int, nonce string) error {
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	j, err := read()
	unlock()
	if err != nil {
		return err
	}
	e, ok := j.Entries[id]
	if !ok {
		return errors.New("Unknown Scheduler receipt")
	}
	return Receipt(ctx, e.Version, nonce, id)
}

// Receipt can be produced only by a nonce embedded in a currently published
// trigger and invoked by the recorded cron daemon. Manual Run now cannot certify.
func Receipt(ctx context.Context, branch, nonce string, id int) error {
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	proofs, err := readBackendProofs()
	if err != nil {
		return err
	}
	if _, ok := proofs.Branches[branch]; ok {
		return receiveBackendReceipt(ctx, j, &proofs, branch, nonce, id)
	}
	m, ok := j.Migrations[branch]
	if !ok || m.Certified {
		return nil
	}
	if m.Host == nil {
		observation := BackendStatus(ctx)
		if observation.Message != "" {
			return diagnostic(observation.Code, observation.Message)
		}
		return diagnostic("waiting_for_backend_binding", "Scheduler setup is waiting for its installed engine to be verified. PHP has not been changed.")
	}
	host, err := qualifyBackendHost(ctx)
	if err != nil {
		return err
	}
	if host != *m.Host {
		return errors.New("Scheduler migration cron identity changed; review migration")
	}
	if err = cronReceiptOrigin(host); err != nil {
		return err
	}
	if id > 0 {
		target, ok := m.Replacements[id]
		if !ok || target.Nonce != nonce {
			return errors.New("Stale Scheduler migration receipt")
		}
		raw, err := os.ReadFile(CronPath(id))
		if err != nil {
			return err
		}
		if hashBytes(raw) != target.Hash {
			return errors.New("Scheduler migration trigger changed")
		}
		target.Received = true
		m.Replacements[id] = target
	} else {
		if m.Marker != nonce || m.MarkerPhase < 1 || m.MarkerPhase > 2 {
			return errors.New("Stale Scheduler deletion receipt")
		}
		for id := range m.Deletions {
			if _, err := os.Lstat(CronPath(id)); !errors.Is(err, os.ErrNotExist) {
				return errors.New("Deleted Scheduler trigger is still present")
			}
		}
		raw, err := os.ReadFile(markerPath(branch))
		if err != nil || string(raw) != markerContent(branch, m) {
			return errors.New("Scheduler deletion receipt trigger changed")
		}
		m.MarkerReceived = true
	}
	j.Migrations[branch] = m
	return save(j)
}

func reconcileLegacy(ctx context.Context, j *journal, branch string) error {
	boot, err := bootID()
	if err != nil {
		return err
	}
	proofs, err := readBackendProofs()
	if err != nil {
		return err
	}
	legacyBoot := j.Legacy[branch]
	if legacyBoot == "" {
		if a, ok := proofs.Branches[branch]; ok && !a.Certified {
			return diagnostic("legacy_origin_unproven", "Scheduler backend history remains unresolved; review its migration state.")
		}
		return nil
	}
	if legacyBoot != boot {
		// A naturally occurring boot is also valid evidence; never request one.
		if a, ok := proofs.Branches[branch]; ok {
			if a.Boot == boot {
				return diagnostic("legacy_origin_unproven", "Scheduler boot evidence conflicts with its backend history; review migration.")
			}
			for id, e := range j.Entries {
				if e.Version == branch && e.Enabled {
					if err = verifyRegistration(ctx, e); err != nil {
						return err
					}
					_ = id
				}
			}
			a.Certified = true
			a.Preparing = false
			a.RetiredByBoot = boot
			proofs.Branches[branch] = a
			if err = saveBackendProofs(proofs); err != nil {
				return err
			}
		}
		delete(j.Legacy, branch)
		for id, e := range j.Entries {
			if e.Version == branch {
				e.Ready = true
				j.Entries[id] = e
			}
		}
		return nil
	}
	if a, ok := proofs.Branches[branch]; ok && a.Boot == boot {
		return reconcileBackendAttempt(ctx, j, &proofs, branch)
	}
	m, ok := j.Migrations[branch]
	if !ok {
		return errors.New("Earlier Scheduler launches need migration review; complete the Fluxo installer and retry")
	}
	if m.Host == nil {
		observation := BackendStatus(ctx)
		if observation.Message != "" {
			return diagnostic(observation.Code, observation.Message)
		}
		return diagnostic("waiting_for_backend_binding", "Scheduler setup is waiting for its installed engine to be verified. PHP has not been changed.")
	}
	host, err := qualifyBackendHost(ctx)
	if err != nil {
		return err
	}
	if host != *m.Host {
		return errors.New("Scheduler migration cron identity changed; review migration")
	}
	for id, target := range m.Replacements {
		raw, err := os.ReadFile(CronPath(id))
		if err != nil || hashBytes(raw) != target.Hash {
			return errors.New("Scheduler migration trigger changed; review migration")
		}
		if !target.Received {
			return errors.New("Scheduler setup is waiting for cron to load its tracked launcher. PHP has not been changed")
		}
	}
	if len(m.Deletions) > 0 {
		current, readErr := os.ReadFile(markerPath(branch))
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		if string(current) != markerContent(branch, m) {
			previous := m
			previous.Marker = m.PreviousMarker
			if !errors.Is(readErr, os.ErrNotExist) && (m.PreviousMarker == "" || string(current) != markerContent(branch, previous)) {
				return errors.New("Scheduler migration receipt file was modified; review migration")
			}
			if err = publishMigrationMarker(branch, m); err != nil {
				return err
			}
		}
		for id := range m.Deletions {
			if _, err := os.Lstat(CronPath(id)); !errors.Is(err, os.ErrNotExist) {
				return errors.New("Scheduler deletion is incomplete")
			}
		}
		if !m.MarkerReceived {
			return errors.New("Scheduler setup is waiting for cron to retire an earlier trigger. PHP has not been changed")
		}
		if m.MarkerPhase == 1 {
			m.PreviousMarker = m.Marker
			m.Marker, m.MarkerPhase, m.MarkerReceived = uuid.NewString(), 2, false
			j.Migrations[branch] = m
			if err = save(*j); err != nil {
				return err
			}
			if err = publishMigrationMarker(branch, m); err != nil {
				return err
			}
			return errors.New("Scheduler setup is confirming retired triggers. PHP has not been changed")
		}
	}
	if err = verifyPanelBoundary(*j); err != nil {
		return err
	}
	if err = legacyDomainDrained(ctx, host); err != nil {
		return err
	}
	m.Certified = true
	j.Migrations[branch] = m
	delete(j.Legacy, branch)
	for id, e := range j.Entries {
		if e.Version == branch {
			e.Ready = true
			j.Entries[id] = e
		}
	}
	if len(m.Deletions) > 0 {
		if err = os.Remove(markerPath(branch)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func verifyPanelBoundary(j journal) error {
	boot, err := bootID()
	if err != nil {
		return err
	}
	if j.PanelProof == nil || j.PanelProof.Boot != boot {
		return diagnostic("panel_proof_required", panelProofRequiredMessage)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	hash, err := fileDigest(exe)
	if err != nil || hash != j.PanelProof.Binary {
		return diagnostic("panel_proof_required", "Scheduler panel migration belongs to another Fluxo binary. "+panelProofRequiredMessage)
	}
	return nil
}

func legacyDomainDrained(ctx context.Context, host legacyHost) error {
	// pids.current is hierarchical and charged before a fork becomes runnable.
	// Never subtract application/unknown jobs or infer quiescence from a PID scan.
	fd, err := os.Open("/sys/fs/cgroup" + host.controlGroup())
	if err != nil {
		return err
	}
	defer fd.Close()
	info, err := fd.Stat()
	if err != nil {
		return err
	}
	if info.Sys().(*syscall.Stat_t).Ino != host.GroupInode {
		return errors.New("Scheduler cron execution domain changed")
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/self/fd/%d/pids.current", fd.Fd()))
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return err
	}
	after, err := qualifyBackendHost(ctx)
	if err != nil {
		return err
	}
	if after != host {
		return errors.New("Scheduler cron identity changed during observation")
	}
	if n != 1 {
		return diagnostic("waiting_for_legacy_jobs", "Scheduler setup is waiting for earlier cron jobs to finish. PHP has not been changed")
	}
	return nil
}
