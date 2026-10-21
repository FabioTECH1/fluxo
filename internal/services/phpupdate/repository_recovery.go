package phpupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fluxo/internal/database"
)

type repositoryRecoveryBinding struct {
	GenerationID string `json:"generation_id"`
	ManifestHash string `json:"manifest_hash"`
	PackagesHash string `json:"packages_hash"`
}

func recoveryPolicyAuthorized(g recoveryGeneration, policy string) bool {
	if policy == g.Archives.PolicyHash {
		return true
	}
	if database.DB == nil {
		return false
	}
	rows, err := database.DB.Query(`SELECT a.before_policy,a.after_policy FROM php_repository_recovery_authorizations a JOIN php_repository_migrations m ON m.plan_id=a.plan_id WHERE a.generation_id=? AND a.manifest_hash=? AND m.state='committed'`, g.ID, digest(g.Archives))
	if err != nil {
		return false
	}
	defer rows.Close()
	edges := map[string][]string{}
	for rows.Next() {
		var before, after string
		if rows.Scan(&before, &after) != nil {
			return false
		}
		edges[before] = append(edges[before], after)
	}
	if rows.Err() != nil {
		return false
	}
	queue := []string{g.Archives.PolicyHash}
	seen := map[string]bool{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == policy {
			return true
		}
		if seen[current] {
			continue
		}
		seen[current] = true
		queue = append(queue, edges[current]...)
	}
	return false
}
func (m *Manager) repositoryRecoveryBindings(ctx context.Context, id, before string) ([]repositoryRecoveryBinding, error) {
	rows, err := database.DB.Query(`SELECT g.payload FROM php_update_generations g WHERE NOT EXISTS (SELECT 1 FROM php_update_generations n WHERE n.version=g.version AND n.rowid>g.rowid)`)
	if err != nil {
		return nil, err
	}
	var generations []recoveryGeneration
	for rows.Next() {
		var raw string
		var g recoveryGeneration
		if err = rows.Scan(&raw); err == nil {
			err = json.Unmarshal([]byte(raw), &g)
		}
		if err != nil {
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
	result := []repositoryRecoveryBinding{}
	for _, g := range generations {
		ob, e := m.observePackages(ctx, id, g.Version)
		if e != nil {
			return nil, e
		}
		if ob.PolicyHash != before {
			return nil, ErrPlanChanged
		}
		if _, e = validateRecoveryObservation(g, ob, g.Goal); e != nil {
			continue
		} // An already superseded/unrecoverable generation receives no new rights.
		result = append(result, repositoryRecoveryBinding{g.ID, digest(g.Archives), digest(ob.Packages)})
	}
	return result, nil
}
func (m *Manager) authorizeRepositoryRecovery(ctx context.Context, id, planID, before, after string) error {
	var raw string
	if err := database.DB.QueryRow("SELECT bindings FROM php_repository_migrations WHERE plan_id=?", planID).Scan(&raw); err != nil {
		return err
	}
	var bindings []repositoryRecoveryBinding
	if err := json.Unmarshal([]byte(raw), &bindings); err != nil {
		return err
	}
	for _, b := range bindings {
		g, err := getGeneration(b.GenerationID)
		if err != nil {
			return err
		}
		if digest(g.Archives) != b.ManifestHash {
			return errors.New("Retained recovery manifest changed during repository migration")
		}
		ob, err := m.observePackages(ctx, id, g.Version)
		if err != nil {
			return err
		}
		if ob.PolicyHash != after || ob.JournalDirty || len(ob.UnrelatedPending) > 0 || digest(ob.Packages) != b.PackagesHash {
			return errors.New("Package state changed during repository migration; reconcile it before resuming")
		}
		_, err = database.DB.Exec(`INSERT OR IGNORE INTO php_repository_recovery_authorizations(plan_id,generation_id,manifest_hash,before_policy,after_policy) VALUES (?,?,?,?,?)`, planID, g.ID, b.ManifestHash, before, after)
		if err != nil {
			return err
		}
	}
	return nil
}
