package phpupdate

import (
	"context"
	"encoding/json"
	"errors"
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
