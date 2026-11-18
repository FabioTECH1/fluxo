package bridge

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fluxo/internal/services/executiondomain"
	"golang.org/x/sys/unix"
)

func validateActivationFilePlan(p executionActivation) error {
	seen := map[string]executiondomain.ActivationFile{}
	for _, f := range p.Files {
		if _, duplicate := seen[f.Path]; duplicate || filepath.Clean(f.Path) != f.Path || f.Before != "absent" && len(f.Before) != 64 {
			return errors.New("unrecognized/duplicate activation transition")
		}
		seen[f.Path] = f
		if f.Directory {
			if f.Before != "absent" || f.Mode != 0700 || f.Image || f.Lock || f.Cron || len(f.Data) != 0 || len(f.Fence) != 0 || (f.Path != "/var/lib/fluxo-scheduler" && !((p.Format == 3 || p.Format == 4) && f.Path == "/var/lib/fluxo-scheduler/helpers") && f.Path != "/var/lib/fluxo-system-tasks" && f.Path != "/var/lib/fluxo-system-tasks/helpers") {
				return errors.New("unrecognized activation directory creation")
			}
			continue
		}
		if f.Lock {
			if f.Before != "absent" || f.Mode != 0600 || f.Image || f.Cron || len(f.Data) != 0 || len(f.Fence) != 0 || (f.Path != "/var/lib/fluxo-scheduler/gate.lock" && f.Path != "/var/lib/fluxo-system-tasks.lock") {
				return errors.New("unrecognized activation coordination gate creation")
			}
			continue
		}
		if len(f.Fence) > 0 {
			if f.Before != "absent" || f.Mode != 0600 || f.Image || f.Cron || !bytes.Equal(f.Data, []byte("1\n")) || !bytes.Equal(f.Fence, []byte("activation:"+p.ID+"\n")) || (f.Path != "/var/lib/fluxo-scheduler.initialized" && f.Path != "/var/lib/fluxo-system-tasks.initialized") {
				return errors.New("unrecognized operation-bound initialization fence")
			}
			continue
		}
		if f.Image {
			allowed := (p.Format == 3 || p.Format == 4) && f.Path == "/var/lib/fluxo-scheduler/helpers/"+p.BinarySHA256 || f.Path == "/var/lib/fluxo-scheduler/dispatcher" || f.Path == "/var/lib/fluxo-system-tasks/helpers/"+p.BinarySHA256
			if !allowed || f.Mode != 0700 || f.Cron || len(f.Data) != 0 || f.Path != "/var/lib/fluxo-scheduler/dispatcher" && f.Before != "absent" && f.Before != p.BinarySHA256 {
				return errors.New("image escaped exact executing-helper scope")
			}
			continue
		}
		if f.Cron {
			id, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(f.Path), "fluxo-cron-"))
			if err != nil || id <= 0 || filepath.Dir(f.Path) != "/etc/cron.d" || filepath.Base(f.Path) != "fluxo-cron-"+strconv.Itoa(id) || f.Mode != 0644 || f.Before == "absent" || f.CronMTime <= 0 {
				return errors.New("invalid exact registered trigger transition")
			}
			continue
		}
		if (f.Path != "/var/lib/fluxo-scheduler/state.json" && f.Path != "/var/lib/fluxo-system-tasks/state.json") || f.Mode != 0600 {
			return errors.New("activation target is outside owned journal scope")
		}
	}

	if p.Kind == activationTaskHelperRefresh || taskBackendTransitionKind(p.Kind) {
		state, ok := seen["/var/lib/fluxo-system-tasks/state.json"]
		if !ok || state.Before == "absent" || len(state.Data) == 0 || p.Auxiliary["/var/lib/fluxo-system-tasks.initialized"] == "absent" {
			return errors.New("task helper refresh requires its existing current task core and anchor")
		}
		for _, change := range p.Files {
			if change.Directory || change.Lock || len(change.Fence) != 0 || change.Before == "absent" && !change.Image {
				return errors.New("task helper refresh cannot initialize execution infrastructure")
			}
			if change.Image {
				if taskBackendTransitionKind(p.Kind) {
					return errors.New("task backend requalification cannot refresh helpers")
				}
				if change.Path != "/var/lib/fluxo-system-tasks/helpers/"+p.BinarySHA256 {
					return errors.New("task helper refresh image escaped task-only scope")
				}
			} else if !change.Cron && change.Path != "/var/lib/fluxo-system-tasks/state.json" {
				return errors.New("task helper refresh cannot change Scheduler state")
			}
		}
		return nil
	}
	if p.Format == 3 || p.Format == 4 {
		if f, ok := seen["/var/lib/fluxo-scheduler/helpers/"+p.BinarySHA256]; !ok || !f.Image {
			return errors.New("schema5 activation lacks its exact immutable bootstrap image")
		}
	}
	for _, base := range []string{"/var/lib/fluxo-scheduler", "/var/lib/fluxo-system-tasks"} {
		state, ok := seen[base+"/state.json"]
		if !ok {
			return errors.New("activation must explicitly fence both current execution cores")
		}
		anchor, hasAnchor := seen[base+".initialized"]
		directory, hasDirectory := seen[base]
		if state.Before == "absent" {
			if !hasAnchor || len(anchor.Fence) == 0 || !hasDirectory || !directory.Directory || p.Auxiliary[base+".initialized"] != "absent" {
				return errors.New("current empty core lacks archived original absence and marker/directory creation authority")
			}
			gate := base + ".lock"
			if base == "/var/lib/fluxo-scheduler" {
				gate = base + "/gate.lock"
				if f, ok := seen[base+"/dispatcher"]; !ok || !f.Image || f.Before != "absent" {
					return errors.New("new Scheduler core lacks its exact compatible dispatcher")
				}
			} else if f, ok := seen[base+"/helpers"]; !ok || !f.Directory {
				return errors.New("new task core lacks its protected helper namespace")
			}
			if f, ok := seen[gate]; !ok || !f.Lock {
				return errors.New("new execution core lacks its explicit stable coordination gate")
			}

		} else {
			if hasAnchor || hasDirectory {
				return errors.New("existing history cannot be reinterpreted as clean absence")
			}
			if p.Auxiliary[base+".initialized"] == "absent" {
				return errors.New("existing core lost its original initialization evidence")
			}
			for _, f := range p.Files {
				if (p.Format == 3 || p.Format == 4) && base == "/var/lib/fluxo-scheduler" && f.Directory && f.Path == base+"/helpers" {
					continue
				}
				if (f.Directory || f.Lock || len(f.Fence) > 0) && (f.Path == base || strings.HasPrefix(f.Path, base+"/") || f.Path == base+".lock") {
					return errors.New("existing core cannot authorize unrelated clean-absence infrastructure")
				}
			}
		}
	}
	return nil
}

