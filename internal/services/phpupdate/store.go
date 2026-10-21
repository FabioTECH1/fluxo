package phpupdate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

var ErrBusy = errors.New("another PHP maintenance operation is in progress")
var ErrPlanChanged = errors.New("PHP packages or affected applications changed; check for updates again")
var ErrUnsupported = errors.New("this PHP configuration requires manual maintenance")
var ErrSupervisor = errors.New("PHP maintenance requires an installed root-owned Fluxo binary and systemd")
var ErrRecoveryRequired = errors.New("Resolve the previous PHP package/runtime update using its latest recovery report before starting another update")

const operationColumns = `id,kind,version,plan_id,status,phase,message,output,username,ip_address,created_at,updated_at,started_at,finished_at,checkpoint,result`

func scanOperation(row interface{ Scan(...any) error }) (Operation, error) {
	var o Operation
	var result string
	err := row.Scan(&o.ID, &o.Kind, &o.Version, &o.PlanID, &o.Status, &o.Phase, &o.Message, &o.Output, &o.Username, &o.IPAddress, &o.CreatedAt, &o.UpdatedAt, &o.StartedAt, &o.FinishedAt, &o.Checkpoint, &result)
	o.Result = json.RawMessage(result)
	return o, err
}
func GetOperation(id string) (Operation, error) {
	return scanOperation(database.DB.QueryRow("SELECT "+operationColumns+" FROM php_update_operations WHERE id=?", id))
}
func ActiveOperation() (*Operation, error) {
	o, err := scanOperation(database.DB.QueryRow("SELECT " + operationColumns + " FROM php_update_operations WHERE status IN ('queued','running') LIMIT 1"))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &o, err
}
func GetPlan(id string) (Plan, error) {
	var p Plan
	var raw string
	if err := database.DB.QueryRow("SELECT payload FROM php_update_plans WHERE id=?", id).Scan(&raw); err != nil {
		return p, err
	}
	err := json.Unmarshal([]byte(raw), &p)
	return p, err
}
func Snapshots() ([]Snapshot, error) {
	rows, err := database.DB.Query("SELECT payload,dismissed_candidate FROM php_update_snapshots ORDER BY version DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Snapshot{}
	for rows.Next() {
		var raw, dismissed string
		if err := rows.Scan(&raw, &dismissed); err != nil {
			return nil, err
		}
		var s Snapshot
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return nil, err
		}
		s.UpdateAvailable = s.State == "available" && s.Plan != nil && time.Now().Unix()-s.CheckedAt < int64(FreshFor.Seconds())
		if s.State == "available" && !s.UpdateAvailable {
			s.State = "stale"
		}
		s.Notice = s.Notice && s.Plan != nil && len(s.Plan.Inventory.Changes) > 0 && dismissed != s.Plan.CandidateID
		items = append(items, s)
	}
	return items, rows.Err()
}
func saveSnapshot(s Snapshot) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = database.DB.Exec(`INSERT INTO php_update_snapshots(version,payload) VALUES (?,?) ON CONFLICT(version) DO UPDATE SET payload=excluded.payload`, s.Version, string(raw))
	return err
}
func Dismiss(version, candidate string) error {
	items, err := Snapshots()
	if err != nil {
		return err
	}
	for _, s := range items {
		if s.Version == version && s.Plan != nil && s.Plan.CandidateID == candidate {
			for _, p := range s.Plan.Inventory.Changes {
				newer, err := candidateNewer(version, p)
				if err != nil {
					return err
				}
				if newer {
					if _, err = database.DB.Exec(`INSERT INTO php_update_notice_revisions(version,package,architecture,revision) VALUES (?,?,?,?) ON CONFLICT(version,package,architecture) DO UPDATE SET revision=excluded.revision`, version, p.Name, p.Architecture, p.Candidate); err != nil {
						return err
					}
				}
			}
			_, err = database.DB.Exec("UPDATE php_update_snapshots SET dismissed_candidate=? WHERE version=?", candidate, version)
			return err
		}
	}
	return ErrPlanChanged
}

type candidateRevision struct{ Name, Architecture, Revision string }

func candidateIdentity(changes []Package) string {
	items := []candidateRevision{}
	for _, p := range changes {
		items = append(items, candidateRevision{p.Name, p.Architecture, p.Candidate})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name+":"+items[i].Architecture < items[j].Name+":"+items[j].Architecture
	})
	return digest(items)
}
func candidateNewer(branch string, p Package) (bool, error) {
	var previous string
	err := database.DB.QueryRow(`SELECT revision FROM php_update_notice_revisions WHERE version=? AND package=? AND architecture=?`, branch, p.Name, p.Architecture).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	// dpkg implements Debian epoch/revision/backport ordering. This runs only
	// during checks/dismissals, never while serving cached page state.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err = syscmd.Run(ctx, 5*time.Second, "/usr/bin/dpkg", "--compare-versions", p.Candidate, "gt", previous); err == nil {
		return true, nil
	}
	if _, e := syscmd.Run(ctx, 5*time.Second, "/usr/bin/dpkg", "--compare-versions", p.Candidate, "le", previous); e == nil {
		return false, nil
	}
	return false, fmt.Errorf("Could not compare package notice revisions: %w", err)
}
func shouldNotify(inv Inventory) (bool, error) {
	for _, p := range inv.Changes {
		newer, err := candidateNewer(inv.Version, p)
		if err != nil {
			return false, err
		}
		if newer {
			return true, nil
		}
	}
	return false, nil
}
func Failures() ([]Operation, error) {
	rows, err := database.DB.Query(`SELECT ` + operationColumns + ` FROM php_update_operations o
	WHERE kind IN ('apply','verify','repair','rollback') AND status NOT IN ('queued','running','succeeded')
	AND NOT EXISTS (SELECT 1 FROM php_update_operations n WHERE n.kind IN ('apply','verify','repair','rollback') AND n.version=o.version AND n.status NOT IN ('queued','running') AND n.rowid>o.rowid)
	ORDER BY created_at DESC LIMIT 10`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Operation{}
	for rows.Next() {
		o, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, o)
	}
	return items, rows.Err()
}

