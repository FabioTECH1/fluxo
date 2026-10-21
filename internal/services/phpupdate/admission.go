package phpupdate

import (
	"database/sql"
	"encoding/json"
	"time"
)

type rowQuery interface{ QueryRow(string, ...any) *sql.Row }

// The caller uses the same SQLite transaction for admission and inserting its
// durable operation claim. An intervening writer cannot commit a stale claim.
func admitApply(q rowQuery, branch, planID string) error {
	var pending int
	if err := q.QueryRow(`SELECT (SELECT COUNT(*) FROM php_update_generations WHERE state NOT IN ('succeeded','rolled_back')) + (SELECT COUNT(*) FROM php_update_barriers)`).Scan(&pending); err != nil {
		return err
	}
	if pending > 0 {
		return ErrRecoveryRequired
	}
	if err := q.QueryRow(`SELECT COUNT(*) FROM php_update_operations o WHERE version=? AND kind IN ('apply','verify') AND status NOT IN ('queued','running','succeeded') AND checkpoint IN ('package_transaction','packages_updated') AND NOT EXISTS (SELECT 1 FROM php_update_generations g WHERE g.id=o.id) AND NOT EXISTS (SELECT 1 FROM php_update_recovery_attempts a WHERE a.operation_id=o.id) AND NOT EXISTS (SELECT 1 FROM php_update_operations n WHERE n.version=o.version AND n.kind IN ('apply','verify') AND n.status='succeeded' AND n.rowid>o.rowid)`, branch).Scan(&pending); err != nil {
		return err
	}
	if pending > 0 {
		return ErrRecoveryRequired
	}
	var raw string
	if err := q.QueryRow("SELECT payload FROM php_update_snapshots WHERE version=?", branch).Scan(&raw); err != nil {
		return err
	}
	var s Snapshot
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return err
	}
	if s.State != "available" || s.Plan == nil || s.Plan.ID != planID || time.Now().Unix()-s.CheckedAt >= int64(FreshFor.Seconds()) {
		return ErrPlanChanged
	}
	return nil
}

func admitRecovery(q rowQuery, p RecoveryPlan) (recoveryGeneration, error) {
	var raw string
	err := q.QueryRow(`SELECT payload FROM php_update_generations g WHERE id=? AND head_operation_id=? AND NOT EXISTS (SELECT 1 FROM php_update_operations n WHERE n.kind='apply' AND n.version=g.version AND n.rowid>(SELECT rowid FROM php_update_operations WHERE id=g.id))`, p.GenerationID, p.Head).Scan(&raw)
	if err == sql.ErrNoRows {
		return recoveryGeneration{}, ErrPlanChanged
	}
	if err != nil {
		return recoveryGeneration{}, err
	}
	var g recoveryGeneration
	if err = json.Unmarshal([]byte(raw), &g); err != nil {
		return g, err
	}
	if g.Head != p.Head || g.ID != p.GenerationID || g.Version != p.Version {
		return g, ErrPlanChanged
	}
	if p.Kind != "repair" && p.Kind != "rollback" || p.Goal != "after" && p.Goal != "before" {
		return g, ErrUnsupported
	}
	if p.Goal == "before" && !g.Archives.RollbackAvailable {
		return g, ErrUnsupported
	}
	return g, nil
}
