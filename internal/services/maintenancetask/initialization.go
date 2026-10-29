package maintenancetask

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const initializingGate = "initializing\n"
const readyGate = "ready\n"

func writeGatePhase(f *os.File, phase string) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.WriteAt([]byte(phase), 0); err != nil {
		return err
	}
	return f.Sync()
}

// The parent lock's durable initialization intent is written before mkdir or
// any journal publication. Only that authenticated phase can finish a partial
// empty initialization. Missing historical evidence is never inferred empty.
func finishInitialization(f *os.File, initialize bool, anchorErr, stateErr error) error {
	if anchorErr != nil && !os.IsNotExist(anchorErr) {
		return anchorErr
	}
	if stateErr != nil && !os.IsNotExist(stateErr) {
		return stateErr
	}
	raw := make([]byte, 32)
	n, err := f.ReadAt(raw, 0)
	if err != nil && err != io.EOF {
		return err
	}
	phase := string(raw[:n])
	if phase != "" && phase != initializingGate && phase != readyGate {
		return errors.New("Invalid system-task initialization phase")
	}
	if phase == "" && os.IsNotExist(anchorErr) && os.IsNotExist(stateErr) && initialize {
		if _, e := os.Lstat(stateRoot); !os.IsNotExist(e) {
			return errors.New("Unproven partial system-task initialization; preserve and restore its evidence")
		}
		if err = writeGatePhase(f, initializingGate); err != nil {
			return err
		}
		if err = syncDir(filepath.Dir(stateRoot)); err != nil {
			return err
		}
		phase = initializingGate
	}
	if phase == initializingGate {
		if !initialize {
			return errors.New("System-task initialization was interrupted; run explicit reconciliation")
		}
		if err = os.Mkdir(stateRoot, 0700); err != nil && !os.IsExist(err) {
			return err
		}
		if err = trustedDatabaseDirectory(stateRoot); err != nil {
			return err
		}
		empty := journal{1, map[int]registration{}, map[string]invocation{}}
		canonical, _ := json.Marshal(empty)
		entries, e := os.ReadDir(stateRoot)
		if e != nil {
			return e
		}
		for _, entry := range entries {
			path := filepath.Join(stateRoot, entry.Name())
			if entry.Name() == "state.json" {
				b, e := readRegular(path, 4096)
				if e != nil {
					return e
				}
				if !bytes.Equal(b, canonical) {
					return errors.New("Initialization intent cannot replace nonempty or changed system-task history")
				}
				continue
			}
			if !strings.HasPrefix(entry.Name(), ".task-") {
				return errors.New("Unexpected artifact in interrupted system-task initialization")
			}
			if _, e = readRegular(path, 4096); e != nil {
				return e
			} // retain incomplete atomic-write evidence
		}
		if e = save(empty); e != nil {
			return e
		}
		if e = atomicFile(stateRoot+".initialized", []byte("1\n"), 0600); e != nil {
			return e
		}
		return writeGatePhase(f, readyGate)
	}
	if anchorErr != nil || stateErr != nil {
		return errors.New("System-task history or initialization proof is missing; preserve and restore its evidence")
	}
	// Empty lock bytes are accepted only with an existing initialized journal
	// from earlier compatible builds. Reads never rewrite their marker/history.
	return nil
}
