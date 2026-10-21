package phpupdate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/packages"
	"github.com/google/uuid"
)

type observedPackage struct {
	Name         string `json:"name"`
	Architecture string `json:"architecture"`
	Installed    string `json:"installed"`
	State        int    `json:"state"`
	Automatic    bool   `json:"automatic"`
	Held         bool   `json:"held"`
}
type packageObservation struct {
	Version          string            `json:"version"`
	Packages         []observedPackage `json:"packages"`
	UnrelatedPending []string          `json:"unrelated_pending"`
	JournalDirty     bool              `json:"journal_dirty"`
	PolicyHash       string            `json:"policy_hash"`
}
type RecoverySummary struct {
	GenerationID   string `json:"generation_id"`
	OperationID    string `json:"operation_id"`
	Version        string `json:"version"`
	State          string `json:"state"`
	CanRepair      bool   `json:"can_repair"`
	CanRollback    bool   `json:"can_rollback"`
	RollbackReason string `json:"rollback_reason,omitempty"`
}
type RecoveryPlan struct {
	ID             string             `json:"id"`
	GenerationID   string             `json:"generation_id"`
	Head           string             `json:"operation_id"`
	Version        string             `json:"version"`
	Kind           string             `json:"kind"`
	Goal           string             `json:"goal"`
	CreatedAt      int64              `json:"created_at"`
	Observation    packageObservation `json:"observation"`
	Packages       []PackageArchive   `json:"packages"`
	Impact         Impact             `json:"impact"`
	RetryFPMReload bool               `json:"retry_fpm_reload"`
}

func (m *Manager) observePackages(ctx context.Context, id, branch string) (packageObservation, error) {
	var result struct {
		Observation packageObservation `json:"observation"`
	}
	raw, err := bridgeRaw(ctx, id, map[string]any{"action": "observe", "version": branch})
	if err != nil {
		return result.Observation, err
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return result.Observation, err
	}
	if result.Observation.Version != branch || len(result.Observation.Packages) == 0 {
		return result.Observation, errors.New("Package observation was incomplete")
	}
	return result.Observation, nil
}

func validateRecoveryObservation(g recoveryGeneration, ob packageObservation, goal string) (bool, error) {
	if ob.JournalDirty || len(ob.UnrelatedPending) > 0 {
		return false, errors.New("The package journal or unrelated packages require operator repair before this scoped recovery")
	}
	if ob.PolicyHash != g.Archives.PolicyHash || ob.Version != g.Version {
		return false, errors.New("Repository policy changed since this update; operator review is required")
	}
	targets := g.Archives.After
	if goal == "before" {
		if !g.Archives.RollbackAvailable {
			return false, errors.New("Verified previous PHP packages were not retained")
		}
		targets = g.Archives.Before
	}
	if goal != "before" && goal != "after" || len(targets) == 0 || len(targets) != len(g.Archives.Packages) || len(ob.Packages) != len(targets) {
		return false, errors.New("The recorded PHP package family changed or is incomplete")
	}
	matched := true
	for _, p := range ob.Packages {
		var original *Package
		var target *PackageArchive
		for i := range g.Archives.Packages {
			q := &g.Archives.Packages[i]
			if p.Name == q.Name && p.Architecture == q.Architecture {
				original = q
			}
		}
		for i := range targets {
			q := &targets[i]
			if p.Name == q.Name && p.Architecture == q.Architecture {
				target = q
			}
		}
		if original == nil || target == nil || p.Installed != original.Installed && p.Installed != original.Candidate || p.Automatic != original.Automatic || p.Held != original.Held {
			return false, errors.New("Package versions or installation marks changed outside the recorded update")
		}
		// dpkg current-state 6 is fully installed. Other branch states may be
		// repairable by the exact retained set; unrelated state is never repaired.
		if p.State != 6 || p.Installed != target.Version {
			matched = false
		}
		if p.Held && (p.State != 6 || p.Installed != target.Version) {
			return false, errors.New("A held package prevents this recovery")
		}
	}
	return matched, nil
}

