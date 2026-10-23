package phpupdate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const inspectedUnitProperties = "Id,FragmentPath,DropInPaths,ExecStart,ExecStartPre,ExecStartPost,ExecStop,ExecStopPost,ExecReload,ExecSearchPath,PassEnvironment,User,Group,WorkingDirectory,Type,KillMode,KillSignal,SendSIGKILL,TimeoutStopUSec,Restart,Environment,EnvironmentFiles,RootDirectory,RootImage"

var managedUnitPattern = regexp.MustCompile(`^fluxo-daemon-[1-9][0-9]*(?:-[1-9][0-9]*)?\.service$`)
var unitMarkerPathPattern = regexp.MustCompile(`^/[A-Za-z0-9_./-]+$`)

type maintenanceBarrier struct {
	Natural          bool              `json:"natural,omitempty"`
	Unit             string            `json:"unit"`
	GenerationID     string            `json:"generation_id"`
	Path             string            `json:"path"`
	Marker           string            `json:"marker"`
	Content          string            `json:"content"`
	Original         map[string]string `json:"original"`
	WasActive        bool              `json:"was_active"`
	State            string            `json:"state"`
	NativeSignalHead string            `json:"native_signal_head,omitempty"`
	NativeDelivery   string            `json:"native_delivery,omitempty"`
	NativeIdentity   string            `json:"native_identity,omitempty"`
}

func barrierContent(marker string) (string, error) {
	if !unitMarkerPathPattern.MatchString(marker) || filepath.Clean(marker) != marker {
		return "", errors.New("Unsupported maintenance marker path")
	}
	return "[Unit]\nConditionPathExists=!" + marker + "\n[Service]\nRestart=no\nKillMode=mixed\nKillSignal=SIGTERM\nSendSIGKILL=no\nTimeoutStopSec=infinity\n", nil
}

func syncDirectory(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

var syncMaintenanceNamespace = syncDirectory

// Syncing files inside a new drop-in directory does not persist the directory's
// own entry. Confirm its parent link on both initial preparation and retries.
func ensureDurableManagedDropInDirectory(name string) error {
	if !managedUnitPattern.MatchString(name) {
		return errors.New("Invalid managed service directory")
	}
	parent := "/etc/systemd/system"
	if err := rootOwned(parent); err != nil {
		return err
	}
	dir := filepath.Join(parent, name+".d")
	if err := os.Mkdir(dir, 0755); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Unsafe managed service drop-in directory")
	}
	if err = rootOwned(dir); err != nil {
		return err
	}
	return syncMaintenanceNamespace(parent)
}

