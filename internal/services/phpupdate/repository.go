package phpupdate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/phprepository"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

type repositoryFile struct {
	Path       string  `json:"path"`
	Before     *string `json:"before"`
	After      string  `json:"after"`
	BeforeMode *uint32 `json:"before_mode"`
	BeforeUID  *uint32 `json:"before_uid"`
	BeforeGID  *uint32 `json:"before_gid"`
}
type repositoryConfiguration struct {
	ConfigurationID     string              `json:"configuration_id"`
	Codename            string              `json:"codename"`
	Architecture        string              `json:"architecture"`
	PolicyHash          string              `json:"policy_hash"`
	AfterPolicy         string              `json:"after_policy"`
	Changes             []repositoryFile    `json:"changes"`
	PPAEntries          int                 `json:"ppa_entries"`
	SuryEntries         int                 `json:"sury_entries"`
	KeyFingerprint      string              `json:"key_fingerprint"`
	KeyHash             string              `json:"key_hash"`
	RollbackUnavailable []string            `json:"rollback_unavailable"`
	PackageBlockers     map[string][]string `json:"package_blockers"`
	RetainedExtensions  []string            `json:"retained_extensions"`
	PackageInventory    json.RawMessage     `json:"package_inventory"`
	MigrationBlockers   []string            `json:"migration_blockers"`
}
type repositoryPlan struct {
	ID            string                  `json:"id"`
	CreatedAt     int64                   `json:"created_at"`
	Configuration repositoryConfiguration `json:"configuration"`
}

// Only this public summary leaves the server; source contents and credentials
// stay in the private plan/journal, never in operation results or HTTP responses.
type RepositoryReview struct {
	ID                  string              `json:"id"`
	CreatedAt           int64               `json:"created_at"`
	Codename            string              `json:"codename"`
	Architecture        string              `json:"architecture"`
	Files               []string            `json:"files"`
	DisabledPPAEntries  int                 `json:"disabled_ppa_entries"`
	KeyFingerprint      string              `json:"key_fingerprint"`
	RollbackUnavailable []string            `json:"rollback_unavailable"`
	PackageBlockers     map[string][]string `json:"package_blockers"`
	RetainedExtensions  []string            `json:"retained_extensions"`
	MigrationBlockers   []string            `json:"migration_blockers"`
}
type RepositoryStatus struct {
	State        string            `json:"state"`
	Message      string            `json:"message"`
	CanReview    bool              `json:"can_review"`
	ResumePlanID string            `json:"resume_plan_id,omitempty"`
	Review       *RepositoryReview `json:"review,omitempty"`
}

func publicRepositoryPlan(p repositoryPlan) RepositoryReview {
	files := []string{}
	for _, f := range p.Configuration.Changes {
		files = append(files, f.Path)
	}
	return RepositoryReview{p.ID, p.CreatedAt, p.Configuration.Codename, p.Configuration.Architecture, files, p.Configuration.PPAEntries, p.Configuration.KeyFingerprint, p.Configuration.RollbackUnavailable, p.Configuration.PackageBlockers, p.Configuration.RetainedExtensions, p.Configuration.MigrationBlockers}
}
func (m *Manager) repositoryDirectory() string { return filepath.Join(m.DataDir, "php-repository") }
func repositoryBridge(ctx context.Context, id string, request map[string]any) (json.RawMessage, error) {
	request["key"] = base64.StdEncoding.EncodeToString(phprepository.Key)
	raw, _ := json.Marshal(request)
	w := &bridgeWriter{id: id}
	err := syscmd.RunMaintenance(ctx, string(raw), w, []string{"DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=l"}, "/usr/bin/python3", "-I", "-c", phprepository.Script+"\n"+aptBridge)
	if err != nil {
		if w.err != "" {
			return nil, errors.New(w.err)
		}
		return nil, fmt.Errorf("PHP repository setup command failed: %w", err)
	}
	if w.result == nil {
		return nil, errors.New("Repository helper returned no complete result")
	}
	var result struct {
		Repository json.RawMessage `json:"repository"`
	}
	err = json.Unmarshal(w.result, &result)
	return result.Repository, err
}
func repositoryPending() (string, error) {
	var id string
	err := database.DB.QueryRow("SELECT plan_id FROM php_repository_migrations WHERE state='pending' LIMIT 1").Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}
