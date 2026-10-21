package phpupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"fluxo/internal/services/octaneruntime"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

// Failed enablement is not a PHP package generation. Its separate root-owned
// journal keeps cleanup resumable without inventing a package repair operation.
type octaneCleanup struct {
	ID, Branch, Directory string
	DaemonID, Port        int
	Definition            map[string]string
	Content               string
	Snapshot              unitSnapshot
	Consumer              Consumer
	State, Delivery       string
}

func cleanupPaths(name string) (journal, override, marker string) {
	dir := filepath.Join("/etc/systemd/system", name+".d")
	return filepath.Join(dir, "90-fluxo-octane-cleanup.json"), filepath.Join(dir, "90-fluxo-octane-cleanup.conf"), filepath.Join(dir, "90-fluxo-octane-cleanup.hold")
}

func saveOctaneCleanup(path string, j octaneCleanup) error {
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return writeDurableExclusive(path, raw, 0600)
	}
	if err = rootOwned(path); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".fluxo-cleanup-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err == nil {
		err = syncDirectory(filepath.Dir(path))
	}
	return err
}

func verifyOctaneCleanupHold(ctx context.Context, j octaneCleanup) error {
	_, override, marker := cleanupPaths(j.Snapshot.Name)
	content, err := barrierContent(marker)
	if err != nil {
		return err
	}
	if err = verifyBarrierFile(marker, j.ID+"\n"); err != nil {
		return err
	}
	if err = verifyBarrierFile(override, content); err != nil {
		return err
	}
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", j.Snapshot.Name, "--property="+inspectedUnitProperties+",NeedDaemonReload")
	if err != nil {
		return err
	}
	p := unitProperties(out)
	if p["NeedDaemonReload"] != "no" || p["DropInPaths"] != override || p["Restart"] != "no" || p["KillMode"] != "mixed" || p["KillSignal"] != "15" || p["SendSIGKILL"] != "no" || p["TimeoutStopUSec"] != "infinity" {
		return errors.New("Failed Octane startup does not have effective cleanup stop protections")
	}
	delete(p, "NeedDaemonReload")
	for _, key := range []string{"DropInPaths", "Restart", "KillMode", "KillSignal", "SendSIGKILL", "TimeoutStopUSec"} {
		p[key] = j.Definition[key]
	}
	if canonicalDefinition(p) != canonicalDefinition(j.Definition) {
		return errors.New("Octane service changed during failed-enable cleanup")
	}
	return nil
}

