package maintenancetask

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Retire removes launch authority before the trigger. A cached dispatcher can
// no longer submit work, and an already admitted invocation finishes naturally.
func Retire(ctx context.Context, id int) error {
	unlock, err := gate(ctx, false)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	r, ok := j.Entries[id]
	if !ok {
		return errors.New("Scheduled-job registration is missing")
	}
	if r.Enabled {
		// Revocation must remain possible after row/layout drift. Authenticate
		// the exact published trigger/helper and legacy cutover, not permission
		// to execute the changed database command. Admitted work stays journaled.
		if err = verifyRegistrationFiles(r); err != nil {
			return err
		}
		if err = readyBackend(ctx, r); err != nil {
			return err
		}
		r.Enabled = false
		j.Entries[id] = r
		if err = save(j); err != nil {
			return err
		}
	}
	b, err := readRegular(cronPath(id), 1<<20)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if string(b) != cronContent(r) {
		return errors.New("Retired scheduled-job trigger changed")
	}
	if err = os.Remove(cronPath(id)); err != nil {
		return err
	}
	return syncDir(filepath.Dir(cronPath(id)))
}

func DefaultRetired(kind Kind) (bool, error) {
	a, err := absent()
	if err != nil || a {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5e9)
	defer cancel()
	unlock, err := gate(ctx, false)
	if err != nil {
		return false, err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return false, err
	}
	for _, r := range j.Entries {
		if r.Contract.Kind == kind && !r.Enabled {
			return true, nil
		}
	}
	return false, nil
}

// compact revokes only proven terminal/empty authorities. Complete immutable
// receipts remain on disk before their live entries are removed. Missing live
// authority always rejects a delayed Execute, including across reboot.
func compact(ctx context.Context, j *journal) error {
	for name, v := range j.Invocations {
		if v.Status != "completed" && v.Status != "blocked" && v.Status != "interrupted" {
			continue
		}
		p, err := inspectInvocation(ctx, v)
		if err != nil {
			return err
		}
		n, err := domainCount(name)
		if err != nil {
			return err
		}
		if n != 0 || p["LoadState"] != "not-found" && (p["MainPID"] != "0" || p["ControlPID"] != "0" || p["ActiveState"] != "inactive" && p["ActiveState"] != "failed") {
			continue
		}
		dir := filepath.Join(stateRoot, "history")
		if err = os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
			return err
		}
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, name+".json")
		if old, e := readRegular(path, 8<<20); e == nil {
			if string(old) != string(b) {
				return errors.New("Scheduled-job history receipt changed")
			}
		} else if os.IsNotExist(e) {
			if err = atomicFile(path, b, 0600); err != nil {
				return err
			}
		} else {
			return e
		}
		delete(j.Invocations, name)
	}
	return save(*j)
}

// retireAbsentSubmissions is deliberately distinct from completion. No effect
// authority has been consumed; revoking it makes any delayed service harmless.
func retireAbsentSubmissions(ctx context.Context, j *journal) error {
	changed := false
	for name, v := range j.Invocations {
		if v.Status != "submitted" {
			continue
		}
		p, err := inspectInvocation(ctx, v)
		if err != nil {
			return err
		}
		n, err := domainCount(name)
		if err != nil {
			return err
		}
		if p["LoadState"] == "not-found" && n == 0 {
			v.Status = "blocked"
			v.Error = "Unconfirmed unstarted submission retired; it was not executed or replayed"
			j.Invocations[name] = v
			changed = true
		}
	}
	if changed {
		return save(*j)
	}
	return nil
}
