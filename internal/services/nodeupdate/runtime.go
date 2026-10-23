package nodeupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/nodetoolchain"
	"fluxo/internal/services/phpupdate"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

func barrierPath(u Unit) string {
	return filepath.Join("/etc/systemd/system", u.Name+".d", "90-fluxo-node-update.conf")
}
func permitPath(u Unit, id string) string {
	return filepath.Join("/run/fluxo-node-updates", id, u.Name+".permit")
}
func barrierContent(u Unit, id string) string {
	return "[Unit]\nConditionPathExists=" + permitPath(u, id) + "\n[Service]\nRestart=no\nKillMode=control-group\nKillSignal=SIGTERM\nSendSIGKILL=no\nTimeoutStopSec=infinity\n"
}
func durableDirectory(dir string, mode os.FileMode) error {
	if err := os.MkdirAll(dir, mode); err != nil {
		return err
	}
	if err := trustedPath(dir); err != nil {
		return err
	}
	for p := dir; p != "/"; p = filepath.Dir(p) {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		err = f.Sync()
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
func durableWrite(p string, content []byte, mode os.FileMode, exclusive bool) error {
	if err := trustedPath(filepath.Dir(p)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".node-maintenance-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(content)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if exclusive {
		err = os.Link(f.Name(), p)
		if os.IsExist(err) {
			if trustedPath(p) != nil {
				return errors.New("Unsafe existing Node.js maintenance file")
			}
			b, e := os.ReadFile(p)
			if e != nil || string(b) != string(content) {
				return errors.New("Conflicting Node.js maintenance file")
			}
			return nil
		}
		if err == nil {
			err = os.Remove(f.Name())
		}
	} else {
		err = os.Rename(f.Name(), p)
	}
	if err != nil {
		return err
	}
	return syncDir(filepath.Dir(p))
}
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func verifyBarrier(u Unit, id string) error {
	p := barrierPath(u)
	if err := trustedPath(p); err != nil {
		return err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if string(b) != barrierContent(u, id) {
		return errors.New("Node.js maintenance barrier has changed")
	}
	return nil
}
func (m *Manager) hold(ctx context.Context, g Generation) error {
	if _, err := uuid.Parse(g.ID); err != nil {
		return err
	}
	for _, u := range g.executionImpact().Units {
		if u.Name != fmt.Sprintf("fluxo-daemon-%d.service", u.ID) || u.ID <= 0 {
			return errors.New("Invalid managed Node.js unit identity")
		}
		if _, err := database.DB.Exec("INSERT INTO node_update_barriers(unit,generation_id) VALUES(?,?) ON CONFLICT(unit) DO NOTHING", u.Name, g.ID); err != nil {
			return err
		}
		var owner string
		if err := database.DB.QueryRow("SELECT generation_id FROM node_update_barriers WHERE unit=?", u.Name).Scan(&owner); err != nil || owner != g.ID {
			return errors.New("Node.js start barrier belongs to another generation")
		}
		if err := durableDirectory(filepath.Dir(barrierPath(u)), 0755); err != nil {
			return err
		}
		if err := durableWrite(barrierPath(u), []byte(barrierContent(u, g.ID)), 0644, true); err != nil {
			return err
		}
	}
	_, err := syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "daemon-reload")
	return err
}
func (m *Manager) stopConsumers(ctx context.Context, g Generation) error {
	for _, u := range g.executionImpact().Units {
		if err := verifyBarrier(u, g.ID); err != nil {
			return err
		}
		if err := os.Remove(permitPath(u, g.ID)); err != nil && !os.IsNotExist(err) {
			return err
		}
		out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", u.Name, "--property=Restart,SendSIGKILL,TimeoutStopUSec,KillMode")
		if err != nil {
			return err
		}
		p := properties(out)
		if p["Restart"] != "no" || p["SendSIGKILL"] != "no" || p["TimeoutStopUSec"] != "infinity" || p["KillMode"] != "control-group" {
			return errors.New("Node.js stop barrier is not effective")
		}
		if _, err = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "stop", "--no-block", u.Name); err != nil {
			return err
		}
	}
	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		drained := true
		for _, u := range g.executionImpact().Units {
			pids, err := groupPIDs("/system.slice/" + u.Name)
			if err != nil {
				return err
			}
			if len(pids) > 0 {
				drained = false
			}
			out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", u.Name, "--property=ActiveState,MainPID")
			if err != nil {
				return err
			}
			p := properties(out)
			if p["ActiveState"] != "inactive" && p["ActiveState"] != "failed" || p["MainPID"] != "0" {
				drained = false
			}
		}
		if drained {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Application processes did not stop safely; runtime was held for recovery without force-killing them")
		case <-tick.C:
		}
	}
}
func (m *Manager) startConsumers(ctx context.Context, g Generation) error {
	for _, u := range g.executionImpact().Units {
		if !u.Active {
			continue
		}
		if err := verifyBarrier(u, g.ID); err != nil {
			return err
		}
		if err := resetFailedForStart(ctx, u.Name); err != nil {
			return err
		}
		if err := durableDirectory(filepath.Dir(permitPath(u, g.ID)), 0755); err != nil {
			return err
		}
		if err := durableWrite(permitPath(u, g.ID), []byte(g.ID+"\n"), 0644, false); err != nil {
			return err
		}
		if _, err := syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "start", "--no-block", u.Name); err != nil {
			return err
		}
	}
	return nil
}
func (m *Manager) verifyConsumers(ctx context.Context, g Generation, target nodetoolchain.Release) error {
	deadline := time.NewTimer(45 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	stableSince := map[string]time.Time{}
	identities := map[string]string{}
	last := "Applications are still starting"
	for {
		healthy := true
		for _, original := range g.executionImpact().Units {
			u, err := inspectUnit(ctx, original, g.ID)
			if err != nil {
				healthy = false
				last = err.Error()
				continue
			}
			if u.Enabled != original.Enabled {
				return errors.New("Application enablement changed during maintenance")
			}
			if !original.Active {
				if u.Active || len(u.PIDs) > 0 {
					return errors.New("A previously stopped application was started")
				}
				continue
			}
			if !u.Active {
				healthy = false
				continue
			}
			if err = verifyListener(ctx, u, filepath.Join(nodetoolchain.NodeRoot(), "v"+target.Version), !original.BunOnly); err != nil {
				healthy = false
				last = err.Error()
				delete(stableSince, u.Name)
				continue
			}
			key := digest(u.PIDs)
			if identities[u.Name] != key {
				identities[u.Name] = key
				stableSince[u.Name] = time.Now()
			}
			if time.Since(stableSince[u.Name]) < 3*time.Second {
				healthy = false
			}
		}
		if healthy {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("Node.js application verification failed: %s", last)
		case <-tick.C:
		}
	}
}
func (m *Manager) release(ctx context.Context, g Generation) error {
	// Runtime health and approval are durable before release starts. Any leftover
	// barrier keeps admission closed independently of the operation's status.
	for _, u := range g.executionImpact().Units {
		p := barrierPath(u)
		if _, err := os.Lstat(p); err == nil {
			if err = verifyBarrier(u, g.ID); err != nil {
				return err
			}
			if err = os.Remove(p); err != nil {
				return err
			}
			if err = syncDir(filepath.Dir(p)); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if _, err := database.DB.Exec("DELETE FROM node_update_barriers WHERE unit=? AND generation_id=?", u.Name, g.ID); err != nil {
			return err
		}
	}
	if _, err := syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "daemon-reload"); err != nil {
		return err
	}
	return nil
}
func (m *Manager) apply(ctx context.Context, o Operation) error {
	p, err := GetPlan(o.PlanID)
	if err != nil {
		return err
	}
	if p.ConsumerPolicy != consumerPolicy || p.Kind != o.Kind || time.Now().Unix()-p.CreatedAt >= ReviewSeconds || len(p.Impact.BlockedReasons) > 0 {
		return ErrChanged
	}
	owned, err := m.Inspect(ctx)
	if err != nil {
		return err
	}
	if !sameRuntime(owned, p.Before) {
		return ErrChanged
	}
	var count int
	if err = database.DB.QueryRow(`SELECT (SELECT COUNT(*) FROM deployments WHERE status='running')+(SELECT COUNT(*) FROM commands WHERE status='running')`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return errors.New("Wait for accepted deployments and site commands to finish before updating Node.js")
	}
	var g Generation
	if o.Kind == "apply" {
		impact, e := m.Impact(ctx, owned.Root, "")
		if e != nil {
			return e
		}
		if !sameApplyConsumers(impact, p.Impact) {
			return ErrChanged
		}
		s, e := readSnapshot()
		if e != nil {
			return e
		}
		if s.State != "available" || s.DiscoveryID != p.DiscoveryID || s.Candidate == nil || !sameSelection(*s.Candidate, p.After) || time.Now().Unix()-s.CheckedAt >= FreshSeconds {
			return ErrChanged
		}
		if err = nodetoolchain.SupportedMajor(owned.Release.Version, time.Now()); err != nil {
			return err
		}
		if a, ok := versionMajor(p.After.Version); !ok {
			return ErrChanged
		} else if b, _ := versionMajor(p.Before.Release.Version); a != b {
			return ErrChanged
		}
		if err = updateOperation(o.ID, "running", "staging", "Verifying the approved release before stopping applications"); err != nil {
			return err
		}
		staged, e := m.Stage(ctx, p.After, nil)
		if e != nil {
			return e
		}
		if digest(staged) != digest(p.After) {
			return ErrChanged
		}
		freshImpact, e := m.Impact(ctx, owned.Root, "")
		if e != nil {
			return e
		}
		if !sameApplyConsumers(freshImpact, p.Impact) {
			return ErrChanged
		}
		coordination, e := applyCoordination(p.Impact, impact, freshImpact)
		if e != nil {
			return e
		}
		retained, e := m.refreshRetained(ctx, p)
		if e != nil {
			return e
		}
		if err = validateBunRuntimes(ctx, p.Impact); err != nil {
			return err
		}
		boot, e := bootID()
		if e != nil {
			return e
		}
		g = Generation{ID: o.ID, Head: o.ID, State: "prepared", Plan: p, Boot: boot, Goal: "after", Retained: &retained, Coordination: &coordination}
		if err = saveGeneration(g); err != nil {
			return err
		}
	} else {
		g, err = getGeneration(p.GenerationID)
		if err != nil {
			return err
		}
		impact, e := m.RecoveryImpact(ctx, g)
		if e != nil {
			return e
		}
		// Health and PIDs can change naturally while a recovery is reviewed.
		// Rebind the authenticated contract, retaining the ORIGINAL intent.
		if g.Head != o.ID || !sameConsumers(impact, p.Impact) || !samePHPIntent(impact.PHP, p.Impact.PHP) || !sameOriginalConsumers(impact, g.Plan.Impact) {
			return ErrChanged
		}
		if err = updateOperation(o.ID, "running", "reauthenticating", "Authenticating both retained Node.js releases before stopping applications"); err != nil {
			return err
		}
		retained, e := m.refreshRetained(ctx, g.Plan)
		if e != nil {
			return e
		}
		if p.Retained == nil || digest(retained) != digest(*p.Retained) {
			return ErrChanged
		}
		target := retained.After
		if o.Kind == "rollback" {
			target = retained.Before
		}
		if digest(target) != digest(p.After) {
			return ErrChanged
		}
		if err = validateBunRuntimes(ctx, impact); err != nil {
			return err
		}
		g.Retained = &retained
		g.Coordination = &impact
		g.Goal = "after"
		if o.Kind == "rollback" {
			g.Goal = "before"
		}
		g.State = "prepared"
		if err = saveGeneration(g); err != nil {
			return err
		}
	}
	if err = m.transition(ctx, o, &g); err == nil {
		return nil
	}
	// Only an apply automatically rolls back. Recovery is always the operator's
	// reviewed intent. A failed rollback keeps its journal and start barriers.
	if o.Kind == "apply" && g.ActivationAttempted {
		cause := err
		g.Goal = "before"
		g.State = "rolling_back"
		if e := saveGeneration(g); e != nil {
			return e
		}
		_ = updateOperation(o.ID, "running", "rolling_back", "Update failed; restoring the retained previous Node.js release")
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if e := m.transition(rollbackCtx, o, &g); e == nil {
			_ = updateOperation(o.ID, "rolled_back", "rolled_back", "Update failed and the previous runtime was restored and verified: "+cause.Error())
			return nil
		} else {
			err = fmt.Errorf("Update failed: %v; rollback needs recovery: %w", cause, e)
		}
	}
	g.State = "recovery_required"
	if e := saveGeneration(g); e != nil {
		return e
	}
	_ = updateOperation(o.ID, "recovery_required", "recovery_required", err.Error())
	return err
}
func (m *Manager) transition(ctx context.Context, o Operation, g *Generation) error {
	if g.Coordination == nil || !sameOriginalConsumers(*g.Coordination, g.Plan.Impact) {
		return errors.New("Fresh reviewed application coordination is required before maintenance")
	}
	if err := validateBunRuntimes(ctx, *g.Coordination); err != nil {
		return err
	}
	if g.Retained == nil {
		return errors.New("Fresh retained Node.js proofs are required before stopping applications")
	}
	// These are predictable activation prerequisites, so check both paths
	// before any service hold or stop. Activation repeats the checks for drift.
	for _, r := range []nodetoolchain.Release{g.Retained.Before, g.Retained.After} {
		if err := m.ValidateRelease(r); err != nil {
			return err
		}
	}
	target := g.Retained.After
	if g.Goal == "before" {
		target = g.Retained.Before
	}
	if err := phpupdate.ValidateMaintenancePHP(ctx, g.executionImpact().PHP); err != nil {
		return err
	}
	if err := m.preparePHPState(ctx, g); err != nil {
		return err
	}
	if err := m.hold(ctx, *g); err != nil {
		return err
	}
	if err := m.holdPHP(ctx, g); err != nil {
		return err
	}
	// Verify the whole hold set before draining even the first application.
	for _, c := range g.executionImpact().PHP {
		for _, u := range c.Units {
			if err := m.verifyPHPHold(ctx, g, u); err != nil {
				return err
			}
		}
	}
	for _, u := range g.executionImpact().Units {
		if err := verifyBarrier(u, g.ID); err != nil {
			return err
		}
		out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", u.Name, "--property=DropInPaths,Restart,SendSIGKILL,TimeoutStopUSec,KillMode,NeedDaemonReload")
		if err != nil {
			return err
		}
		p := properties(out)
		if p["DropInPaths"] != barrierPath(u) || p["Restart"] != "no" || p["SendSIGKILL"] != "no" || p["TimeoutStopUSec"] != "infinity" || p["KillMode"] != "control-group" || p["NeedDaemonReload"] != "no" {
			return errors.New("Application start hold is not effective")
		}
	}
	g.State = "stopping"
	if err := saveGeneration(*g); err != nil {
		return err
	}
	if err := updateOperation(o.ID, "running", "stopping", "Draining affected Node.js and PHP consumers; custom Artisan commands receive no stop signal"); err != nil {
		return err
	}
	if err := m.stopConsumers(ctx, *g); err != nil {
		return err
	}
	if err := m.drainPHP(ctx, g); err != nil {
		return err
	}
	g.State = "activating"
	if err := saveGeneration(*g); err != nil {
		return err
	}
	if err := updateOperation(o.ID, "running", "activating", "Activating the exact retained Node.js release"); err != nil {
		return err
	}
	g.ActivationAttempted = true
	if err := saveGeneration(*g); err != nil {
		return err
	}
	if err := nodetoolchain.ActivateRelease(target); err != nil {
		return err
	}
	active, err := m.Inspect(ctx)
	if err != nil {
		return err
	}
	if active.Release.Version != target.Version || active.Release.NPM != target.NPM || active.Release.TreeSHA256 != target.TreeSHA256 {
		return errors.New("Activated Node.js or bundled npm differs from the approved release")
	}
	if err := nodetoolchain.ValidateManagers(ctx, g.Plan.Before.Managers); err != nil {
		return err
	}
	g.State = "starting"
	if err := saveGeneration(*g); err != nil {
		return err
	}
	if err := m.startConsumers(ctx, *g); err != nil {
		return err
	}
	if err := m.resumePHP(ctx, g); err != nil {
		return err
	}
	if err := updateOperation(o.ID, "running", "verifying", "Verifying application stability, listener ownership and runtime adoption"); err != nil {
		return err
	}
	if err := m.verifyConsumers(ctx, *g, target); err != nil {
		return err
	}
	if err := m.verifyPHP(ctx, g, filepath.Join(nodetoolchain.NodeRoot(), "v"+target.Version)); err != nil {
		return err
	}
	if err := validateBunRuntimes(ctx, *g.Coordination); err != nil {
		return err
	}
	if err := nodetoolchain.ApproveRelease(target); err != nil {
		return err
	}
	g.State = "verified"
	if err := saveGeneration(*g); err != nil {
		return err
	}
	if err := m.releasePHP(ctx, g); err != nil {
		return err
	}
	if err := m.release(ctx, *g); err != nil {
		return err
	}
	g.State = "succeeded"
	if g.Goal == "before" {
		g.State = "rolled_back"
	}
	if err := saveGeneration(*g); err != nil {
		return err
	}
	s, e := readSnapshot()
	if e != nil {
		return e
	}
	s.State = "stale"
	s.UpdateAvailable = false
	s.Notice = false
	if err := saveSnapshot(s, time.Now().Unix()); err != nil {
		return err
	}
	return updateOperation(o.ID, g.State, "complete", map[bool]string{true: "Previous Node.js runtime restored and applications verified", false: "Node.js updated and affected applications verified"}[g.Goal == "before"])
}
func sameSelection(a, b nodetoolchain.Release) bool {
	a.TreeSHA256, b.TreeSHA256 = "", ""
	return digest(a) == digest(b)
}
func versionMajor(v string) (string, bool) { f := strings.Split(v, "."); return f[0], len(f) == 3 }