func (m *Manager) RepositoryStatus(ctx context.Context) RepositoryStatus {
	if id, err := repositoryPending(); err != nil {
		return RepositoryStatus{State: "blocked", Message: err.Error()}
	} else if id != "" {
		p, e := loadRepositoryPlan(id)
		if e != nil {
			return RepositoryStatus{State: "blocked", Message: e.Error()}
		}
		review := publicRepositoryPlan(p)
		return RepositoryStatus{State: "interrupted", Message: "PHP repository setup needs to be resumed. Package updates remain blocked until configuration is reconciled.", ResumePlanID: id, Review: &review}
	}
	raw, err := repositoryBridge(ctx, "", map[string]any{"action": "repository_status"})
	if err != nil {
		return RepositoryStatus{State: "blocked", Message: err.Error()}
	}
	var c repositoryConfiguration
	if err = json.Unmarshal(raw, &c); err != nil {
		return RepositoryStatus{State: "blocked", Message: err.Error()}
	}
	if c.PPAEntries == 0 && c.SuryEntries == 1 && len(c.Changes) == 0 {
		var auth struct {
			PolicyHash string `json:"policy_hash"`
			KeySHA256  string `json:"key_sha256"`
		}
		b, e := os.ReadFile(filepath.Join(m.repositoryDirectory(), "authorization.json"))
		if e == nil && json.Unmarshal(b, &auth) == nil && repositoryConfigurationCurrent(c, auth.PolicyHash, auth.KeySHA256) {
			return RepositoryStatus{State: "current", Message: "Using Ondřej Surý’s new PHP repository."}
		}
	}
	return RepositoryStatus{State: "available", Message: "PHP repository setup is incomplete. Review the repair to continue receiving PHP updates.", CanReview: true}
}

