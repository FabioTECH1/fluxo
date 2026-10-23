package nodeupdate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/phpupdate"
	"fluxo/internal/syscmd"
)

// This belongs to the Node generation. PHP package history is never fabricated
// or edited when coordinating these consumers for a Node-only switch.
var phpUnitName = regexp.MustCompile(`^fluxo-daemon-[1-9][0-9]*(?:-[1-9][0-9]*)?\.service$`)

type PHPState struct {
	Evidence *phpupdate.MaintenanceEvidence          `json:"evidence,omitempty"`
	Schema   int                                     `json:"schema"`
	Original []phpupdate.MaintenanceConsumer         `json:"original"`
	Attempt  map[string]phpupdate.MaintenanceRuntime `json:"attempt"`
	Stops    map[string]phpupdate.MaintenanceStop    `json:"stops"`
	Resumes  map[string]phpupdate.MaintenanceResume  `json:"resumes"`
}
type phpBarrier struct {
	Unit       string            `json:"unit"`
	Generation string            `json:"generation"`
	Original   map[string]string `json:"original"`
	Releasing  bool              `json:"releasing,omitempty"`
}

func phpBarrierUnit(name string) Unit { return Unit{Name: name} }
func phpBarrierContent(name, id string) string {
	return strings.Replace(barrierContent(phpBarrierUnit(name), id), "KillMode=control-group", "KillMode=mixed", 1)
}
func readPHPBarrier(name string) (phpBarrier, error) {
	var b phpBarrier
	var raw string
	if err := database.DB.QueryRow("SELECT payload FROM node_update_php_barriers WHERE unit=?", name).Scan(&raw); err != nil {
		return b, err
	}
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		return b, err
	}
	if b.Unit != name || !phpUnitName.MatchString(name) || b.Generation == "" || b.Original == nil {
		return b, errors.New("Invalid Node-owned PHP hold")
	}
	return b, nil
}
func normalizePHPBarrier(name, id string, p map[string]string) (map[string]string, error) {
	b, err := readPHPBarrier(name)
	if errors.Is(err, sql.ErrNoRows) {
		if p["DropInPaths"] != "" {
			return nil, errors.New("Unrecognized PHP consumer override")
		}
		return p, nil
	}
	if err != nil {
		return nil, err
	}
	if b.Generation != id || id == "" {
		return nil, errors.New("PHP consumer hold belongs to another generation")
	}
	path := barrierPath(phpBarrierUnit(name))
	data, e := os.ReadFile(path)
	if e == nil {
		if err = trustedPath(path); err != nil {
			return nil, err
		}
		if string(data) != phpBarrierContent(name, id) || p["DropInPaths"] != path {
			return nil, errors.New("PHP consumer hold changed")
		}
		for key, want := range map[string]string{"Restart": "no", "KillMode": "mixed", "KillSignal": "15", "SendSIGKILL": "no", "TimeoutStopUSec": "infinity"} {
			got := p[key]
			if key == "KillSignal" && got == "SIGTERM" {
				got = "15"
			}
			if got != want {
				return nil, fmt.Errorf("PHP consumer hold is not effective (%s)", key)
			}
		}
		for _, key := range []string{"Restart", "KillMode", "KillSignal", "SendSIGKILL", "TimeoutStopUSec", "DropInPaths"} {
			p[key] = b.Original[key]
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	} else if p["DropInPaths"] != "" {
		return nil, errors.New("PHP hold journal does not match systemd")
	}
	return p, nil
}

func phpNodeProcess(roots []string) func(phpupdate.MaintenanceRuntime, int) (bool, error) {
	return func(u phpupdate.MaintenanceRuntime, pid int) (bool, error) {
		path := fmt.Sprintf("/proc/%d/exe", pid)
		exe, err := os.Readlink(path)
		if err != nil {
			return false, err
		}
		if filepath.Base(strings.TrimSuffix(exe, " (deleted)")) != "node" && filepath.Base(exe) != "nodejs" {
			return false, nil
		}
		actual, err := os.Stat(path)
		if err != nil {
			return false, err
		}
		matched := false
		for _, root := range roots {
			expected, e := os.Stat(filepath.Join(root, "bin/node"))
			if e == nil && os.SameFile(actual, expected) && exe == filepath.Join(root, "bin/node") {
				matched = true
			}
		}
		if !matched {
			return false, errors.New("PHP consumer has an unauthenticated Node.js child")
		}
		key, e := processKey(pid)
		if e != nil {
			return false, e
		}
		if u.PIDs[pid] != key {
			return false, errors.New("PHP Node.js child changed during inspection")
		}
		// With a live controller, prove ancestry as well as cgroup membership.
		if u.MainPID > 0 {
			cursor := pid
			seen := map[int]bool{}
			for cursor != u.MainPID {
				if cursor <= 1 || seen[cursor] || u.PIDs[cursor] == "" {
					return false, errors.New("Node.js process is not the PHP controller's descendant")
				}
				seen[cursor] = true
				stat, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", cursor))
				if e != nil {
					return false, e
				}
				end := strings.LastIndex(string(stat), ")")
				if end < 0 {
					return false, ErrChanged
				}
				fields := strings.Fields(string(stat)[end+1:])
				if len(fields) < 2 {
					return false, ErrChanged
				}
				cursor, e = strconv.Atoi(fields[1])
				if e != nil {
					return false, e
				}
			}
		}
		return true, nil
	}
}

func inspectPHPConsumers(ctx context.Context, roots []string, id string, node []Unit, original *Impact) ([]phpupdate.MaintenanceConsumer, []string, error) {
	exempt := map[int]bool{}
	for _, u := range node {
		exempt[u.ID] = true
	}
	set, reasons, err := phpupdate.InspectMaintenanceConsumers(ctx, exempt, phpupdate.MaintenanceInspection{Recovery: original != nil, Normalize: func(name string, p map[string]string) (map[string]string, error) {
		return normalizePHPBarrier(name, id, p)
	}, AdditionalProcess: phpNodeProcess(roots)})
	if err != nil {
		return set, reasons, err
	}
	known := map[string]bool{}
	for _, c := range set {
		for _, u := range c.Units {
			known[u.Runtime.Name] = true
		}
	}
	rows, e := database.DB.QueryContext(ctx, "SELECT unit,generation_id FROM node_update_php_barriers")
	if e != nil {
		return set, reasons, e
	}
	for rows.Next() {
		var name, owner string
		if e = rows.Scan(&name, &owner); e != nil {
			rows.Close()
			return set, reasons, e
		}
		if owner != id || id == "" || !known[name] {
			reasons = append(reasons, "Unknown or foreign Node-owned PHP consumer hold: "+name)
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return set, reasons, e
	}
	if original != nil && original.PHPPolicy > 0 {
		if !phpupdate.SameMaintenanceConsumers(set, original.PHP) {
			reasons = append(reasons, "PHP consumer configuration changed since the retained review")
			return set, reasons, nil
		}
		for i := range set {
			for j := range set[i].Units {
				set[i].Units[j].Runtime.Active = original.PHP[i].Units[j].Runtime.Active
			}
		}
	}
	return set, reasons, nil
}

func samePHPIntent(a, b []phpupdate.MaintenanceConsumer) bool {
	if !phpupdate.SameMaintenanceConsumers(a, b) {
		return false
	}
	for i := range a {
		for j := range a[i].Units {
			if a[i].Units[j].Runtime.Active != b[i].Units[j].Runtime.Active {
				return false
			}
		}
	}
	return true
}
func (m *Manager) preparePHPState(ctx context.Context, g *Generation) error {
	set := g.executionImpact().PHP
	if g.PHPState == nil {
		g.PHPState = &PHPState{Schema: 1, Original: set, Stops: map[string]phpupdate.MaintenanceStop{}, Resumes: map[string]phpupdate.MaintenanceResume{}}
	}
	s := g.PHPState
	if s.Schema != 1 || s.Stops == nil || s.Resumes == nil || !samePHPIntent(set, s.Original) {
		return errors.New("PHP consumer coordination history is missing or changed")
	}
	if err := phpupdate.ValidateMaintenanceResumes(s.Original, s.Resumes); err != nil {
		return err
	}
	previous := s.Attempt
	s.Attempt = map[string]phpupdate.MaintenanceRuntime{}
	for _, c := range set {
		for _, u := range c.Units {
			now, err := phpupdate.ObserveMaintenanceUnit(ctx, u.Runtime)
			if err != nil {
				return err
			}
			now.Natural = u.Runtime.Natural
			if err = phpupdate.ValidateMaintenanceProcesses(ctx, c, &now, phpNodeProcess([]string{g.Plan.Before.Root, filepath.Join(filepath.Dir(g.Plan.Before.Root), "v"+g.Plan.After.Version)}), true); err != nil {
				return err
			}
			if !u.Runtime.Active && (now.Active || len(now.PIDs) > 0) {
				return errors.New("A stopped PHP consumer started outside maintenance")
			}
			if stop := s.Stops[now.Name]; now.Octane != nil && stop.Delivery == "sent" && stop.Identity != now.PIDs[now.Octane.MasterPID] {
				s.Stops[now.Name] = phpupdate.MaintenanceStop{}
			}
			histories := []phpupdate.MaintenanceRuntime{previous[now.Name]}
			for _, original := range s.Original {
				for _, old := range original.Units {
					if old.Runtime.Name == now.Name {
						histories = append(histories, old.Runtime)
					}
				}
			}
			now, err = retainPHPDomain(now, histories...)
			if err != nil {
				return err
			}
			s.Attempt[now.Name] = now
		}
	}
	return saveGeneration(*g)
}

func (m *Manager) holdPHP(ctx context.Context, g *Generation) error {
	if err := holdPHPEvidence(ctx, g); err != nil {
		return err
	}
	for _, c := range g.executionImpact().PHP {
		for _, u := range c.Units {
			name := u.Runtime.Name
			if !phpUnitName.MatchString(name) {
				return ErrChanged
			}
			b, err := readPHPBarrier(name)
			if errors.Is(err, sql.ErrNoRows) {
				b = phpBarrier{Unit: name, Generation: g.ID, Original: u.Definition}
				raw, e := json.Marshal(b)
				if e != nil {
					return e
				}
				if _, err = database.DB.Exec("INSERT INTO node_update_php_barriers(unit,generation_id,payload) VALUES(?,?,?)", name, g.ID, string(raw)); err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if b.Generation != g.ID || digest(b.Original) != digest(u.Definition) {
				return ErrChanged
			}
			if b.Releasing {
				b.Releasing = false
				raw, _ := json.Marshal(b)
				if _, err = database.DB.Exec("UPDATE node_update_php_barriers SET payload=? WHERE unit=? AND generation_id=?", string(raw), name, g.ID); err != nil {
					return err
				}
			}
			path := barrierPath(phpBarrierUnit(name))
			if err = durableDirectory(filepath.Dir(path), 0755); err != nil {
				return err
			}
			if err = durableWrite(path, []byte(phpBarrierContent(name, g.ID)), 0644, true); err != nil {
				return err
			}
			if err = os.Remove(permitPath(phpBarrierUnit(name), g.ID)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	_, err := syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "daemon-reload")
	return err
}

func (m *Manager) verifyPHPHold(ctx context.Context, g *Generation, u phpupdate.MaintenanceUnit) error {
	b, e := readPHPBarrier(u.Runtime.Name)
	if e != nil {
		return e
	}
	if b.Generation != g.ID {
		return ErrChanged
	}
	if !b.Releasing {
		if _, e = os.Stat(barrierPath(phpBarrierUnit(b.Unit))); e != nil {
			return e
		}
	}

	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", u.Runtime.Name, "--property=Id,FragmentPath,DropInPaths,ExecStart,ExecStartPre,ExecStartPost,ExecStop,ExecStopPost,ExecReload,ExecSearchPath,PassEnvironment,User,Group,WorkingDirectory,Type,KillMode,KillSignal,SendSIGKILL,TimeoutStopUSec,Restart,Environment,EnvironmentFiles,RootDirectory,RootImage,NeedDaemonReload")
	if err != nil {
		return err
	}
	p := properties(out)
	if p["NeedDaemonReload"] != "no" {
		return errors.New("PHP maintenance hold is not loaded")
	}
	delete(p, "NeedDaemonReload")
	p, err = normalizePHPBarrier(u.Runtime.Name, g.ID, p)
	if err != nil {
		return err
	}
	for k, v := range p {
		if strings.HasPrefix(k, "Exec") {
			p[k], _, _ = strings.Cut(v, "; start_time=")
		}
	}
	if digest(p) != digest(u.Definition) {
		return errors.New("PHP consumer definition changed during Node.js maintenance")
	}
	return nil
}

func (m *Manager) drainPHP(ctx context.Context, g *Generation) error {
	for _, c := range g.executionImpact().PHP {
		for _, u := range c.Units {
			if err := m.verifyPHPHold(ctx, g, u); err != nil {
				return err
			}
			attempt, ok := g.PHPState.Attempt[u.Runtime.Name]
			if !ok || attempt.Natural != u.Runtime.Natural {
				return ErrChanged
			}
			// Active in the attempt describes observation, while the stop
			// protocol needs immutable original running/stopped intent. A
			// failed/draining original active unit remains eligible to drain.
			attempt.Active = u.Runtime.Active
			stop := g.PHPState.Stops[u.Runtime.Name]
			if err := phpupdate.StopMaintenanceConsumer(ctx, g.Head, attempt, &stop, func() error { g.PHPState.Stops[u.Runtime.Name] = stop; return saveGeneration(*g) }); err != nil {
				return err
			}
		}
	}
	deadline := time.NewTimer(5 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		ready := true
		for _, c := range g.executionImpact().PHP {
			for _, u := range c.Units {
				if err := m.verifyPHPHold(ctx, g, u); err != nil {
					return err
				}
				old := g.PHPState.Attempt[u.Runtime.Name]
				now, err := phpupdate.ObserveMaintenanceUnit(ctx, old)
				if err != nil {
					return err
				}
				retained, e := retainPHPDomain(now, old)
				if e != nil {
					return e
				}
				if digest(retained.PIDs) != digest(old.PIDs) {
					old.PIDs = retained.PIDs
					g.PHPState.Attempt[old.Name] = old
					if e = saveGeneration(*g); e != nil {
						return e
					}
				}
				drained, err := phpupdate.MaintenanceDrained(old, now)
				if err != nil {
					return err
				}
				ready = ready && drained
			}
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("PHP consumers are still draining. Custom Artisan commands receive no signal: let them finish or use their application's documented graceful exit, then review recovery. Runtime holds remain in place; no work was force-killed")
		case <-tick.C:
		}
	}
}

func (m *Manager) resumePHP(ctx context.Context, g *Generation) error {
	if err := holdPHPEvidence(ctx, g); err != nil {
		return err
	}
	for _, c := range g.PHPState.Original {
		for _, u := range c.Units {
			if err := m.verifyPHPHold(ctx, g, u); err != nil {
				return err
			}
			dispatch := func() error {
				unit := phpBarrierUnit(u.Runtime.Name)
				if err := resetFailedForStart(ctx, unit.Name); err != nil {
					return err
				}
				if err := durableDirectory(filepath.Dir(permitPath(unit, g.ID)), 0755); err != nil {
					return err
				}
				if err := durableWrite(permitPath(unit, g.ID), []byte(g.ID+"\n"), 0644, false); err != nil {
					return err
				}
				_, err := syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "start", "--no-block", unit.Name)
				return err
			}
			if u.Runtime.Natural {
				if err := phpupdate.ResumeMaintenanceNatural(ctx, g.PHPState.Evidence, g.PHPState.Resumes, u.Runtime, func(resumes map[string]phpupdate.MaintenanceResume) error {
					g.PHPState.Resumes = resumes
					return saveGeneration(*g)
				}, dispatch); err != nil {
					return err
				}
			} else if u.Runtime.Active {
				if err := dispatch(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (m *Manager) verifyPHP(ctx context.Context, g *Generation, targetRoot string) error {
	if err := phpupdate.ValidateMaintenancePHP(ctx, g.executionImpact().PHP); err != nil {
		return err
	}
	deadline := time.NewTimer(45 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	stable := map[string]time.Time{}
	identities := map[string]string{}
	for {
		ready := true
		for _, c := range g.PHPState.Original {
			for _, u := range c.Units {
				if err := m.verifyPHPHold(ctx, g, u); err != nil {
					return err
				}
				out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", u.Runtime.Name, "--property=UnitFileState")
				if err != nil {
					return err
				}
				if properties(out)["UnitFileState"] != u.Enabled {
					return errors.New("PHP consumer enablement changed")
				}
				now, err := phpupdate.ObserveMaintenanceUnit(ctx, u.Runtime)
				if err != nil {
					return err
				}
				if !u.Runtime.Active {
					drained, e := phpupdate.MaintenanceDrained(u.Runtime, now)
					if e != nil || !drained {
						return errors.New("A previously stopped PHP consumer was started")
					}
					continue
				}
				if u.Runtime.Natural {
					ok, completed, e := phpupdate.MaintenanceNaturalReady(ctx, g.PHPState.Evidence, g.PHPState.Resumes, u.Runtime, func(resumes map[string]phpupdate.MaintenanceResume) error {
						g.PHPState.Resumes = resumes
						return saveGeneration(*g)
					})
					if e != nil {
						return e
					}
					if !ok {
						ready = false
						continue
					}
					if completed {
						continue
					}
				}
				if !now.Active {
					ready = false
					continue
				}
				identity, err := phpupdate.MaintenanceHealthIdentity(ctx, c, now, phpNodeProcess([]string{targetRoot}))
				if errors.Is(err, phpupdate.ErrMaintenanceObservationChanged) {
					ready = false // Refresh the full observation; no pass on partial evidence.
					continue
				}
				if err != nil {
					return err
				}
				if identities[now.Name] != identity {
					identities[now.Name] = identity
					stable[now.Name] = time.Now()
				}
				if time.Since(stable[now.Name]) < 3*time.Second {
					ready = false
				}
			}
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("PHP consumers did not verify after the Node.js switch; review recovery without replaying uncertain Artisan work")
		case <-tick.C:
		}
	}
}

func (m *Manager) releasePHP(ctx context.Context, g *Generation) error {
	for _, c := range g.executionImpact().PHP {
		for _, u := range c.Units {
			b, err := readPHPBarrier(u.Runtime.Name)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if b.Generation != g.ID {
				return ErrChanged
			}
			if err = m.verifyPHPHold(ctx, g, u); err != nil {
				return err
			}
			b.Releasing = true
			raw, _ := json.Marshal(b)
			res, err := database.DB.Exec("UPDATE node_update_php_barriers SET payload=? WHERE unit=? AND generation_id=?", string(raw), b.Unit, g.ID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n != 1 {
				return ErrChanged
			}
			path := barrierPath(phpBarrierUnit(b.Unit))
			if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			if err = syncDir(filepath.Dir(path)); err != nil {
				return err
			}
			if _, err = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "daemon-reload"); err != nil {
				return err
			}
			// The journal remains until the unmodified effective contract is checked.
			if err = m.verifyPHPHold(ctx, g, u); err != nil {
				return err
			}
			if _, err = database.DB.Exec("DELETE FROM node_update_php_barriers WHERE unit=? AND generation_id=?", b.Unit, g.ID); err != nil {
				return err
			}
			if err = os.Remove(permitPath(phpBarrierUnit(b.Unit), g.ID)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return phpupdate.ReleaseMaintenanceEvidence(ctx, "node", g.ID, g.PHPState.Evidence, nil, func() error { return saveGeneration(*g) })
}

func holdPHPEvidence(ctx context.Context, g *Generation) error {
	if g.PHPState == nil {
		return errors.New("Missing PHP consumer coordination")
	}
	var units []phpupdate.MaintenanceRuntime
	for _, c := range g.PHPState.Original {
		for _, u := range c.Units {
			units = append(units, u.Runtime)
		}
	}
	return phpupdate.HoldMaintenanceEvidence(ctx, "node", g.ID, units, &g.PHPState.Evidence, func() error { return saveGeneration(*g) })
}

// A new cgroup observation cannot disprove the lifetime of a previously
// recorded descendant. Retain it until boot/start evidence proves it gone.
func retainPHPDomain(current phpupdate.MaintenanceRuntime, history ...phpupdate.MaintenanceRuntime) (phpupdate.MaintenanceRuntime, error) {
	merged := map[int]string{}
	for pid, key := range current.PIDs {
		merged[pid] = key
	}
	for _, old := range history {
		for pid, key := range old.PIDs {
			if merged[pid] != "" {
				continue
			}
			observed, err := processKey(pid)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return current, err
			}
			if observed == key {
				merged[pid] = key
			}
		}
	}
	current.PIDs = merged
	return current, nil
}
