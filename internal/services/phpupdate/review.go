package phpupdate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/packages"
	"github.com/google/uuid"
)

var ErrReviewChanged = errors.New("PHP installation readiness changed or the review expired; choose Update to review installation again")

// ReviewPlan inspects installation readiness only after a package update has
// been selected. Discovery and GET requests never inspect application runtimes.
func (m *Manager) ReviewPlan(ctx context.Context, id string) (Plan, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	release, err := packages.Lifecycle(ctx, true)
	if err != nil {
		return Plan{}, err
	}
	defer release()
	unlock, err := packages.Acquire(ctx)
	if err != nil {
		return Plan{}, err
	}
	defer unlock()
	if active, err := ActiveOperation(); err != nil {
		return Plan{}, err
	} else if active != nil {
		return Plan{}, ErrBusy
	}
	p, err := GetPlan(id)
	if err != nil {
		return Plan{}, err
	}
	if err = currentDiscovery(p); err != nil {
		return Plan{}, err
	}
	impact, inspectErr := inspectImpact(ctx, p.Version)
	addPreservedExtensionNotes(&impact, p.Inventory)
	if inspectErr != nil {
		impact.BlockedReasons = append(impact.BlockedReasons, inspectErr.Error())
	}
	if err = validateNativeChanges(p.Inventory, impact); err != nil {
		impact.BlockedReasons = append(impact.BlockedReasons, err.Error())
	}
	if len(impact.BlockedReasons) == 0 {
		if _, err = captureRuntime(ctx, Plan{Version: p.Version, Impact: impact}); err != nil {
			impact.BlockedReasons = append(impact.BlockedReasons, err.Error())
		}
	}
	p.Rollback, err = m.reviewRollbackCapability(ctx, p, m.prepareArchives)
	if err != nil {
		return Plan{}, err
	}

	return publishReview(p, impact)
}

func currentDiscovery(p Plan) error {
	if p.Reviewed || p.ParentPlanID != "" || time.Now().Unix()-p.CreatedAt >= int64(FreshFor.Seconds()) {
		return ErrPlanChanged
	}
	items, err := Snapshots()
	if err != nil {
		return err
	}
	for _, s := range items {
		if s.Version == p.Version && s.UpdateAvailable && s.Plan != nil && s.Plan.ID == p.ID && digest(s.Plan.Inventory) == digest(p.Inventory) {
			return nil
		}
	}
	return ErrPlanChanged
}

// Recheck the parent inside the same transaction that publishes the immutable
// child: a newer or failed Check cannot grant an old review installation rights.
func publishReview(parent Plan, impact Impact) (Plan, error) {
	p := parent
	p.ID, p.ParentPlanID, p.Reviewed = uuid.NewString(), parent.ID, true
	p.CreatedAt, p.Impact = time.Now().Unix(), impact
	p.Impact.BlockedReasons = uniqueReasons(p.Impact.BlockedReasons)
	tx, err := database.DB.Begin()
	if err != nil {
		return Plan{}, err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRow("SELECT payload FROM php_update_snapshots WHERE version=?", p.Version).Scan(&raw); err != nil {
		return Plan{}, err
	}
	var s Snapshot
	if err = json.Unmarshal([]byte(raw), &s); err != nil {
		return Plan{}, err
	}
	if parent.Reviewed || parent.ParentPlanID != "" || s.State != "available" || s.Plan == nil || s.Plan.ID != parent.ID ||
		time.Now().Unix()-s.CheckedAt >= int64(FreshFor.Seconds()) || digest(parent.Inventory) != digest(s.Plan.Inventory) {
		return Plan{}, ErrPlanChanged
	}
	rawBytes, err := json.Marshal(p)
	if err != nil {
		return Plan{}, err
	}
	if _, err = tx.Exec("INSERT INTO php_update_plans(id,version,payload,created_at) VALUES (?,?,?,?)", p.ID, p.Version, string(rawBytes), p.CreatedAt); err != nil {
		return Plan{}, err
	}
	if err = tx.Commit(); err != nil {
		return Plan{}, err
	}
	return p, nil
}

func uniqueReasons(items []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, item := range items {
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

func addPreservedExtensionNotes(impact *Impact, inventory Inventory) {
	for _, pkg := range inventory.Packages {
		if pkg.PreservedExtension {
			impact.Notes = append(impact.Notes, "Preserving "+pkg.Name+" at its authenticated installed revision because the new repository does not offer a newer build. No extension downgrade is included.")
		}
	}
}

type archivePreparer func(context.Context, Operation, Plan, bool, ...bool) (archiveManifest, error)

func (m *Manager) reviewRollbackCapability(ctx context.Context, p Plan, prepare archivePreparer) (RollbackCapability, error) {
	capability := p.Rollback
	var err error
	// Revalidate a previous retention failure before requesting the exceptional
	// decision again. The evidence must come from an attempt that stopped before generation
	// creation. A fresh probe never changes its retained manifest.
	var failedID string
	err = database.DB.QueryRow(`SELECT r.operation_id FROM php_update_retention r JOIN php_update_operations o ON o.id=r.operation_id WHERE r.inventory_hash=? AND r.available=0 AND o.status NOT IN ('queued','running') AND NOT EXISTS(SELECT 1 FROM php_update_generations g WHERE g.id=o.id)`, digest(p.Inventory)).Scan(&failedID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return RollbackCapability{}, err
	}
	if failedID != "" {
		probeID := uuid.NewString()
		probeDir, e := m.archiveDirectory(probeID)
		if e != nil {
			return RollbackCapability{}, e
		}
		// The probe owns this fresh UUID directory; generation archives and the
		// original failed attempt remain intact. Repeated reviews cannot overwrite
		// a retained manifest or accumulate another package family indefinitely.
		defer os.RemoveAll(filepath.Dir(probeDir))
		archives, e := prepare(ctx, Operation{ID: probeID}, p, true, true)
		if e != nil {
			return RollbackCapability{}, e
		}
		capability = RollbackCapability{Available: archives.RollbackAvailable, Reason: archives.RollbackReason}
		if archives.RollbackAvailable {
			if _, e = database.DB.Exec("UPDATE php_update_retention SET available=1,reason='',checked_at=? WHERE inventory_hash=?", time.Now().Unix(), digest(p.Inventory)); e != nil {
				return RollbackCapability{}, e
			}
		}
	}

	return capability, nil
}
