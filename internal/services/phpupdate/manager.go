package phpupdate

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/packages"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

//go:embed apt_bridge.py
var aptBridge string

var repositoryCredentials = regexp.MustCompile(`(?i)(https?://)[^/@\s]+:[^/@\s]+@`)

func redact(s string) string { return repositoryCredentials.ReplaceAllString(s, "${1}[redacted]@") }

type Manager struct {
	DataDir string
	Launch  func(context.Context, Operation) error
}

func NewManager(dataDir string) *Manager {
	m := &Manager{DataDir: dataDir}
	m.Launch = m.launch
	return m
}

// Both subprocess levels must use the same absolute, trusted state directory.
func (m *Manager) workerEnvironment() ([]string, error) {
	dir, err := filepath.Abs(m.DataDir)
	if err != nil {
		return nil, err
	}
	if err = rootOwned(dir); err != nil {
		return nil, err
	}
	return []string{"FLUXO_ENV=prod", "FLUXO_DATA_DIR=" + dir}, nil
}

func trustedExecutable() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	if err := rootOwned(p); err != nil {
		return "", fmt.Errorf("PHP maintenance needs a root-owned installed Fluxo binary: %w", err)
	}
	return p, nil
}
func (m *Manager) launch(ctx context.Context, o Operation) error {
	if os.Geteuid() != 0 || os.Getenv("FLUXO_ENV") != "prod" {
		return ErrSupervisor
	}
	exe, err := trustedExecutable()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSupervisor, err)
	}
	if _, err = os.Stat("/run/systemd/system"); err != nil {
		return ErrSupervisor
	}
	dir, err := filepath.Abs(m.DataDir)
	if err != nil {
		return err
	}
	if err = rootOwned(dir); err != nil {
		return fmt.Errorf("%w: %v", ErrSupervisor, err)
	}
	var b strings.Builder
	err = syscmd.RunMaintenance(ctx, "", &b, nil, "/usr/bin/systemd-run", "--quiet", "--unit=fluxo-php-update-"+o.ID,
		"--property=Type=exec", "--property=Restart=no", "--property=TimeoutStopSec=infinity",
		"--setenv=FLUXO_ENV=prod", "--setenv=FLUXO_DATA_DIR="+dir, "--", exe, "php-update-worker", o.ID)
	if err != nil {
		return fmt.Errorf("start PHP maintenance supervisor: %w (%s)", err, strings.TrimSpace(b.String()))
	}
	return nil
}

