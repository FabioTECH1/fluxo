package phpupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

// MaintenanceEvidence belongs to the caller's durable generation, not to a
// worker process. An active target with ONLY ordering references keeps systemd
// execution results loaded even when a disabled finite service exits and the
// worker dies or exits cleanly. It never pulls in, enables or restarts a service.
// https://www.freedesktop.org/software/systemd/man/255/systemd.unit.html#Unit%20Garbage%20Collection
type MaintenanceEvidence struct {
	Owner      string   `json:"owner"`
	Generation string   `json:"generation"`
	Units      []string `json:"units"`
	State      string   `json:"state"`
}

func evidenceContract(owner, generation string, units []MaintenanceRuntime) (*MaintenanceEvidence, error) {
	id, err := uuid.Parse(generation)
	if err != nil || id == uuid.Nil || id.String() != generation || (owner != "php" && owner != "node") {
		return nil, errors.New("Invalid consumer evidence owner")
	}
	e := &MaintenanceEvidence{Owner: owner, Generation: generation, State: "preparing"}
	seen := map[string]bool{}
	for _, u := range units {
		if !u.Natural {
			continue
		}
		if !managedUnitPattern.MatchString(u.Name) || seen[u.Name] {
			return nil, errors.New("Invalid or duplicate natural consumer evidence reference")
		}
		seen[u.Name] = true
		e.Units = append(e.Units, u.Name)
	}
	sort.Strings(e.Units)
	return e, nil
}

func (e MaintenanceEvidence) name() string {
	return "fluxo-" + e.Owner + "-consumer-evidence-" + e.Generation + ".target"
}
func (e MaintenanceEvidence) path() string { return filepath.Join("/etc/systemd/system", e.name()) }
func (e MaintenanceEvidence) content() string {
	return "[Unit]\nDescription=Fluxo consumer execution evidence\nDefaultDependencies=no\nAfter=" + strings.Join(e.Units, " ") + "\n"
}
func (e MaintenanceEvidence) validate() error {
	units := make([]MaintenanceRuntime, 0, len(e.Units))
	for _, name := range e.Units {
		units = append(units, MaintenanceRuntime{Name: name, Natural: true})
	}
	want, err := evidenceContract(e.Owner, e.Generation, units)
	if err != nil || len(e.Units) == 0 || !slices.Equal(e.Units, want.Units) {
		return errors.New("Invalid consumer evidence journal")
	}
	switch e.State {
	case "preparing", "held", "releasing", "released":
		return nil
	}
	return errors.New("Invalid consumer evidence state")
}

func (e MaintenanceEvidence) verifyFile() error {
	if err := verifyBarrierFile(e.path(), e.content()); err != nil {
		return err
	}
	info, err := os.Lstat(e.path())
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Nlink != 1 {
		return errors.New("Consumer evidence file has an unexpected link")
	}
	return nil
}

func (e MaintenanceEvidence) verifyLoaded(ctx context.Context, active bool) error {
	if err := e.verifyFile(); err != nil {
		return err
	}
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", e.name(), "--property=Id,LoadState,ActiveState,FragmentPath,DropInPaths,NeedDaemonReload,After,Before,Wants,Requires,Requisite,BindsTo,PartOf,Conflicts,OnFailure,OnSuccess,UnitFileState,DefaultDependencies,StopWhenUnneeded")
	if err != nil {
		return err
	}
	p := unitProperties(out)
	return e.verifyProperties(p, active)
}

func (e MaintenanceEvidence) verifyProperties(p map[string]string, active bool) error {
	if p["Id"] != e.name() || p["LoadState"] != "loaded" || p["FragmentPath"] != e.path() || p["DropInPaths"] != "" || p["NeedDaemonReload"] != "no" || p["UnitFileState"] != "static" || p["DefaultDependencies"] != "no" || p["StopWhenUnneeded"] != "no" {
		return errors.New("Consumer evidence reference differs from its owned contract")
	}
	if active && p["ActiveState"] != "active" {
		return errors.New("Consumer execution evidence is no longer retained")
	}
	after := strings.Fields(p["After"])
	sort.Strings(after)
	if !slices.Equal(after, e.Units) {
		return errors.New("Consumer evidence references changed")
	}
	for _, key := range []string{"Before", "Wants", "Requires", "Requisite", "BindsTo", "PartOf", "Conflicts", "OnFailure", "OnSuccess"} {
		if p[key] != "" {
			return fmt.Errorf("Consumer evidence has an unsupported dependency (%s)", key)
		}
	}
	return nil
}

// HoldMaintenanceEvidence is idempotent across goals and successor workers.
// The approved names and owner must match before any existing reference is used.
// Save intent before creating files; never replace an unjournaled reference.
func HoldMaintenanceEvidence(ctx context.Context, owner, generation string, units []MaintenanceRuntime, receipt **MaintenanceEvidence, save func() error) error {
	want, err := evidenceContract(owner, generation, units)
	if err != nil {
		return err
	}
	if *receipt != nil {
		e := *receipt
		if err = e.validate(); err != nil || e.Owner != owner || e.Generation != generation || !slices.Equal(e.Units, want.Units) {
			return errors.New("Consumer evidence history differs from approved intent")
		}
	} else {
		if len(want.Units) == 0 {
			return nil
		}
		if _, err = os.Lstat(want.path()); !errors.Is(err, os.ErrNotExist) {
			return errors.New("Unjournaled consumer evidence reference; operator review required")
		}
		*receipt = want
	}
	e := *receipt
	e.State = "preparing"
	if err = save(); err != nil {
		return err
	}
	if err = ensureBarrierFile(e.path(), e.content(), 0644); err != nil {
		return err
	}
	if _, err = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err = e.verifyLoaded(ctx, false); err != nil {
		return err
	}
	if _, err = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "start", e.name()); err != nil {
		return err
	}
	if err = e.verifyLoaded(ctx, true); err != nil {
		return err
	}
	e.State = "held"
	return save()
}

func verifyNaturalEvidence(ctx context.Context, e *MaintenanceEvidence, name string) error {
	if e == nil {
		return errors.New("Custom Artisan execution has no retained evidence reference")
	}
	if err := e.validate(); err != nil {
		return err
	}
	if e.State != "held" || !slices.Contains(e.Units, name) {
		return errors.New("Custom Artisan execution evidence is not held")
	}
	return e.verifyLoaded(ctx, true)
}

// Release only after the caller durably verified the generation. A crash during
// cleanup leaves an exact releasing receipt. Completed invocation records stay
// with the generation; absent results never authorize another dispatch.
func ReleaseMaintenanceEvidence(ctx context.Context, owner, generation string, e *MaintenanceEvidence, save func() error) error {
	if e == nil {
		return nil // Older generations may have no reference to release.
	}
	if err := e.validate(); err != nil {
		return err
	}
	if e.Owner != owner || e.Generation != generation {
		return errors.New("Cannot release another generation's consumer evidence")
	}
	if e.State != "releasing" && e.State != "released" {
		if err := e.verifyLoaded(ctx, true); err != nil {
			return err
		}
		e.State = "releasing"
		if err := save(); err != nil {
			return err
		}
	}
	if err := e.verifyFile(); err == nil {
		if err = e.verifyLoaded(ctx, false); err != nil {
			return err
		}
		if _, err = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "stop", e.name()); err != nil {
			return err
		}
		if err = os.Remove(e.path()); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := syncDirectory(filepath.Dir(e.path())); err != nil {
		return err
	}
	if _, err := syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "daemon-reload"); err != nil {
		return err
	}
	e.State = "released"
	return save()
}
