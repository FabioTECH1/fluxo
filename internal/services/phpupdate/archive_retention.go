package phpupdate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"reflect"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/scheduler"
	"github.com/google/uuid"
)

var errArchiveCleanupDeferred = errors.New("PHP archive cleanup is deferred while maintenance or recovery is pending")

type archiveRetentionQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type archiveGeneration struct {
	Order   int64
	Value   recoveryGeneration
	Settled bool
}

type archivePruneCandidate struct {
	Old, Successor archiveGeneration
}

func canonicalGenerationID(id string) bool {
	u, err := uuid.Parse(id)
	return err == nil && u.String() == id
}

// Read the full history, never the presentation queries which hide superseded
// attempts. Columns and payload must agree before any history authorizes unlink.
func readArchiveGenerations(ctx context.Context, q archiveRetentionQuery, settledOnly bool) ([]archiveGeneration, error) {
	rows, err := q.QueryContext(ctx, `SELECT g.id,g.version,g.head_operation_id,g.state,g.payload,
	 o.rowid,o.kind,o.version,h.kind,h.version,h.status,
	 COALESCE(a.generation_id,''),COALESCE(a.parent_id,'')
	 FROM php_update_generations g
	 LEFT JOIN php_update_operations o ON o.id=g.id
	 LEFT JOIN php_update_operations h ON h.id=g.head_operation_id
	 LEFT JOIN php_update_recovery_attempts a ON a.operation_id=h.id ORDER BY o.rowid`)
	if err != nil {
		return nil, err
	}
	var result []archiveGeneration
	for rows.Next() {
		var id, branch, head, state, raw, originalKind, originalBranch, headKind, headBranch, headStatus, attemptGeneration, parent string
		var item archiveGeneration
		if err = rows.Scan(&id, &branch, &head, &state, &raw, &item.Order, &originalKind, &originalBranch, &headKind, &headBranch, &headStatus, &attemptGeneration, &parent); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(raw), &item.Value); err != nil {
			break
		}
		g := item.Value
		if !canonicalGenerationID(id) || !canonicalGenerationID(head) || !ValidBranch(branch) || item.Order <= 0 ||
			g.ID != id || g.Head != head || g.Version != branch || generationState(g) != state || originalKind != "apply" || originalBranch != branch || headBranch != branch {
			err = errors.New("inconsistent PHP archive generation identity")
			break
		}
		if head == id {
			if headKind != "apply" || attemptGeneration != "" {
				err = errors.New("inconsistent PHP archive generation head")
				break
			}
		} else if (headKind != "repair" && headKind != "rollback") || attemptGeneration != id || !canonicalGenerationID(parent) {
			err = errors.New("inconsistent PHP recovery attempt linkage")
			break
		}
		item.Settled = !g.Pending && headStatus == "succeeded" && ((g.State == "succeeded" && g.Goal == "after" && g.VerifiedGoal == "after") || (g.State == "rolled_back" && g.Goal == "before" && g.VerifiedGoal == "before"))
		if settledOnly && !item.Settled {
			err = errArchiveCleanupDeferred
			break
		}
		result = append(result, item)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Validate parent ordering after closing rows (the live database has one
	// connection); no query may silently accept an orphan recovery head.
	for _, item := range result {
		g := item.Value
		if g.Head == g.ID {
			continue
		}
		var count int
		err = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM php_update_recovery_attempts a
		 JOIN php_update_operations h ON h.id=a.operation_id JOIN php_update_operations p ON p.id=a.parent_id
		 WHERE a.operation_id=? AND a.generation_id=? AND p.version=? AND p.rowid>=? AND p.rowid<h.rowid
		 AND (p.id=? OR EXISTS(SELECT 1 FROM php_update_recovery_attempts b WHERE b.operation_id=p.id AND b.generation_id=?))`, g.Head, g.ID, g.Version, item.Order, g.ID, g.ID).Scan(&count)
		if err != nil {
			return nil, err
		}
		if count != 1 {
			return nil, errors.New("invalid PHP recovery parent")
		}
	}
	return result, nil
}

func archiveRetentionHeads(items []archiveGeneration) (map[string]archiveGeneration, map[string]archiveGeneration, map[string]archiveGeneration) {
	latest, forward, rollback := map[string]archiveGeneration{}, map[string]archiveGeneration{}, map[string]archiveGeneration{}
	for _, item := range items {
		g := item.Value
		if item.Order > latest[g.Version].Order {
			latest[g.Version] = item
		}
		if !item.Settled {
			continue
		}
		if g.State == "succeeded" && item.Order > forward[g.Version].Order {
			forward[g.Version] = item
		}
	}
	for _, item := range items {
		g := item.Value
		if item.Settled && g.Archives.RollbackAvailable && item.Order <= forward[g.Version].Order && item.Order > rollback[g.Version].Order {
			rollback[g.Version] = item
		}
	}
	return latest, forward, rollback
}

func archivePruneCandidates(items []archiveGeneration, pinned map[string]bool) []archivePruneCandidate {
	latest, forward, rollback := archiveRetentionHeads(items)
	var result []archivePruneCandidate
	for _, item := range items {
		g := item.Value
		next := forward[g.Version]
		if !item.Settled || next.Order <= item.Order || latest[g.Version].Value.ID == g.ID || pinned[g.ID] {
			continue
		}
		// An acknowledged no-rollback update does not discard the last complete
		// rollback-capable set. This exception grants it no new recovery rights.
		if !next.Value.Archives.RollbackAvailable && rollback[g.Version].Value.ID == g.ID {
			continue
		}
		result = append(result, archivePruneCandidate{Old: item, Successor: next})
	}
	return result
}

func archiveCleanupIdle(ctx context.Context, q archiveRetentionQuery) error {
	var pending int
	if err := q.QueryRowContext(ctx, `SELECT
	 (SELECT COUNT(*) FROM php_update_operations WHERE status IN ('queued','running')) +
	 (SELECT COUNT(*) FROM php_update_generations WHERE state NOT IN ('succeeded','rolled_back')) +
	 (SELECT COUNT(*) FROM php_update_barriers) +
	 (SELECT COUNT(*) FROM php_repository_migrations WHERE state='pending') +
	 (SELECT COUNT(*) FROM php_update_operations o WHERE kind IN ('apply','verify')
	 AND status NOT IN ('queued','running','succeeded') AND checkpoint IN ('package_transaction','packages_updated')
	 AND NOT EXISTS(SELECT 1 FROM php_update_generations g WHERE g.id=o.id)
	 AND NOT EXISTS(SELECT 1 FROM php_update_recovery_attempts a WHERE a.operation_id=o.id)
	 AND NOT EXISTS(SELECT 1 FROM php_update_operations n WHERE n.version=o.version
	 AND n.kind IN ('apply','verify') AND n.status='succeeded' AND n.rowid>o.rowid))`).Scan(&pending); err != nil {
		return err
	}
	if pending != 0 {
		return errArchiveCleanupDeferred
	}
	return nil
}

// Caller owns exclusive lifecycle, then package locks. Hashing and snapshot
// inspection happen outside the short SQLite write boundary used for unlink.
func (m *Manager) pruneArchivesLocked(ctx context.Context) (int, error) {
	if os.Geteuid() != 0 {
		return 0, ErrSupervisor
	}
	if err := rootOwned(m.DataDir); err != nil {
		return 0, err
	}
	if err := archiveCleanupIdle(ctx, database.DB); err != nil {
		return 0, err
	}
	if pending, err := scheduler.Pending(); err != nil {
		return 0, err
	} else if pending {
		return 0, errArchiveCleanupDeferred
	}
	items, err := readArchiveGenerations(ctx, database.DB, true)
	if err != nil {
		return 0, err
	}
	store, err := openArchiveRetentionStore(m.DataDir, 0)
	if err != nil {
		return 0, err
	}
	defer store.Close()
	pinned, footprint, err := store.snapshotPins(ctx)
	if err != nil {
		return 0, err
	}
	candidates := archivePruneCandidates(items, pinned)
	var failures []error
	for _, candidate := range candidates {
		if err = ctx.Err(); err != nil {
			return 0, err
		}
		// Empty histories must not repeatedly hash the successor and starve
		// populated candidates later in a long retained audit history.
		set, e := store.inspectGeneration(ctx, candidate.Old.Value, false)
		if e != nil {
			failures = append(failures, e)
			continue
		}
		if len(set.Files) == 0 {
			set.Close()
			continue
		}
		// A successor must still have its complete authenticated package sets.
		anchor, e := store.inspectGeneration(ctx, candidate.Successor.Value, true)
		if e != nil {
			set.Close()
			failures = append(failures, e)
			continue
		}
		count, e := m.unlinkObsoleteArchiveBatch(ctx, store, set, anchor, candidate, items, footprint)
		anchor.Close()
		set.Close()
		// One bounded batch per pass. Missing files are authenticated no-ops on
		// the next pass, with the manifest and audit history left in place.
		return count, e
	}
	return 0, errors.Join(failures...)
}

func (m *Manager) unlinkObsoleteArchiveBatch(ctx context.Context, store *archiveRetentionStore, set, anchor *archiveRetentionSet, candidate archivePruneCandidate, observed []archiveGeneration, footprint string) (int, error) {
	// No long hashing or copies while admission is excluded by this transaction.
	conn, err := database.DB.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return 0, err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	if err = archiveCleanupIdle(ctx, conn); err != nil {
		return 0, err
	}
	current, err := readArchiveGenerations(ctx, conn, true)
	if err != nil {
		return 0, err
	}
	if !reflect.DeepEqual(observed, current) {
		return 0, errArchiveCleanupDeferred
	}
	now, err := store.snapshotFootprint(ctx)
	if err != nil {
		return 0, err
	}
	if now != footprint {
		return 0, errArchiveCleanupDeferred
	}
	if err = set.Unchanged(); err != nil {
		return 0, err
	}
	if err = anchor.Unchanged(); err != nil {
		return 0, err
	}
	count, bytes, removeErr := set.UnlinkBatch(ctx, 64)
	if count > 0 {
		message := fmt.Sprintf("Removed %d obsolete PHP package archives (%d bytes), superseded by verified update %s. Manifests and history retained.", count, bytes, candidate.Successor.Value.ID)
		log.Printf("PHP archive cleanup %s: %s", candidate.Old.Value.ID, message)
		_, err = conn.ExecContext(ctx, `INSERT INTO php_update_events(operation_id,phase,message,created_at) VALUES (?,'archives_pruned',?,?)`, candidate.Old.Value.Head, message, time.Now().Unix())
		if err == nil {
			_, err = conn.ExecContext(ctx, "COMMIT")
		}
	}
	return count, errors.Join(removeErr, err)
}

func (m *Manager) cleanupArchivesAfterSuccess(ctx context.Context) {
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := m.pruneArchivesLocked(bounded); err != nil && !errors.Is(err, errArchiveCleanupDeferred) {
		log.Printf("PHP package archive cleanup deferred: %s", redact(err.Error()))
	}
}

func (m *Manager) cleanupArchivesInBackground(ctx context.Context) {
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	lockCtx, stop := context.WithTimeout(bounded, time.Second)
	defer stop()
	release, err := packages.Lifecycle(lockCtx, false)
	if err != nil {
		return
	}
	defer release()
	unlock, err := packages.Acquire(lockCtx)
	if err != nil {
		return
	}
	defer unlock()
	m.cleanupArchivesAfterSuccess(bounded)
}