func repositoryConfigurationCurrent(c repositoryConfiguration, policy, keyHash string) bool {
	pinned := fmt.Sprintf("%x", sha256.Sum256(phprepository.Key))
	return c.PPAEntries == 0 && c.SuryEntries == 1 && len(c.Changes) == 0 && c.KeyFingerprint == phprepository.Fingerprint && c.KeyHash == pinned && keyHash == pinned && policy == c.PolicyHash
}
func loadRepositoryPlan(id string) (repositoryPlan, error) {
	var p repositoryPlan
	var raw string
	err := database.DB.QueryRow("SELECT payload FROM php_repository_plans WHERE id=?", id).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &p)
	}
	return p, err
}
func repositoryAdmission() error {
	var pending int
	err := database.DB.QueryRow("SELECT (SELECT COUNT(*) FROM php_update_generations WHERE state NOT IN ('succeeded','rolled_back')) + (SELECT COUNT(*) FROM php_update_barriers)").Scan(&pending)
	if err != nil {
		return err
	}
	if pending > 0 {
		return ErrRecoveryRequired
	}
	return nil
}
func (m *Manager) EnqueueRepository(ctx context.Context, kind, planID, requestID, username, ip string) (Operation, error) {
	if kind != "repository_review" && kind != "repository_apply" {
		return Operation{}, ErrUnsupported
	}
	if old, err := scanOperation(database.DB.QueryRow("SELECT "+operationColumns+" FROM php_update_operations WHERE request_id=?", requestID)); err == nil {
		if old.Kind != kind || old.PlanID != planID {
			return Operation{}, ErrPlanChanged
		}
		return old, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Operation{}, err
	}
	if err := repositoryAdmission(); err != nil {
		return Operation{}, err
	}
	pending, err := repositoryPending()
	if err != nil {
		return Operation{}, err
	}
	if pending != "" && (kind != "repository_apply" || planID != pending) {
		return Operation{}, ErrRecoveryRequired
	}
	if kind == "repository_apply" {
		var state string
		if e := database.DB.QueryRow("SELECT state FROM php_repository_migrations WHERE plan_id=?", planID).Scan(&state); e == nil && state != "pending" {
			return Operation{}, ErrReviewChanged
		} else if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return Operation{}, e
		}
		p, e := loadRepositoryPlan(planID)
		if e != nil {
			return Operation{}, e
		}
		if pending == "" && len(p.Configuration.MigrationBlockers) > 0 {
			return Operation{}, ErrUnsupported
		}
		if pending == "" && time.Now().Unix()-p.CreatedAt >= int64(ReviewFreshFor.Seconds()) {
			return Operation{}, ErrReviewChanged
		}
	}
	o := Operation{ID: uuid.NewString(), Kind: kind, PlanID: planID, Status: "queued", Phase: "queued", Username: username, IPAddress: ip, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(), Result: json.RawMessage("{}")}
	_, err = database.DB.Exec("INSERT INTO php_update_operations(id,kind,plan_id,request_id,username,ip_address,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?)", o.ID, kind, planID, requestID, username, ip, o.CreatedAt, o.UpdatedAt)
	if err != nil {
		if active, e := ActiveOperation(); e == nil && active != nil {
			return Operation{}, ErrBusy
		}
		return Operation{}, err
	}
	launchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err = m.Launch(launchCtx, o); err != nil {
		if errors.Is(err, ErrSupervisor) {
			_ = finish(o.ID, "failed", err.Error())
			return Operation{}, err
		}
	}
	return o, nil
}
func (m *Manager) runRepository(ctx context.Context, o Operation) error {
	if err := repositoryAdmission(); err != nil {
		return err
	}
	directory := m.repositoryDirectory()
	if err := ensureDurableRecoveryDirectory(directory); err != nil {
		return err
	}
	if o.Kind == "repository_review" {
		if id, err := repositoryPending(); err != nil {
			return err
		} else if id != "" {
			return ErrRecoveryRequired
		}
		_ = progress(o.ID, "reviewing_repository", "Verifying the proposed PHP repository and preserving available rollback packages")
		raw, err := repositoryBridge(ctx, o.ID, map[string]any{"action": "repository_review", "directory": directory})
		if err != nil {
			return err
		}
		p := repositoryPlan{ID: uuid.NewString(), CreatedAt: time.Now().Unix()}
		if err = json.Unmarshal(raw, &p.Configuration); err != nil {
			return err
		}
		payload, _ := json.Marshal(p)
		if _, err = database.DB.Exec("INSERT INTO php_repository_plans(id,payload,created_at) VALUES (?,?,?)", p.ID, string(payload), p.CreatedAt); err != nil {
			return err
		}
		result, _ := json.Marshal(publicRepositoryPlan(p))
		_, err = database.DB.Exec("UPDATE php_update_operations SET result=? WHERE id=?", string(result), o.ID)
		if err != nil {
			return err
		}
		return finish(o.ID, "succeeded", "PHP repository review is ready. No active sources, packages or services were changed.")
	}
	p, err := loadRepositoryPlan(o.PlanID)
	if err != nil {
		return err
	}
	pending, err := repositoryPending()
	if err != nil {
		return err
	}
	if pending == "" && time.Now().Unix()-p.CreatedAt >= int64(ReviewFreshFor.Seconds()) {
		return ErrReviewChanged
	}
	if pending != "" && pending != p.ID {
		return ErrRecoveryRequired
	}
	// The durable intent survives a lost supervisor or partial source publication.
	if pending == "" {
		bindings, e := m.repositoryRecoveryBindings(ctx, o.ID, p.Configuration.PolicyHash)
		if e != nil {
			return e
		}
		rawBindings, _ := json.Marshal(bindings)
		_, err = database.DB.Exec("INSERT OR IGNORE INTO php_repository_migrations(plan_id,state,before_policy,created_at,bindings) VALUES (?,'pending',?,?,?)", p.ID, p.Configuration.PolicyHash, time.Now().Unix(), string(rawBindings))
	}
	if err != nil {
		return err
	}
	var intentState string
	if err = database.DB.QueryRow("SELECT state FROM php_repository_migrations WHERE plan_id=?", p.ID).Scan(&intentState); err != nil {
		return err
	}
	if intentState != "pending" {
		return ErrReviewChanged
	}
	_ = progress(o.ID, "switching_repository", "Switching PHP package sources; no PHP packages are being installed")
	raw, err := repositoryBridge(ctx, o.ID, map[string]any{"action": "repository_apply", "directory": directory, "plan_id": p.ID, "plan": p.Configuration})
	if err != nil {
		journalPath := filepath.Join(directory, "journal.json")
		journalBytes, journalErr := os.ReadFile(journalPath)
		var journal struct {
			PlanID string `json:"plan_id"`
			State  string `json:"state"`
		}
		safeToRetire := os.IsNotExist(journalErr)
		if journalErr == nil && json.Unmarshal(journalBytes, &journal) == nil {
			safeToRetire = m.repositoryIntentUnpublished(p.ID, journal.PlanID, journal.State)
		}
		if safeToRetire {
			_, _ = database.DB.Exec("UPDATE php_repository_migrations SET state='failed' WHERE plan_id=? AND state='pending'", p.ID)
		}
		return fmt.Errorf("%w. Retry repository setup to reconcile its saved configuration; no PHP packages were installed", err)
	}
	var result struct {
		State        string `json:"state"`
		BeforePolicy string `json:"before_policy"`
		AfterPolicy  string `json:"after_policy"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return err
	}
	if result.State != "committed" || result.BeforePolicy != p.Configuration.PolicyHash || len(result.AfterPolicy) != 64 {
		return errors.New("Repository migration returned an incomplete commitment")
	}
	if err = m.authorizeRepositoryRecovery(ctx, o.ID, p.ID, result.BeforePolicy, result.AfterPolicy); err != nil {
		return err
	}
	tx, err := database.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE php_repository_migrations SET state='committed',after_policy=? WHERE plan_id=?", result.AfterPolicy, p.ID); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM php_update_snapshots"); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE php_update_schedule SET next_check=? WHERE id=1", time.Now().Add(FreshFor).Unix()); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return finish(o.ID, "succeeded", "PHP repository switched successfully. Check for PHP updates next; no packages or application services were changed.")
}

// A different terminal journal predates this intent: the helper never published
// this plan. Require its matching terminal DB record as well; ambiguous or
// pending journals keep the maintenance gate closed.
func (m *Manager) repositoryIntentUnpublished(planID, journalID, state string) bool {
	if journalID == planID {
		return state == "restored"
	}
	var recorded string
	if journalID == "" || database.DB.QueryRow("SELECT state FROM php_repository_migrations WHERE plan_id=?", journalID).Scan(&recorded) != nil {
		return false
	}
	return state == "committed" && recorded == "committed" || state == "restored" && recorded == "failed"
}

// BootstrapRepository is deliberately restricted to a fresh installation. An
// existing panel uses reviewed supervised operations and its own audit database.
func BootstrapRepository(ctx context.Context, directory string) error {
	if os.Geteuid() != 0 {
		return ErrSupervisor
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(directory), "fluxo.db")); err == nil {
		return errors.New("Existing installations must review repository migration from Runtime > PHP")
	}
	release, err := packages.Lifecycle(ctx, false)
	if err != nil {
		return err
	}
	defer release()
	unlock, err := packages.Acquire(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if err = ensureDurableRecoveryDirectory(directory); err != nil {
		return err
	}
	raw, err := repositoryBridge(ctx, "", map[string]any{"action": "repository_review", "directory": directory})
	if err != nil {
		return err
	}
	var c repositoryConfiguration
	if err = json.Unmarshal(raw, &c); err != nil {
		return err
	}
	if c.PPAEntries > 0 {
		return errors.New("An existing PHP PPA requires an explicit reviewed migration; active sources were not changed")
	}
	_, err = repositoryBridge(ctx, "", map[string]any{"action": "repository_apply", "directory": directory, "plan_id": uuid.NewString(), "plan": c})
	return err
}