// Link a fully written inode into place without overwriting any existing file.
// A process crash cannot leave a partially written effective systemd override.
func writeDurableExclusive(path string, data []byte, mode os.FileMode) error {
	if err := rootOwned(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".fluxo-maintenance-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Link(f.Name(), path); err != nil {
		return err
	}
	// Remove the temporary link before verification: managed files must not have
	// an unexpected second link that could later modify their contents.
	if err = os.Remove(f.Name()); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func verifyBarrierFile(path, content string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != int64(len(content)) {
		return errors.New("Unrecognized maintenance barrier file")
	}
	if err = rootOwned(path); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if string(data) != content {
		return errors.New("Maintenance barrier contents changed; operator attention required")
	}
	return nil
}

func ensureBarrierFile(path, content string, mode os.FileMode) error {
	err := verifyBarrierFile(path, content)
	if err == nil {
		// A previous publish may have succeeded before its final fsync failed.
		// Visible matching bytes alone do not prove persistence on a retry.
		f, e := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
		if e != nil {
			return e
		}
		e = f.Sync()
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		return syncMaintenanceNamespace(filepath.Dir(path))
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = writeDurableExclusive(path, []byte(content), mode); err != nil {
		return err
	}
	return verifyBarrierFile(path, content)
}

func getBarrier(name string) (maintenanceBarrier, error) {
	var b maintenanceBarrier
	var raw string
	err := database.DB.QueryRow("SELECT payload FROM php_update_barriers WHERE unit_name=?", name).Scan(&raw)
	if err != nil {
		return b, err
	}
	err = json.Unmarshal([]byte(raw), &b)
	if err != nil {
		return b, err
	}
	if b.Unit != name || !managedUnitPattern.MatchString(name) {
		return b, errors.New("Invalid managed service barrier")
	}
	if _, err = uuid.Parse(b.GenerationID); err != nil {
		return b, err
	}
	want := filepath.Join("/etc/systemd/system", name+".d", "90-fluxo-php-maintenance.conf")
	content, err := barrierContent(b.Marker)
	if err != nil || b.Path != want || b.Content != content || b.Marker != filepath.Join(filepath.Dir(want), "90-fluxo-php-"+b.GenerationID+".hold") {
		return b, errors.New("Invalid maintenance barrier paths or contents")
	}
	return b, nil
}

func saveBarrier(b maintenanceBarrier) error {
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	_, err = database.DB.Exec("UPDATE php_update_barriers SET payload=? WHERE unit_name=? AND generation_id=?", string(raw), b.Unit, b.GenerationID)
	return err
}

// Recognize only our journaled and byte-for-byte verified override. Everything
// else still goes through the original restrictive service contract.
func normalizeBarrierDefinition(name string, p map[string]string) (map[string]string, error) {
	b, err := getBarrier(name)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return nil, err
	}
	if (b.State == "preparing" || b.State == "releasing") && p["DropInPaths"] == "" && canonicalDefinition(p) == canonicalDefinition(b.Original) {
		return p, nil
	}
	if p["DropInPaths"] != b.Path {
		return nil, errors.New("Effective maintenance override differs from the recorded barrier")
	}
	err = verifyBarrierFile(b.Path, b.Content)
	if err != nil && !(b.State == "releasing" && errors.Is(err, os.ErrNotExist)) {
		return nil, err
	}
	expected := map[string]string{"Restart": "no", "KillMode": "mixed", "KillSignal": "15", "SendSIGKILL": "no", "TimeoutStopUSec": "infinity"}
	for key, want := range expected {
		got := p[key]
		if key == "KillSignal" && got == "SIGTERM" {
			got = "15"
		}
		if got != want {
			return nil, fmt.Errorf("Maintenance stop protection is not effective (%s)", key)
		}
	}
	copy := map[string]string{}
	for key, value := range p {
		copy[key] = value
	}
	for key := range expected {
		copy[key] = b.Original[key]
	}
	copy["DropInPaths"] = b.Original["DropInPaths"]
	return copy, nil
}

func canonicalDefinition(p map[string]string) string {
	keys := make([]string, 0, len(p))
	for key := range p {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var lines []string
	for _, key := range keys {
		value := p[key]
		if strings.HasPrefix(key, "Exec") {
			value, _, _ = strings.Cut(value, "; start_time=")
		}
		lines = append(lines, key+"="+value)
	}
	return strings.Join(lines, "\n")
}

func (m *Manager) prepareBarrier(ctx context.Context, g recoveryGeneration, u unitSnapshot) error {
	if !managedUnitPattern.MatchString(u.Name) {
		return errors.New("Only verified managed consumers may receive a maintenance barrier")
	}
	if _, err := uuid.Parse(g.ID); err != nil {
		return err
	}
	if err := ensureDurableManagedDropInDirectory(u.Name); err != nil {
		return err
	}
	desiredActive := u.Active
	for _, original := range g.Original.Units {
		if original.Name == u.Name {
			desiredActive = original.Active
		}
	}
	b, err := getBarrier(u.Name)
	if err == nil {
		if b.GenerationID != g.ID {
			return ErrBusy
		}
		if b.WasActive != desiredActive {
			return ErrPlanChanged
		}
		if b.Natural != u.Natural {
			return ErrPlanChanged
		}
		if b.State != "preparing" && b.State != "starting" && b.State != "started" && b.State != "releasing" {
			return verifyHeldBarrier(ctx, b)
		}
		if b.State != "preparing" {
			// A later rollback may need to hold the same generation again.
			if err = verifyEffectiveBarrier(ctx, b); err != nil {
				return err
			}
			b.State = "preparing"
			b.NativeSignalHead, b.NativeDelivery, b.NativeIdentity = "", "", ""
			if err = saveBarrier(b); err != nil {
				return err
			}
		}
	} else {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		out, e := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", u.Name, "--property="+inspectedUnitProperties)
		if e != nil {
			return e
		}
		original := unitProperties(out)
		if original["DropInPaths"] != "" {
			return errors.New("Unrecognized managed consumer override")
		}
		if _, e = uuid.Parse(g.ID); e != nil {
			return e
		}
		path := filepath.Join("/etc/systemd/system", u.Name+".d", "90-fluxo-php-maintenance.conf")
		marker := filepath.Join(filepath.Dir(path), "90-fluxo-php-"+g.ID+".hold")
		content, e := barrierContent(marker)
		if e != nil {
			return e
		}
		b = maintenanceBarrier{Natural: u.Natural, Unit: u.Name, GenerationID: g.ID, Path: path, Marker: marker, Content: content, Original: original, WasActive: desiredActive, State: "preparing"}
		if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
			return e
		}
		if e = rootOwned(filepath.Dir(path)); e != nil {
			return e
		}
		if _, e = os.Lstat(path); !errors.Is(e, os.ErrNotExist) {
			return errors.New("A maintenance override already exists without a matching journal")
		}
		raw, _ := json.Marshal(b)
		if _, e = database.DB.Exec("INSERT INTO php_update_barriers(unit_name,generation_id,payload) VALUES (?,?,?)", b.Unit, b.GenerationID, string(raw)); e != nil {
			return e
		}
	}
	// Resume each interrupted boundary. A row by itself never proves a hold.
	if err = ensureBarrierFile(b.Marker, b.GenerationID+"\n", 0600); err != nil {
		return err
	}
	if err = ensureBarrierFile(b.Path, b.Content, 0644); err != nil {
		return err
	}
	if _, err = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err = verifyHeldBarrier(ctx, b); err != nil {
		return err
	}
	b.State = "held"
	return saveBarrier(b)
}

func verifyEffectiveBarrier(ctx context.Context, b maintenanceBarrier) error {
	if err := verifyBarrierFile(b.Path, b.Content); err != nil {
		return err
	}
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", b.Unit, "--property="+inspectedUnitProperties+",NeedDaemonReload")
	if err != nil {
		return err
	}
	p := unitProperties(out)
	if p["NeedDaemonReload"] != "no" {
		return errors.New("Maintenance override has not been loaded by systemd")
	}
	delete(p, "NeedDaemonReload")
	normalized, err := normalizeBarrierDefinition(b.Unit, p)
	if err != nil {
		return err
	}
	if canonicalDefinition(normalized) != canonicalDefinition(b.Original) {
		return errors.New("Managed service definition changed during maintenance")
	}
	return nil
}

func verifyHeldBarrier(ctx context.Context, b maintenanceBarrier) error {
	if err := verifyBarrierFile(b.Marker, b.GenerationID+"\n"); err != nil {
		return err
	}
	return verifyEffectiveBarrier(ctx, b)
}

// This tests the process trees, never Redis queue depth. Producers remain free
// to enqueue jobs, which the restarted consumers will pick up after maintenance.
func (m *Manager) drainConsumers(ctx context.Context, g *recoveryGeneration) error {
	for _, u := range g.Attempt.Units {
		if !managedUnitPattern.MatchString(u.Name) {
			continue
		}
		if err := m.prepareBarrier(ctx, *g, u); err != nil {
			return err
		}
	}
	g.State = "draining"
	if err := saveGeneration(*g); err != nil {
		return err
	}
	if err := progress(g.Head, "draining", "Holding consumer restarts, draining supported services and waiting for custom Artisan commands to exit naturally"); err != nil {
		return err
	}
	for _, u := range g.Attempt.Units {
		if !managedUnitPattern.MatchString(u.Name) {
			continue
		}
		b, err := getBarrier(u.Name)
		if err != nil {
			return err
		}
		if err = verifyHeldBarrier(ctx, b); err != nil {
			return err
		}
		if b.Natural {
			// No stop job and no signal: arbitrary Artisan actions do not share
			// Queue Worker's shutdown semantics. Observe their full domain below.
			continue
		}
		if b.State == "held" || b.State == "stopping" {
			state, e := observeUnit(ctx, u.Name, u.CGroup)
			if e != nil {
				return e
			}
			if !u.Active && (state.Active || len(state.PIDs) > 0) {
				return errors.New("A previously stopped consumer was started during maintenance")
			}
			if u.Active && state.Active && !sameOriginalMain(u, state) {
				return errors.New("Consumer main process changed before graceful shutdown")
			}
			// Journal intent first. Repeating a systemd stop request is safe only
			// while the verified original main is still active; an existing stop
			// job is observed without signalling its children a second time.
			b.State = "stopping"
			if err = saveBarrier(b); err != nil {
				return err
			}
			if u.Active && state.Active && sameOriginalMain(u, state) {
				if u.Octane != nil {
					// Never send TERM to the Artisan Swoole controller: octane:stop
					// would use SIGKILL. Signal only its proved native serving master.
					if b.NativeDelivery != "sent" && b.NativeSignalHead != g.Head {
						if u.Octane.Backend == "roadrunner" && b.NativeSignalHead != "" {
							return errors.New("RoadRunner shutdown signal delivery is uncertain. Repair will not repeat a signal that could shorten an in-flight request's native deadline; complete a controlled native shutdown before resuming Repair")
						}
						b.NativeSignalHead = g.Head
						b.NativeDelivery = "intent"
						b.NativeIdentity = u.PIDs[u.Octane.MasterPID]
						if err = saveBarrier(b); err != nil {
							return err
						}
						if key, e := processKey(u.Octane.MasterPID); e == nil && key == u.PIDs[u.Octane.MasterPID] {
							if err = signalRecordedProcess(ctx, u, u.Octane.MasterPID, unix.SIGTERM); err != nil {
								return err
							}
							b.NativeDelivery = "sent"
							if err = saveBarrier(b); err != nil {
								return err
							}
						} else if !errors.Is(e, os.ErrNotExist) {
							return errors.New("Octane serving master changed before graceful stop")
						}
					}
				} else {
					if _, err = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "stop", "--no-block", u.Name); err != nil {
						return err
					}
				}
			}
		}
	}
	deadline := time.NewTimer(5 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		ready := true
		for _, u := range g.Attempt.Units {
			if !managedUnitPattern.MatchString(u.Name) {
				continue
			}
			b, e := getBarrier(u.Name)
			if e != nil {
				return e
			}
			if e = verifyHeldBarrier(ctx, b); e != nil {
				return e
			}
			current, e := observeUnit(ctx, u.Name, u.CGroup)
			if e != nil {
				return e
			}
			drained, e := fullyDrained(u, current, processKey)
			if e != nil {
				return e
			}
			if !drained {
				ready = false
				continue
			}
			if b.State != "drained" {
				b.State = "drained"
				if e = saveBarrier(b); e != nil {
					return e
				}
			}
		}
		if ready {
			g.State = "drained"
			return saveGeneration(*g)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Consumers are still draining. Custom Artisan commands receive no stop signal: let them finish or use their application's documented graceful shutdown, then review Repair. Keep their process entries. No PHP package changes were started and Fluxo did not force-stop work")
		case <-tick.C:
		}
	}
}