// CleanupManagedOctane never sends TERM to an Octane Artisan controller. A
// proved native master receives one pidfd signal; all descendants must exit
// before disable/removal. Uncertain/unsupported startup remains held for an
// explicit retry or controlled operator shutdown, including across a reboot.
func CleanupManagedOctane(ctx context.Context, branch string, id int, directory string, port int) error {
	if !ValidBranch(branch) || id < 1 || !filepath.IsAbs(directory) {
		return errors.New("Invalid managed Octane cleanup identity")
	}
	name := unitName(id, 1)
	path := filepath.Join("/etc/systemd/system", name)
	journal, override, marker := cleanupPaths(name)
	var j octaneCleanup
	if info, err := os.Lstat(journal); err == nil {
		if !info.Mode().IsRegular() || info.Size() > 1024*1024 {
			return errors.New("Unrecognized Octane cleanup journal")
		}
		if err = rootOwned(journal); err != nil {
			return err
		}
		raw, err := os.ReadFile(journal)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &j); err != nil {
			return err
		}
		if _, err = uuid.Parse(j.ID); err != nil || j.Branch != branch || j.DaemonID != id || j.Port != port || j.Directory != directory || j.Snapshot.Name != name {
			return errors.New("Octane cleanup journal identity differs from this service")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else {
		// A startup transition or incoherent process read must not prevent the
		// verified definition from receiving its hold. Preserve any observed
		// identities now; require complete observation only after protection.
		u, observationErr := observeUnit(ctx, name, "")
		if _, err = os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			if observationErr != nil {
				return observationErr
			}
			if u.Active || len(u.PIDs) != 0 {
				return errors.New("Octane service definition is missing while processes remain")
			}
			// A prior completion may have unlinked its journal before its final
			// directory sync failed. Reconfirm absence durably on this retry.
			if info, e := os.Lstat(filepath.Dir(journal)); e == nil {
				if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					return errors.New("Unsafe completed Octane cleanup directory")
				}
				if err = rootOwned(filepath.Dir(journal)); err != nil {
					return err
				}
				for _, file := range []string{override, marker} {
					if _, e = os.Lstat(file); !errors.Is(e, os.ErrNotExist) {
						return errors.New("Octane cleanup protection exists without its journal; operator review required")
					}
				}
				if err = syncMaintenanceNamespace(filepath.Dir(journal)); err != nil {
					return err
				}
			} else if !errors.Is(e, os.ErrNotExist) {
				return e
			}
			if err = syncMaintenanceNamespace(filepath.Dir(path)); err != nil {
				return err
			}
			return nil
		}
		if err = rootOwned(path); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
			return errors.New("Unrecognized Octane service definition")
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "daemon-reload"); err != nil {
			return err
		}
		out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", name, "--property="+inspectedUnitProperties)
		if err != nil {
			return err
		}
		c := Consumer{ID: id, Kind: "laravel_octane", Name: "failed Octane enablement", Directory: directory, Port: port, Instances: 1, Command: octaneruntime.Command(branch, port)}
		definition := unitProperties(out)
		if err = validateUnitDefinition(branch, &c, definition); err != nil {
			return err
		}
		j = octaneCleanup{ID: uuid.NewString(), Branch: branch, DaemonID: id, Directory: directory, Port: port, Definition: definition, Content: string(content), Snapshot: u, Consumer: c, State: "preparing"}
		if err = ensureDurableManagedDropInDirectory(name); err != nil {
			return err
		}
		if err = saveOctaneCleanup(journal, j); err != nil {
			return err
		}
	}
	if err := ensureDurableManagedDropInDirectory(name); err != nil {
		return err
	}
	if j.State != "removing" {
		if err := verifyBarrierFile(path, j.Content); err != nil {
			return err
		}
		content, _ := barrierContent(marker)
		if err := ensureBarrierFile(marker, j.ID+"\n", 0600); err != nil {
			return err
		}
		if err := ensureBarrierFile(override, content, 0644); err != nil {
			return err
		}
		if _, err := syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "daemon-reload"); err != nil {
			return err
		}
		for {
			if err := verifyOctaneCleanupHold(ctx, j); err != nil {
				return err
			}
			u, err := observeUnit(ctx, name, j.Snapshot.CGroup)
			if err != nil {
				return err
			}
			drained, err := fullyDrained(j.Snapshot, u, processKey)
			if err != nil {
				return err
			}
			if drained {
				break
			}
			if j.Delivery == "" && u.Active {
				probe, probeErr := probeOctane(ctx, branch, j.Consumer, false)
				if probeErr == nil {
					j.Consumer.Octane = &probe.Descriptor
					u.Octane, probeErr = inspectOctaneProcesses(ctx, branch, j.Consumer, u)
				}
				if probeErr == nil {
					j.Snapshot = u
					j.Delivery = "intent"
					if err = saveOctaneCleanup(journal, j); err != nil {
						return err
					}
					if err = signalRecordedProcess(ctx, u, u.Octane.MasterPID, unix.SIGTERM); err != nil {
						return err
					}
					j.Delivery = "sent"
					if err = saveOctaneCleanup(journal, j); err != nil {
						return err
					}
				}
			} // An unconfirmed intent is observed, never repeated automatically.
			select {
			case <-ctx.Done():
				return fmt.Errorf("Octane cleanup is still held; complete a controlled native shutdown or retry after current work exits: %w", ctx.Err())
			case <-time.After(200 * time.Millisecond):
			}
		}
		j.State = "removing"
		if err := saveOctaneCleanup(journal, j); err != nil {
			return err
		}
	}
	u, err := observeUnit(ctx, name, j.Snapshot.CGroup)
	if err != nil {
		return err
	}
	if drained, err := fullyDrained(j.Snapshot, u, processKey); err != nil || !drained {
		return errors.New("Octane processes remain; service definition was retained")
	}
	if err = verifyBarrierFile(path, j.Content); err == nil {
		if _, err = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "disable", name); err != nil {
			return err
		}
		if err = os.Remove(path); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// ENOENT on retry is not evidence that an earlier unlink reached storage.
	if err = syncMaintenanceNamespace(filepath.Dir(path)); err != nil {
		return err
	}
	link := filepath.Join("/etc/systemd/system/multi-user.target.wants", name)
	if target, e := os.Readlink(link); e == nil {
		if target != path && filepath.Clean(filepath.Join(filepath.Dir(link), target)) != path {
			return errors.New("Octane enable link changed during cleanup")
		}
		if err = os.Remove(link); err != nil {
			return err
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if err = syncMaintenanceNamespace(filepath.Dir(link)); err != nil {
		return err
	}
	content, _ := barrierContent(marker)
	for _, f := range []struct{ path, content string }{{marker, j.ID + "\n"}, {override, content}} {
		if err = verifyBarrierFile(f.path, f.content); err == nil {
			err = os.Remove(f.path)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if _, err = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "daemon-reload"); err != nil {
		return err
	}
	u, err = observeUnit(ctx, name, j.Snapshot.CGroup)
	if err != nil || u.Active || len(u.PIDs) != 0 {
		return errors.New("Octane cleanup could not confirm final process exit")
	}
	if err = os.Remove(journal); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(journal))
}
