package nodeupdate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/nodetoolchain"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/scheduler"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

type Manager struct {
	DataDir      string
	Launch       func(context.Context, Operation) error
	Inspect      func(context.Context) (nodetoolchain.OwnedRuntime, error)
	Find         func(context.Context, string) (nodetoolchain.Release, error)
	Authenticate func(context.Context, string, string) (nodetoolchain.Release, error)
	Stage        func(context.Context, nodetoolchain.Release, nodetoolchain.ProgressFunc) (nodetoolchain.Release, error)
	Impact       func(context.Context, string, string) (Impact, error)
}

func NewManager(dir string) *Manager {
	m := &Manager{DataDir: dir, Inspect: nodetoolchain.InspectOwnedNode, Find: nodetoolchain.FindUpdate, Authenticate: nodetoolchain.AuthenticateRelease, Stage: nodetoolchain.StageRelease, Impact: inspectImpact}
	m.Launch = m.launch
	return m
}
func trustedPath(p string) error {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p {
		return ErrSupervisor
	}
	for {
		st, e := os.Lstat(p)
		if e != nil {
			return e
		}
		s, ok := st.Sys().(*syscall.Stat_t)
		if !ok || s.Uid != 0 || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("Untrusted root-owned runtime path: %s", p)
		}
		if p == "/" {
			return nil
		}
		p = filepath.Dir(p)
	}
}
func (m *Manager) launch(ctx context.Context, o Operation) error {
	if os.Geteuid() != 0 || os.Getenv("FLUXO_ENV") != "prod" {
		return ErrSupervisor
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	if err = trustedPath(exe); err != nil {
		return fmt.Errorf("%w: %v", ErrSupervisor, err)
	}
	dir, err := filepath.Abs(m.DataDir)
	if err != nil {
		return err
	}
	if err = trustedPath(dir); err != nil {
		return fmt.Errorf("%w: %v", ErrSupervisor, err)
	}
	if _, err = os.Stat("/run/systemd/system"); err != nil {
		return ErrSupervisor
	}
	var out strings.Builder
	err = syscmd.RunMaintenance(ctx, "", &out, nil, "/usr/bin/systemd-run", "--quiet", "--unit=fluxo-node-update-"+o.ID, "--property=Type=exec", "--property=Restart=no", "--property=TimeoutStopSec=infinity", "--setenv=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "--setenv=FLUXO_ENV=prod", "--setenv=FLUXO_DATA_DIR="+dir, "--", exe, "node-update-worker", o.ID)
	if err != nil {
		return fmt.Errorf("Start Node.js maintenance supervisor: %w", err)
	}
	return nil
}
func (m *Manager) Enqueue(ctx context.Context, kind, planID, requestID, username, ip string) (Operation, error) {
	if kind != "check" && kind != "apply" && kind != "repair" && kind != "rollback" {
		return Operation{}, ErrUnsupported
	}
	if _, err := uuid.Parse(requestID); err != nil {
		return Operation{}, errors.New("A request UUID is required")
	}
	var prior Operation
	err := scanJSON(database.DB.QueryRow("SELECT payload FROM node_update_operations WHERE request_id=?", requestID), &prior)
	if err == nil {
		if prior.Kind != kind || prior.PlanID != planID {
			return Operation{}, ErrChanged
		}
		return prior, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Operation{}, err
	}
	if busy, e := scheduler.UpgradeBusy(); e != nil {
		return Operation{}, e
	} else if busy {
		return Operation{}, ErrBusy
	}
	p := Plan{}
	if kind != "check" {
		p, err = GetPlan(planID)
		if err != nil {
			return Operation{}, err
		}
		if p.Kind != kind || time.Now().Unix()-p.CreatedAt >= ReviewSeconds || len(p.Impact.BlockedReasons) > 0 {
			return Operation{}, ErrChanged
		}
	}
	o := Operation{ID: uuid.NewString(), Kind: kind, PlanID: planID, Status: "queued", Phase: "queued", CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(), Username: username, IPAddress: ip, GenerationID: p.GenerationID}
	raw, err := json.Marshal(o)
	if err != nil {
		return o, err
	}
	tx, err := database.DB.BeginTx(ctx, nil)
	if err != nil {
		return o, err
	}
	defer tx.Rollback()
	var busy bool
	err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM php_update_operations WHERE kind!='check' AND status IN ('queued','running')) OR EXISTS(SELECT 1 FROM php_update_generations WHERE state NOT IN ('succeeded','rolled_back')) OR EXISTS(SELECT 1 FROM php_update_barriers) OR EXISTS(SELECT 1 FROM php_repository_migrations WHERE state='pending') OR EXISTS(SELECT 1 FROM node_update_operations WHERE status IN ('queued','running'))`).Scan(&busy)
	if err != nil {
		return o, err
	}
	if busy {
		return o, ErrBusy
	}
	var pending int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM node_update_generations WHERE state NOT IN ('succeeded','rolled_back') AND id!=?`, p.GenerationID).Scan(&pending); err != nil {
		return o, err
	}
	if pending > 0 {
		return o, ErrBusy
	}
	if kind == "apply" {
		var s Snapshot
		if err = scanJSON(tx.QueryRow("SELECT payload FROM node_update_snapshot WHERE id=1"), &s); err != nil {
			return o, err
		}
		if s.State != "available" || s.Candidate == nil || s.DiscoveryID != p.DiscoveryID || !sameSelection(*s.Candidate, p.After) || time.Now().Unix()-s.CheckedAt >= FreshSeconds {
			return o, ErrChanged
		}
	}
	if kind == "repair" || kind == "rollback" {
		var g Generation
		if err = scanJSON(tx.QueryRow("SELECT payload FROM node_update_generations WHERE id=? AND head=? AND id=(SELECT id FROM node_update_generations ORDER BY rowid DESC LIMIT 1)", p.GenerationID, p.Head), &g); err != nil {
			return o, ErrChanged
		}
		g.Head, g.State = o.ID, "recovery_queued"
		b, _ := json.Marshal(g)
		if _, err = tx.Exec("UPDATE node_update_generations SET head=?,state=?,payload=? WHERE id=? AND head=?", g.Head, g.State, string(b), g.ID, p.Head); err != nil {
			return o, err
		}
	}
	_, err = tx.Exec("INSERT INTO node_update_operations(id,kind,plan_id,request_id,status,updated_at,payload) VALUES(?,?,?,?,?,?,?)", o.ID, kind, planID, requestID, o.Status, o.UpdatedAt, string(raw))
	if err != nil {
		return o, ErrBusy
	}
	if err = tx.Commit(); err != nil {
		return o, err
	}
	launchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err = m.Launch(launchCtx, o); err != nil {
		if errors.Is(err, ErrSupervisor) {
			_ = updateOperation(o.ID, "failed", "launch_failed", err.Error())
			return o, err
		} /* A lost systemd reply is uncertain, not proof the worker failed. */
	}
	return o, nil
}
func (m *Manager) Start(ctx context.Context) {
	go func() {
		t := time.NewTimer(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.reconcile(ctx)
				var next int64
				if database.DB.QueryRow("SELECT next_check FROM node_update_snapshot WHERE id=1").Scan(&next) == nil && time.Now().Unix() >= next {
					if _, err := m.Enqueue(ctx, "check", "", uuid.NewString(), "scheduler", ""); err != nil {
						_, _ = database.DB.Exec("UPDATE node_update_snapshot SET next_check=? WHERE id=1", time.Now().Add(time.Hour).Unix())
					}
				}
				t.Reset(time.Minute)
			}
		}
	}()
}
func (m *Manager) reconcile(ctx context.Context) {
	o, err := activeOperation()
	if err != nil || o == nil {
		return
	}
	var heartbeat int64
	if database.DB.QueryRow("SELECT updated_at FROM node_update_operations WHERE id=?", o.ID).Scan(&heartbeat) != nil || time.Now().Unix()-heartbeat < 60 {
		return
	}
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", "fluxo-node-update-"+o.ID+".service", "--property=ActiveState,MainPID,ControlPID,ControlGroup,LoadState")
	if err != nil {
		return
	}
	p := properties(out)
	if p["ActiveState"] != "inactive" && p["ActiveState"] != "failed" && p["LoadState"] != "not-found" {
		return
	}
	if p["MainPID"] != "0" && p["MainPID"] != "" || p["ControlPID"] != "0" && p["ControlPID"] != "" {
		return
	}
	if p["ControlGroup"] != "" {
		pids, e := groupPIDs(p["ControlGroup"])
		if e != nil || len(pids) > 0 {
			return
		}
	}
	_ = updateOperation(o.ID, "interrupted", "interrupted", "Node.js maintenance was interrupted. Review recovery before changing runtimes; no update was replayed.")
}
func (m *Manager) RunWorker(id string) error {
	if os.Geteuid() != 0 || os.Getenv("FLUXO_ENV") != "prod" {
		return ErrSupervisor
	}
	dir, err := filepath.Abs(m.DataDir)
	if err != nil {
		return err
	}
	if err = trustedPath(dir); err != nil {
		return err
	}
	if _, err := uuid.Parse(id); err != nil {
		return err
	}
	o, err := GetOperation(id)
	if err != nil {
		return err
	}
	res, err := database.DB.Exec("UPDATE node_update_operations SET status='running',updated_at=? WHERE id=? AND status='queued'", time.Now().Unix(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("Node.js operation already claimed")
	}
	if err = updateOperation(id, "running", "waiting_for_lock", "Waiting for runtime maintenance access"); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				_, _ = database.DB.Exec("UPDATE node_update_operations SET updated_at=? WHERE id=? AND status='running'", time.Now().Unix(), id)
			}
		}
	}()
	defer func() {
		if r := recover(); r != nil {
			_ = updateOperation(id, "interrupted", "interrupted", fmt.Sprintf("Node.js maintenance interrupted: %v", r))
		}
	}()
	lockCtx, stop := context.WithTimeout(ctx, 2*time.Minute)
	defer stop()
	unlock, err := packages.Lifecycle(lockCtx, o.Kind == "check")
	if err == nil {
		defer unlock()
		var release func()
		release, err = nodetoolchain.LockRuntime(lockCtx)
		if err == nil {
			defer release()
			if busy, e := database.PHPRuntimeMaintenancePending(); e != nil {
				err = e
			} else if busy {
				err = ErrBusy
			} else if o.Kind == "check" {
				err = m.check(ctx, o)
			} else {
				err = m.apply(ctx, o)
			}
		}
	}
	if err != nil {
		current, e := GetOperation(id)
		if e == nil && current.Status == "running" {
			_ = updateOperation(id, "failed", "failed", err.Error())
		}
	}
	return err
}
func (m *Manager) check(ctx context.Context, o Operation) error {
	s, _ := readSnapshot()
	s.AttemptedAt = time.Now().Unix()
	fail := func(e error) error {
		s.State = "check_failed"
		s.Error = e.Error()
		s.UpdateAvailable = false
		if er := saveSnapshot(s, time.Now().Add(time.Hour).Unix()); er != nil {
			return er
		}
		return e
	}
	if busy, err := database.NodeMaintenancePending(); err != nil {
		return fail(err)
	} else if busy {
		return fail(ErrBusy)
	}
	owned, err := m.Inspect(ctx)
	if err != nil {
		return fail(err)
	}
	if owned.Release.NPM == "" {
		return fail(errors.New("Bundled npm is unavailable; repair the toolchain separately"))
	}
	for name, version := range owned.Managers {
		if version == "" {
			return fail(fmt.Errorf("%s is unavailable; repair the toolchain separately", name))
		}
	}
	if err = nodetoolchain.SupportedMajor(owned.Release.Version, time.Now()); err != nil {
		return fail(err)
	}
	proof := s.Installed.Release
	if proof.TreeSHA256 != owned.Release.TreeSHA256 || proof.Version != owned.Release.Version || proof.TrustRevision != nodetoolchain.TrustRevision() || proof.SHA256 == "" {
		proof, err = m.Authenticate(ctx, owned.Release.Version, owned.Release.NPM)
		if err == nil {
			proof, err = m.Stage(ctx, proof, nil)
		}
		if err != nil {
			return fail(err)
		}
		if proof.TreeSHA256 != owned.Release.TreeSHA256 {
			return fail(errors.New("Installed Node.js files differ from the signed official release"))
		}
	}
	owned.Release = proof
	candidate, err := m.Find(ctx, owned.Release.Version)
	if err != nil {
		return fail(err)
	}
	s = Snapshot{State: "current", Installed: owned, CheckedAt: time.Now().Unix(), AttemptedAt: time.Now().Unix(), DiscoveryID: uuid.NewString()}
	if candidate.Version != "" {
		s.State = "available"
		s.Candidate = &candidate
	}
	if err = saveSnapshot(s, time.Now().Add(24*time.Hour).Unix()); err != nil {
		return err
	}
	return updateOperation(o.ID, "succeeded", "complete", "Node.js update check completed")
}
func sameRuntime(a, b nodetoolchain.OwnedRuntime) bool {
	return a.Root == b.Root && a.Release.Version == b.Release.Version && a.Release.NPM == b.Release.NPM && a.Release.TreeSHA256 == b.Release.TreeSHA256 && digest(a.Managers) == digest(b.Managers)
}
func (m *Manager) Review(ctx context.Context, discoveryID string) (Plan, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	unlock, err := packages.Lifecycle(ctx, true)
	if err != nil {
		return Plan{}, err
	}
	defer unlock()
	if busy, e := database.PHPMaintenancePending(); e != nil {
		return Plan{}, e
	} else if busy {
		return Plan{}, ErrBusy
	}
	if active, e := activeOperation(); e != nil {
		return Plan{}, e
	} else if active != nil {
		return Plan{}, ErrBusy
	}
	lock, err := nodetoolchain.LockRuntime(ctx)
	if err != nil {
		return Plan{}, err
	}
	defer lock()
	s, err := readSnapshot()
	if err != nil {
		return Plan{}, err
	}
	if s.DiscoveryID != discoveryID || s.State != "available" || s.Candidate == nil || time.Now().Unix()-s.CheckedAt >= FreshSeconds {
		return Plan{}, ErrChanged
	}
	owned, err := m.Inspect(ctx)
	if err != nil {
		return Plan{}, err
	}
	if !sameRuntime(owned, s.Installed) {
		return Plan{}, ErrChanged
	}
	owned.Release = s.Installed.Release
	impact, err := m.Impact(ctx, owned.Root, "")
	if err != nil {
		return Plan{}, err
	}
	target, err := m.Stage(ctx, *s.Candidate, nil)
	if err != nil {
		return Plan{}, err
	}
	// The immutable discovery identity does not include a staged tree hash. The
	// reviewed release adds exact tree/npm evidence; selection stays unchanged.
	p := Plan{ID: uuid.NewString(), Kind: "apply", DiscoveryID: s.DiscoveryID, CreatedAt: time.Now().Unix(), Before: owned, After: target, Impact: impact}
	return p, savePlan(p)
}
func (m *Manager) ReviewRecovery(ctx context.Context, generationID, kind string) (Plan, error) {
	if kind != "repair" && kind != "rollback" {
		return Plan{}, ErrUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	release, err := packages.Lifecycle(ctx, true)
	if err != nil {
		return Plan{}, err
	}
	defer release()
	if o, e := activeOperation(); e != nil {
		return Plan{}, e
	} else if o != nil {
		return Plan{}, ErrBusy
	}
	g, err := getGeneration(generationID)
	if err != nil {
		return Plan{}, err
	}
	var latest string
	if err = database.DB.QueryRow("SELECT id FROM node_update_generations ORDER BY rowid DESC LIMIT 1").Scan(&latest); err != nil || latest != g.ID {
		return Plan{}, ErrChanged
	}
	owned, err := m.Inspect(ctx)
	if err != nil {
		return Plan{}, err
	}
	if owned.Release.Version != g.Plan.Before.Release.Version && owned.Release.Version != g.Plan.After.Version {
		return Plan{}, ErrChanged
	}
	impact, err := m.Impact(ctx, owned.Root, g.ID)
	if err != nil {
		return Plan{}, err
	}
	if !sameConsumers(impact, g.Plan.Impact) {
		return Plan{}, ErrChanged
	}
	target := g.Plan.After
	if kind == "rollback" {
		target = g.Plan.Before.Release
	}
	hash, err := nodetoolchain.TreeDigest(filepath.Join(nodetoolchain.NodeRoot(), "v"+target.Version))
	if err != nil || hash != target.TreeSHA256 {
		return Plan{}, errors.New("Retained Node.js release needs manual repair")
	}
	p := Plan{ID: uuid.NewString(), Kind: kind, GenerationID: g.ID, Head: g.Head, CreatedAt: time.Now().Unix(), Before: owned, After: target, Impact: impact}
	return p, savePlan(p)
}