func (m *Manager) startConsumers(ctx context.Context, g *recoveryGeneration) error {
	for _, u := range g.Original.Units {
		if !managedUnitPattern.MatchString(u.Name) {
			continue
		}
		if u.Natural {
			if err := m.resumeNaturalConsumer(ctx, g, u); err != nil {
				return err
			}
			continue
		}
		b, err := getBarrier(u.Name)
		if errors.Is(err, sql.ErrNoRows) {
			current, e := observeUnit(ctx, u.Name, u.CGroup)
			if e != nil {
				return e
			}
			if !u.Active {
				if current.Active || len(current.PIDs) > 0 {
					return errors.New("A previously stopped consumer was started")
				}
				continue
			}
			if current.Active {
				continue
			}
			if e = m.prepareBarrier(ctx, *g, u); e != nil {
				return e
			}
			b, err = getBarrier(u.Name)
		}
		if err != nil {
			return err
		}
		if b.GenerationID != g.ID {
			return ErrPlanChanged
		}
		if err = verifyEffectiveBarrier(ctx, b); err != nil {
			return err
		}
		if !u.Active {
			current, e := observeUnit(ctx, u.Name, u.CGroup)
			if e != nil {
				return e
			}
			drained, e := fullyDrained(u, current, processKey)
			if e != nil {
				return e
			}
			if !drained {
				return errors.New("A previously stopped consumer did not preserve its stopped state")
			}
			continue
		}
		if b.State != "starting" && b.State != "started" {
			if err = verifyHeldBarrier(ctx, b); err != nil {
				return err
			}
			current, e := observeUnit(ctx, u.Name, u.CGroup)
			if e != nil {
				return e
			}
			drained, e := fullyDrained(u, current, processKey)
			if e != nil {
				return e
			}
			if !drained {
				return errors.New("Old consumer processes are still draining")
			}
			b.State = "starting"
			if err = saveBarrier(b); err != nil {
				return err
			}
		}
		if _, err = os.Lstat(b.Marker); err == nil {
			if err = verifyBarrierFile(b.Marker, b.GenerationID+"\n"); err != nil {
				return err
			}
			if err = os.Remove(b.Marker); err != nil {
				return err
			}
			if err = syncDirectory(filepath.Dir(b.Marker)); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		state, e := observeUnit(ctx, u.Name, u.CGroup)
		if e != nil {
			return e
		}
		if !state.Active {
			if len(state.PIDs) > 0 || state.State == "deactivating" {
				return errors.New("Consumer has not finished stopping")
			}
			if _, err = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "start", "--no-block", u.Name); err != nil {
				return err
			}
		}
		b.State = "started"
		if err = saveBarrier(b); err != nil {
			return err
		}
	}
	return nil
}