func currentGeneration(id string) (recoveryGeneration, error) {
	g, err := generationForOperation(id)
	if err != nil {
		return g, err
	}
	if g.Head != id {
		return g, ErrPlanChanged
	}
	var later int
	err = database.DB.QueryRow(`SELECT COUNT(*) FROM php_update_operations WHERE kind='apply' AND version=? AND rowid>(SELECT rowid FROM php_update_operations WHERE id=?)`, g.Version, g.ID).Scan(&later)
	if err != nil {
		return g, err
	}
	if later > 0 {
		return g, ErrPlanChanged
	}
	return g, nil
}

func Recoveries() ([]RecoverySummary, error) {
	rows, err := database.DB.Query(`SELECT g.payload FROM php_update_generations g WHERE NOT EXISTS (SELECT 1 FROM php_update_operations n WHERE n.kind='apply' AND n.version=g.version AND n.rowid>(SELECT rowid FROM php_update_operations WHERE id=g.id)) ORDER BY g.created_at DESC`)
	if err != nil {
		return nil, err
	}
	var generations []recoveryGeneration
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		var g recoveryGeneration
		if err = json.Unmarshal([]byte(raw), &g); err != nil {
			rows.Close()
			return nil, err
		}
		generations = append(generations, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	items := []RecoverySummary{}
	for _, g := range generations {
		o, e := GetOperation(g.Head)
		if e != nil {
			return nil, e
		}
		items = append(items, RecoverySummary{GenerationID: g.ID, OperationID: g.Head, Version: g.Version, State: generationState(g), CanRepair: !o.Active() && (o.Status != "succeeded" || g.State != "succeeded" && g.State != "rolled_back"), CanRollback: !o.Active() && g.Goal != "before" && g.Archives.RollbackAvailable, RollbackReason: g.Archives.RollbackReason})
	}
	return items, nil
}

// Review is read-only with respect to packages and services. The resulting
// immutable plan binds confirmation to this generation/head and package state.
func (m *Manager) ReviewRecovery(ctx context.Context, id, kind string) (RecoveryPlan, error) {
	var review RecoveryPlan
	if kind != "repair" && kind != "rollback" {
		return review, ErrUnsupported
	}
	if active, err := ActiveOperation(); err != nil {
		return review, err
	} else if active != nil {
		return review, ErrBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	unlock, err := packages.Lifecycle(ctx, false)
	if err != nil {
		return review, err
	}
	defer unlock()
	g, err := currentGeneration(id)
	if err != nil {
		return review, err
	}
	o, err := GetOperation(id)
	if err != nil {
		return review, err
	}
	if o.Active() {
		return review, ErrBusy
	}
	if kind == "repair" && o.Status == "succeeded" && (g.State == "succeeded" || g.State == "rolled_back") {
		return review, ErrUnsupported
	}
	goal := g.Goal
	if kind == "rollback" {
		if goal == "before" {
			return review, ErrUnsupported
		}
		goal = "before"
	}
	ob, err := m.observePackages(ctx, "", g.Version)
	if err != nil {
		return review, err
	}
	matched, err := validateRecoveryObservation(g, ob, goal)
	if err != nil {
		return review, err
	}
	if !matched {
		if err = m.verifyRetainedArchives(ctx, "", g, goal); err != nil {
			return review, err
		}
	}
	p, err := GetPlan(g.PlanID)
	if err != nil {
		return review, err
	}
	impact, err := inspectImpact(ctx, g.Version)
	if err != nil {
		return review, err
	}
	if err = recoveryCompatible(p.Impact, impact); err != nil {
		return review, err
	}
	targets := g.Archives.After
	if goal == "before" {
		targets = g.Archives.Before
	}
	review = RecoveryPlan{ID: uuid.NewString(), GenerationID: g.ID, Head: g.Head, Version: g.Version, Kind: kind, Goal: goal, CreatedAt: time.Now().Unix(), Observation: ob, Packages: targets, Impact: impact}
	if kind == "repair" {
		review.RetryFPMReload, err = m.fpmRetryNeeded(ctx, g)
		if err != nil {
			return review, err
		}
	}
	raw, err := json.Marshal(review)
	if err != nil {
		return review, err
	}
	_, err = database.DB.Exec("INSERT INTO php_update_recovery_plans(id,generation_id,payload,created_at) VALUES (?,?,?,?)", review.ID, g.ID, string(raw), review.CreatedAt)
	return review, err
}

func recoveryPlan(id string) (RecoveryPlan, error) {
	var p RecoveryPlan
	var raw string
	err := database.DB.QueryRow("SELECT payload FROM php_update_recovery_plans WHERE id=?", id).Scan(&raw)
	if err != nil {
		return p, err
	}
	err = json.Unmarshal([]byte(raw), &p)
	return p, err
}

func (m *Manager) EnqueueRecovery(ctx context.Context, planID, requestID, username, ip string) (Operation, error) {
	p, err := recoveryPlan(planID)
	if err != nil {
		return Operation{}, err
	}
	if _, err = uuid.Parse(requestID); err != nil {
		return Operation{}, ErrUnsupported
	}
	if old, err := scanOperation(database.DB.QueryRow("SELECT "+operationColumns+" FROM php_update_operations WHERE request_id=?", requestID)); err == nil {
		var stored string
		if err = database.DB.QueryRow("SELECT recovery_plan_id FROM php_update_recovery_attempts WHERE operation_id=?", old.ID).Scan(&stored); err != nil || stored != planID {
			return Operation{}, ErrPlanChanged
		}
		return old, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Operation{}, err
	}
	if time.Now().Unix()-p.CreatedAt > 15*60 {
		return Operation{}, ErrPlanChanged
	}
	g, err := currentGeneration(p.Head)
	if err != nil {
		return Operation{}, err
	}
	if g.ID != p.GenerationID {
		return Operation{}, ErrPlanChanged
	}
	o := Operation{ID: uuid.NewString(), Kind: p.Kind, Version: g.Version, PlanID: g.PlanID, Status: "queued", Phase: "queued", Username: username, IPAddress: ip, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
	tx, err := database.DB.BeginTx(ctx, nil)
	if err != nil {
		return Operation{}, err
	}
	defer tx.Rollback()
	g, err = admitRecovery(tx, p)
	if err != nil {
		return Operation{}, err
	}
	_, err = tx.Exec(`INSERT INTO php_update_operations(id,kind,version,plan_id,request_id,username,ip_address,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?)`, o.ID, o.Kind, o.Version, o.PlanID, requestID, username, ip, o.CreatedAt, o.UpdatedAt)
	if err != nil {
		_ = tx.Rollback()
		if a, e := ActiveOperation(); e == nil && a != nil {
			return Operation{}, ErrBusy
		}
		return Operation{}, err
	}
	g.Head = o.ID
	g.Goal = p.Goal
	g.Pending = true
	raw, _ := json.Marshal(g)
	result, err := tx.Exec("UPDATE php_update_generations SET head_operation_id=?,state=?,payload=? WHERE id=? AND head_operation_id=?", o.ID, generationState(g), string(raw), g.ID, p.Head)
	if err != nil {
		return Operation{}, err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return Operation{}, ErrPlanChanged
	}
	_, err = tx.Exec("INSERT INTO php_update_recovery_attempts(operation_id,generation_id,parent_id,recovery_plan_id) VALUES (?,?,?,?)", o.ID, g.ID, p.Head, planID)
	if err != nil {
		return Operation{}, err
	}
	if err = tx.Commit(); err != nil {
		return Operation{}, err
	}
	launchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err = m.Launch(launchCtx, o); errors.Is(err, ErrSupervisor) {
		_ = finish(o.ID, "failed", err.Error())
		return Operation{}, err
	}
	return o, nil
}

func (m *Manager) recover(ctx context.Context, o Operation) error {
	g, err := currentGeneration(o.ID)
	if err != nil {
		return err
	}
	if g.Head != o.ID {
		return ErrPlanChanged
	}
	var reviewID string
	if err = database.DB.QueryRow("SELECT recovery_plan_id FROM php_update_recovery_attempts WHERE operation_id=?", o.ID).Scan(&reviewID); err != nil {
		return err
	}
	review, err := recoveryPlan(reviewID)
	if err != nil {
		return err
	}
	ob, err := m.observePackages(ctx, o.ID, g.Version)
	if err != nil {
		return err
	}
	if digest(ob) != digest(review.Observation) {
		return errors.New("Package state changed after recovery review; review it again before proceeding")
	}
	matched, err := validateRecoveryObservation(g, ob, g.Goal)
	if err != nil {
		return err
	}
	if !matched {
		if err = m.verifyRetainedArchives(ctx, o.ID, g, g.Goal); err != nil {
			return err
		}
	}
	if g.State == "releasing" {
		if g.VerifiedGoal != "before" && g.VerifiedGoal != "after" {
			return errors.New("The interrupted release has no verified package goal")
		}
		verified, e := validateRecoveryObservation(g, ob, g.VerifiedGoal)
		if e != nil {
			return e
		}
		if !verified {
			return errors.New("Previously verified packages changed before maintenance cleanup")
		}
		if err = m.releaseBarriers(ctx, g); err != nil {
			return err
		}
		g.State = "succeeded"
		if g.VerifiedGoal == "before" {
			g.State = "rolled_back"
		}
		if err = saveGeneration(g); err != nil {
			return err
		}
	}
	p, err := GetPlan(g.PlanID)
	if err != nil {
		return err
	}
	impact, err := inspectImpact(ctx, g.Version)
	if err != nil {
		return err
	}
	if err = recoveryCompatible(p.Impact, impact); err != nil {
		return err
	}
	if !matched || g.State == "prepared" || g.State == "draining" || g.State == "drained" {
		if g.State != "draining" && g.State != "drained" {
			g.Attempt, err = captureRecoveryRuntime(ctx, g)
			if err != nil {
				return err
			}
			g.Attempt.Signalled = map[string]bool{}
			if err = saveGeneration(g); err != nil {
				return err
			}
		}
		if err = m.drainConsumers(ctx, &g); err != nil {
			return err
		}
		if !matched {
			if err = m.packageTransaction(ctx, o, &g); err != nil {
				return err
			}
		}
	}
	if review.RetryFPMReload {
		g.Attempt.ReloadApprovedHead = o.ID
		if err = saveGeneration(g); err != nil {
			return err
		}
	}
	return m.activateGeneration(ctx, o, p, &g)
}

func captureRecoveryRuntime(ctx context.Context, g recoveryGeneration) (runtimeSnapshot, error) {
	r := g.Original
	r.Units = nil
	r.Signalled = nil
	for _, original := range g.Original.Units {
		u, err := observeUnit(ctx, original.Name, original.CGroup)
		if err != nil {
			return r, err
		}
		if !original.Active && (u.Active || len(u.PIDs) > 0) {
			return r, fmt.Errorf("%s was started outside the recorded maintenance operation", original.Name)
		}
		// Preserve escaped descendants as well as every process still in the group.
		for pid, key := range original.PIDs {
			if now, e := processKey(pid); e == nil && now == key {
				u.PIDs[pid] = key
			}
		}
		if u.Active {
			p, e := GetPlan(g.PlanID)
			if e != nil {
				return r, e
			}
			for _, c := range p.Impact.Workers {
				if c.Kind == "laravel_octane" && unitName(c.ID, 1) == u.Name {
					u.Octane, e = inspectOctaneProcesses(ctx, g.Version, c, u)
					if e != nil {
						return r, e
					}
				}
			}
		}
		r.Units = append(r.Units, u)
	}
	return r, nil
}