func activationAuxiliaryMatches(p executionActivation, current map[string]string) bool {
	if len(p.Auxiliary) != len(current) {
		return false
	}
	for path, original := range p.Auxiliary {
		if current[path] == original {
			continue
		}
		allowed := false
		for _, f := range p.Files {
			if f.Path == path && len(f.Fence) > 0 && original == "absent" && (current[path] == digest(f.Fence) || current[path] == digest(f.Data)) {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	return true
}
func validateActivationTargets(p executionActivation, imageSHA string) error {
	allowed := map[string]bool{}
	for _, f := range p.Files {
		allowed[f.Path] = true
		if err := validateActivationCurrent(f, imageSHA); err != nil {
			return err
		}
	}
	for _, f := range p.Files {
		if !f.Directory {
			continue
		}
		entries, err := os.ReadDir(f.Path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			path := filepath.Join(f.Path, entry.Name())
			if allowed[path] {
				continue
			}
			if strings.HasPrefix(entry.Name(), ".execution-activation-") {
				if _, err := readProtected(path, 256<<20); err == nil {
					continue
				}
			}
			return fmt.Errorf("unplanned artifact in activation-created namespace: %s", path)
		}
	}
	return nil
}

// Fully sync a temporary marker, then publish with no-replace atomically. No
// historical initializer can win a race and have its marker overwritten.
func writeActivationFence(path string, data []byte) error {
	if err := trustedDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".execution-activation-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
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
	if err = unix.Renameat2(unix.AT_FDCWD, f.Name(), unix.AT_FDCWD, path, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	return syncActivationDirectory(filepath.Dir(path))
}