func (m *Manager) Enqueue(ctx context.Context, kind, version, planID, requestID, username, ip string, acknowledgeNoRollback ...bool) (Operation, error) {
	acknowledge := len(acknowledgeNoRollback) == 1 && acknowledgeNoRollback[0]
	if kind != "check" && kind != "apply" {
		return Operation{}, ErrUnsupported
	}
	if version != "" && !ValidBranch(version) {
		return Operation{}, ErrUnsupported
	}
	if kind == "check" && version != "" {
		if _, err := os.Stat("/usr/sbin/php-fpm" + version); err != nil {
			return Operation{}, ErrUnsupported
		}
	}
	if requestID != "" {
		o, err := scanOperation(database.DB.QueryRow("SELECT "+operationColumns+" FROM php_update_operations WHERE request_id=?", requestID))
		if err == nil {
			if o.Kind != kind || o.Version != version || o.PlanID != planID {
				return Operation{}, ErrPlanChanged
			}
			return o, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Operation{}, err
		}
	}
	if active, err := ActiveOperation(); err != nil {
		return Operation{}, err
	} else if active != nil {
		if kind == "check" && active.Kind == "check" && (active.Version == version || active.Version == "") {
			return *active, nil
		}
		return Operation{}, ErrBusy
	}
	if kind == "apply" {
		if err := admitApply(database.DB, version, planID); err != nil {
			return Operation{}, err
		}
		p, err := GetPlan(planID)
		if err != nil {
			return Operation{}, err
		}
		if p.Version != version || time.Now().Unix()-p.CreatedAt >= int64(FreshFor.Seconds()) {
			return Operation{}, ErrPlanChanged
		}
		items, err := Snapshots()
		if err != nil {
			return Operation{}, err
		}
		eligible := false
		for _, s := range items {
			if s.Version == version && s.UpdateAvailable && s.Plan.ID == planID {
				eligible = true
			}
		}
		if !eligible {
			return Operation{}, ErrPlanChanged
		}
		if !p.Rollback.Available && !acknowledge {
			return Operation{}, ErrRollbackAcknowledgement
		}
	}
	o := Operation{ID: uuid.NewString(), Kind: kind, Version: version, PlanID: planID, Status: "queued", Phase: "queued", Username: username, IPAddress: ip, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
	var req any
	if requestID != "" {
		req = requestID
	}
	tx, err := database.DB.BeginTx(ctx, nil)
	if err != nil {
		return Operation{}, err
	}
	defer tx.Rollback()
	if kind == "apply" {
		if err = admitApply(tx, version, planID); err != nil {
			return Operation{}, err
		}
	}
	_, err = tx.Exec(`INSERT INTO php_update_operations(id,kind,version,plan_id,request_id,username,ip_address,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?)`, o.ID, kind, version, planID, req, username, ip, o.CreatedAt, o.UpdatedAt)
	if err == nil && kind == "apply" {
		_, err = tx.Exec("INSERT INTO php_update_approvals(operation_id,acknowledge_no_rollback) VALUES (?,?)", o.ID, acknowledge)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		_ = tx.Rollback()
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
		// A lost systemd reply does not prove launch failed. Keep the durable
		// claim until reconciliation establishes the unit's actual state.
		return o, nil
	}
	return o, nil
}

// Start only runs in installed production mode. One small timer drives daily
// discovery; reading any HTTP page never refreshes APT metadata.
func (m *Manager) Start(ctx context.Context) {
	go func() {
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				m.reconcile(ctx)
				var next int64
				if err := database.DB.QueryRow("SELECT next_check FROM php_update_schedule WHERE id=1").Scan(&next); err == nil && time.Now().Unix() >= next {
					if _, err := m.Enqueue(ctx, "check", "", "", "", "scheduler", ""); err != nil && !errors.Is(err, ErrBusy) {
						log.Printf("PHP daily check: %v", err)
					}
				}
				timer.Reset(time.Minute)
			}
		}
	}()
}
func (m *Manager) reconcile(ctx context.Context) {
	o, err := ActiveOperation()
	if err != nil || o == nil {
		return
	}
	if time.Now().Unix()-o.UpdatedAt < 60 {
		return
	}
	text, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", "fluxo-php-update-"+o.ID+".service", "--property=ActiveState,LoadState,MainPID,ControlPID,ControlGroup")
	if err != nil {
		return
	} // inability to query systemd is not proof of interruption.
	properties := unitProperties(text)
	if !supervisorTerminal(properties) {
		return
	}
	if remaining, e := supervisorProcesses(properties["ControlGroup"]); e != nil || remaining {
		return
	}
	if o.Status == "queued" {
		_ = finish(o.ID, "interrupted", "The maintenance supervisor did not start. Check for updates and retry.")
		return
	}
	_ = finish(o.ID, "interrupted", "PHP maintenance was interrupted. Inspect package and runtime state before retrying; no package transaction was replayed.")
}