// Remove only exact, journaled files after all verification passed. A crash in
// cleanup remains recoverable; never delete an administrator's replacement.
func (m *Manager) releaseBarriers(ctx context.Context, g recoveryGeneration) error {
	for _, u := range g.Original.Units {
		if !managedUnitPattern.MatchString(u.Name) {
			continue
		}
		b, err := getBarrier(u.Name)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if b.GenerationID != g.ID {
			return ErrPlanChanged
		}
		if b.State != "releasing" {
			if err = verifyEffectiveBarrier(ctx, b); err != nil {
				return err
			}
			b.State = "releasing"
			if err = saveBarrier(b); err != nil {
				return err
			}
		}
		for _, file := range []struct{ path, content string }{{b.Marker, b.GenerationID + "\n"}, {b.Path, b.Content}} {
			err = verifyBarrierFile(file.path, file.content)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err == nil {
				if err = os.Remove(file.path); err != nil {
					return err
				}
			}
			if err = syncMaintenanceNamespace(filepath.Dir(file.path)); err != nil {
				return err
			}
		}
		if _, err = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "daemon-reload"); err != nil {
			return err
		}
		out, e := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", u.Name, "--property="+inspectedUnitProperties+",NeedDaemonReload")
		if e != nil {
			return e
		}
		p := unitProperties(out)
		if p["NeedDaemonReload"] != "no" {
			return errors.New("Service configuration has not been reloaded")
		}
		delete(p, "NeedDaemonReload")
		if canonicalDefinition(p) != canonicalDefinition(b.Original) {
			return errors.New("Service definition changed while releasing maintenance hold")
		}
		if _, err = database.DB.Exec("DELETE FROM php_update_barriers WHERE unit_name=? AND generation_id=?", u.Name, g.ID); err != nil {
			return err
		}
	}
	return nil
}