// Applying blocks conflicting app mutations even in the launch/recovery gap.
func Applying() (bool, error) {
	return database.PHPMaintenancePending()
}

func (m *Manager) EnqueueVerification(ctx context.Context, previousID, username, ip string) (Operation, error) {
	if _, err := generationForOperation(previousID); err == nil {
		return Operation{}, fmt.Errorf("%w: use the reviewed Repair action for this maintenance generation", ErrUnsupported)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Operation{}, err
	}
	var existing string
	if err := database.DB.QueryRow("SELECT operation_id FROM php_update_verifications WHERE parent_id=?", previousID).Scan(&existing); err == nil {
		return GetOperation(existing)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Operation{}, err
	}
	old, err := GetOperation(previousID)
	if err != nil {
		return Operation{}, err
	}
	if old.Kind == "check" || old.Active() || old.Status == "succeeded" || len(old.Result) == 0 || string(old.Result) == "{}" {
		return Operation{}, ErrUnsupported
	}
	o := Operation{ID: uuid.NewString(), Kind: "verify", Version: old.Version, PlanID: old.PlanID, Status: "queued", Phase: "queued", Username: username, IPAddress: ip, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(), Result: old.Result, Checkpoint: old.Checkpoint}
	tx, err := database.DB.BeginTx(ctx, nil)
	if err != nil {
		return Operation{}, err
	}
	defer tx.Rollback()
	var latest string
	if err := tx.QueryRow("SELECT id FROM php_update_operations WHERE version=? AND kind IN ('apply','verify','repair','rollback') ORDER BY rowid DESC LIMIT 1", old.Version).Scan(&latest); err != nil {
		return Operation{}, err
	}
	if latest != previousID {
		return Operation{}, ErrPlanChanged
	}
	_, err = tx.Exec(`INSERT INTO php_update_operations(id,kind,version,plan_id,username,ip_address,created_at,updated_at,result,checkpoint) VALUES (?,?,?,?,?,?,?,?,?,?)`, o.ID, o.Kind, o.Version, o.PlanID, username, ip, o.CreatedAt, o.UpdatedAt, string(o.Result), o.Checkpoint)
	if err == nil {
		_, err = tx.Exec("INSERT INTO php_update_verifications(parent_id,operation_id) VALUES (?,?)", previousID, o.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		_ = tx.Rollback()
		if e := database.DB.QueryRow("SELECT operation_id FROM php_update_verifications WHERE parent_id=?", previousID).Scan(&existing); e == nil {
			return GetOperation(existing)
		}
		if active, e := ActiveOperation(); e == nil && active != nil {
			return Operation{}, ErrBusy
		}
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

func resolveAvailability(version, planID string) error {
	items, err := Snapshots()
	if err != nil {
		return err
	}
	for _, s := range items {
		if s.Version == version && s.Plan != nil && s.Plan.ID == planID {
			s.State = "current"
			s.Plan = nil
			s.BlockedReasons = []string{}
			s.CheckedAt = time.Now().Unix()
			s.Notice = false
			return saveSnapshot(s)
		}
	}
	return nil
}

func progress(id, phase, message string) error {
	message = redact(message)
	_, err := database.DB.Exec("UPDATE php_update_operations SET phase=?,message=?,updated_at=? WHERE id=? AND status='running'", phase, message, time.Now().Unix(), id)
	if err == nil {
		_, err = database.DB.Exec("INSERT INTO php_update_events(operation_id,phase,message,created_at) VALUES (?,?,?,?)", id, phase, message, time.Now().Unix())
	}
	return err
}
func finish(id, status, message string) error {
	message = redact(message)
	_, err := database.DB.Exec("UPDATE php_update_operations SET status=?,phase=?,message=?,finished_at=?,updated_at=? WHERE id=? AND status IN ('queued','running')", status, status, message, time.Now().Unix(), time.Now().Unix(), id)
	if err == nil {
		_, err = database.DB.Exec(`INSERT INTO activity(site_id,type,summary,username,ip_address)
		SELECT 0,'php_update',kind || ' PHP ' || version || ': ' || ?,username,ip_address FROM php_update_operations WHERE id=?`, status+" — "+message, id)
	}
	return err
}

func invalidateAvailability(version string) error {
	items, err := Snapshots()
	if err != nil {
		return err
	}
	for _, s := range items {
		if s.Version == version {
			s.State = "stale"
			s.Plan = nil
			s.CheckedAt = 0
			s.Notice = false
			s.BlockedReasons = []string{}
			return saveSnapshot(s)
		}
	}
	return nil
}