func (m *Manager) RunWorker(id string) error {
	o, err := GetOperation(id)
	if err != nil {
		return err
	}
	res, err := database.DB.Exec("UPDATE php_update_operations SET status='running',started_at=?,updated_at=? WHERE id=? AND status='queued'", time.Now().Unix(), time.Now().Unix(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("operation already claimed")
	}
	ctx := context.Background()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				_, _ = database.DB.Exec("UPDATE php_update_operations SET updated_at=? WHERE id=? AND status='running'", time.Now().Unix(), id)
			}
		}
	}()
	defer func() {
		if r := recover(); r != nil {
			_ = finish(id, "interrupted", fmt.Sprintf("Maintenance stopped unexpectedly: %v", r))
		}
	}()
	_ = progress(id, "waiting_for_lock", "Waiting for package maintenance access")
	lockCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var releaseLife func()
	if o.Kind == "apply" || o.Kind == "verify" || o.Kind == "repair" || o.Kind == "rollback" {
		releaseLife, err = packages.Lifecycle(lockCtx, false)
		if err != nil {
			_ = finish(id, "failed", err.Error())
			return err
		}
		defer releaseLife()
	}
	unlock, err := packages.Acquire(lockCtx)
	if err != nil {
		_ = finish(id, "failed", err.Error())
		return err
	}
	defer unlock()
	if o.Kind == "check" {
		err = m.check(ctx, o)
	} else if o.Kind == "verify" {
		err = m.verify(ctx, o)
	} else if o.Kind == "repair" || o.Kind == "rollback" {
		err = m.recover(ctx, o)
	} else {
		err = m.apply(ctx, o)
	}
	if err != nil {
		if o.Kind == "check" || errors.Is(err, ErrPlanChanged) {
			items, _ := Snapshots()
			for _, s := range items {
				if o.Version == "" || s.Version == o.Version {
					s.State = "check_failed"
					s.AttemptedAt = time.Now().Unix()
					s.Error = err.Error()
					_ = saveSnapshot(s)
				}
			}
			if o.Kind == "check" {
				_, _ = database.DB.Exec("UPDATE php_update_schedule SET next_check=? WHERE id=1", time.Now().Add(time.Hour).Unix())
			}
		}
		current, _ := GetOperation(id)
		if current.Active() {
			_ = finish(id, "failed", err.Error())
		}
	}
	return err
}

type bridgeWriter struct {
	mu      sync.Mutex
	id      string
	pending string
	result  json.RawMessage
	err     string
	output  strings.Builder
}

func (w *bridgeWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending += string(b)
	for {
		line, rest, ok := strings.Cut(w.pending, "\n")
		if !ok {
			break
		}
		w.pending = rest
		line = redact(line)
		if strings.HasPrefix(line, "FLUXO_JSON:") {
			raw := strings.TrimPrefix(line, "FLUXO_JSON:")
			var event struct {
				Phase       string          `json:"phase"`
				Message     string          `json:"message"`
				Error       string          `json:"error"`
				Branches    json.RawMessage `json:"branches"`
				Archives    json.RawMessage `json:"archives"`
				Observation json.RawMessage `json:"observation"`
			}
			if json.Unmarshal([]byte(raw), &event) == nil {
				if event.Branches != nil || event.Archives != nil || event.Observation != nil {
					w.result = []byte(raw)
				}
				if event.Error != "" {
					w.err = event.Error
				}
				if event.Phase != "" && w.id != "" {
					_ = progress(w.id, event.Phase, event.Message)
				}
			}
		}
		// Bounded durable diagnostic output; never stores environment or credentials.
		if w.id != "" {
			_, _ = database.DB.Exec("UPDATE php_update_operations SET output=substr(output || ?, -65536) WHERE id=?", line+"\n", w.id)
		}
		if w.output.Len() < 65536 {
			w.output.WriteString(line + "\n")
		}
	}
	return len(b), nil
}
func bridgeRaw(ctx context.Context, id string, request any) (json.RawMessage, error) {
	raw, _ := json.Marshal(request)
	w := &bridgeWriter{id: id}
	err := syscmd.RunMaintenance(ctx, string(raw), w, []string{"DEBIAN_FRONTEND=noninteractive", "DEBIAN_PRIORITY=critical", "NEEDRESTART_MODE=l", "APT_LISTCHANGES_FRONTEND=none", "SYSTEMD_OFFLINE=1", "UCF_FORCE_CONFFOLD=1"}, "/usr/bin/python3", "-I", "-c", aptBridge)
	if err != nil {
		if w.err != "" {
			return nil, errors.New(w.err)
		}
		return nil, fmt.Errorf("PHP package inspection failed: %w: %s", err, w.output.String())
	}
	if w.result == nil {
		if r, ok := request.(map[string]any); ok && r["action"] == "inspect" {
			return nil, errors.New("Package inspection returned no complete inventory")
		}
		return nil, nil
	}
	return w.result, nil
}

func bridge(ctx context.Context, id string, request any) ([]Inventory, error) {
	raw, err := bridgeRaw(ctx, id, request)
	if err != nil {
		return nil, err
	}
	var result struct {
		Branches []Inventory `json:"branches"`
	}
	if len(raw) == 0 {
		return nil, nil
	}
	err = json.Unmarshal(raw, &result)
	return result.Branches, err
}

var _ io.Writer = (*bridgeWriter)(nil)

func (m *Manager) check(ctx context.Context, o Operation) error {
	_ = progress(o.ID, "refreshing", "Checking PHP package updates")
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	w := &bridgeWriter{id: o.ID}
	err := syscmd.RunMaintenance(checkCtx, "", w, []string{"DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=l"}, "/usr/bin/apt-get", "-o", "APT::Update::Error-Mode=any", "-o", "DPkg::Lock::Timeout=120", "update")
	if err != nil {
		items, _ := Snapshots()
		for _, s := range items {
			s.State = "check_failed"
			s.AttemptedAt = time.Now().Unix()
			s.Error = "Repository refresh failed; cached candidates were not accepted"
			_ = saveSnapshot(s)
		}
		_, _ = database.DB.Exec("UPDATE php_update_schedule SET next_check=? WHERE id=1", time.Now().Add(time.Hour).Unix())
		return errors.New("Unable to refresh package repositories; retry later")
	}
	inventories, err := bridge(checkCtx, o.ID, map[string]any{"action": "inspect"})
	if err != nil {
		return err
	}
	found := o.Version == ""
	installed := map[string]bool{}
	for _, inv := range inventories {
		installed[inv.Version] = true
		if inv.Version == o.Version {
			found = true
		}
		impact, impactErr := inspectImpact(checkCtx, inv.Version)
		if impactErr != nil {
			inv.BlockedReasons = append(inv.BlockedReasons, impactErr.Error())
		}
		inv.BlockedReasons = append(inv.BlockedReasons, impact.BlockedReasons...)
		if err := validateNativeChanges(inv, impact); err != nil {
			inv.BlockedReasons = append(inv.BlockedReasons, err.Error())
		}
		p := Plan{ID: uuid.NewString(), Version: inv.Version, CreatedAt: time.Now().Unix(), Inventory: inv, Impact: impact}
		p.Rollback = rollbackCapability(inv)
		p.CandidateID = candidateIdentity(inv.Changes)
		notice, err := shouldNotify(inv)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(p)
		if _, err = database.DB.Exec("INSERT INTO php_update_plans(id,version,payload,created_at) VALUES (?,?,?,?)", p.ID, p.Version, string(raw), p.CreatedAt); err != nil {
			return err
		}
		state := "current"
		if len(inv.Changes) > 0 {
			state = "available"
		}
		if len(inv.BlockedReasons) > 0 {
			state = "blocked"
		}
		if err = saveSnapshot(Snapshot{Version: inv.Version, State: state, CheckedAt: p.CreatedAt, AttemptedAt: p.CreatedAt, Plan: &p, BlockedReasons: inv.BlockedReasons, Notice: notice}); err != nil {
			return err
		}
	}
	// Removed branches must not leave permanent global availability notices.
	if err := reconcileInstalledSnapshots(installed); err != nil {
		return err
	}
	_, _ = database.DB.Exec("UPDATE php_update_schedule SET next_check=? WHERE id=1", time.Now().Add(FreshFor).Unix())
	if !found {
		return errors.New("The requested PHP branch is not installed")
	}
	_ = finish(o.ID, "succeeded", "PHP update check completed")
	return nil
}

func reconcileInstalledSnapshots(installed map[string]bool) error {
	previous, err := Snapshots()
	if err != nil {
		return err
	}
	for _, s := range previous {
		if !installed[s.Version] {
			if _, err := database.DB.Exec("DELETE FROM php_update_snapshots WHERE version=?", s.Version); err != nil {
				return err
			}
		}
	}
	return nil
}
