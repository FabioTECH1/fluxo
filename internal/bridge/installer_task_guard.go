package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const taskInitAnchor = "/var/lib/fluxo-system-tasks.initialized"
const taskGuardPrefix = "fluxo-installer-task-guard-v1 "

func taskGuardBytes(operation, plan, pending string) []byte {
	return []byte(taskGuardPrefix + operation + " " + plan + " " + pending + "\n")
}

// The old reader checks this anchor with a 32-byte budget, including once
// inside its flock. One atomic rename both denies every legacy gate entrant
// and publishes the exact staged operation identity needed after a crash.
func installTaskGuard(path, operation, plan, pending string) error {
	before, err := readProtected(path, 32)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, []byte("1\n")) {
		return errors.New("historical task initialization anchor changed")
	}
	return writeActivationFile(path, taskGuardBytes(operation, plan, pending), 0600)
}

// Recover the regular installer pointer if death occurred between its two
// publications. The already existing pending intent supplies the image,
// installation and service. Never discover an operation by scanning stages.
func readInstallerTaskGuard(intent *installerExecutionIntent) (*installerExecutionIntent, bool, error) {
	if intent == nil || intent.Phase != "pending" || intent.Operation != "" || intent.TaskCore != "" {
		return intent, false, nil
	}
	raw, err := readProtected(taskInitAnchor, 1024)
	if os.IsNotExist(err) || err == nil && bytes.Equal(raw, []byte("1\n")) {
		return intent, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	next, err := decodeTaskGuard(raw, *intent)
	if err != nil {
		return nil, false, err
	}
	return &next, true, nil
}

func guardedInstallerIntent(dataDir string, intent *installerExecutionIntent) (*installerExecutionIntent, bool, error) {
	guarded, found, err := readInstallerTaskGuard(intent)
	if err != nil || !found {
		return guarded, found, err
	}
	next := *guarded
	// Neither archive publication nor a floor write can precede the regular
	// installer pointer. Never reconstruct that pointer over later evidence.
	for _, name := range []string{"execution-activations", "execution-readers.json"} {
		if _, e := os.Lstat(filepath.Join(dataDir, name)); !os.IsNotExist(e) {
			return nil, false, errors.New("guard-only interruption has conflicting reader authority")
		}
	}
	stage := installerStagedArchive(dataDir, next.Operation)
	staged, err := inspectInstallerRecoveryArchive(dataDir, next)
	if err != nil {
		return nil, false, err
	}
	if !staged {
		return nil, false, errors.New("historical guard without installer pointer requires its staged archive")
	}
	plan, err := readProtected(filepath.Join(stage, "plan.json"), 128<<20)
	if err != nil {
		return nil, false, err
	}
	var p executionActivation
	if err = json.Unmarshal(plan, &p); err != nil || digest(plan) != next.Plan || p.InstallerTaskGuard == "" {
		return nil, false, errors.New("historical guard differs from its staged conversion")
	}
	for i, f := range p.Files {
		if f.Before == "absent" {
			continue
		}
		sha, e := activationProtectedHash(filepath.Join(stage, fmt.Sprintf("before-%d", i)), 256<<20)
		if e != nil || sha != f.Before {
			return nil, false, errors.New("historical guard source backup changed")
		}
	}
	return &next, true, nil
}

func decodeTaskGuard(raw []byte, intent installerExecutionIntent) (installerExecutionIntent, error) {
	fields := strings.Fields(string(raw))
	pending, err := json.Marshal(intent)
	if err != nil {
		return intent, err
	}
	if len(fields) != 4 || fields[3] != digest(pending) || !validSHA256(fields[2]) {
		return intent, errors.New("invalid historical task guard")
	}
	id, err := uuid.Parse(fields[1])
	if err != nil || id.String() != fields[1] || !bytes.Equal(raw, taskGuardBytes(fields[1], fields[2], fields[3])) {
		return intent, errors.New("invalid historical task guard identity")
	}
	intent.Operation, intent.Plan = fields[1], fields[2]
	return intent, nil
}

// Original anchor bytes become admissible again only once the exact intended
// task core is durable. At that point every schema1 helper rejects the core.
func guardedAuxiliary(plan executionActivation, planHash string, aux map[string]string) error {
	if plan.InstallerTaskGuard == "" {
		return nil
	}
	want := digest(taskGuardBytes(plan.ID, planHash, plan.InstallerTaskGuard))
	if aux[taskInitAnchor] == want {
		aux[taskInitAnchor] = digest([]byte("1\n"))
		return nil
	}
	if aux[taskInitAnchor] == digest([]byte("1\n")) {
		return verifyGuardedTaskCore(plan)
	}
	return errors.New("installer task guard lost before its exact current core")
}

func releaseInstallerTaskGuard(plan executionActivation, planHash string) error {
	if plan.InstallerTaskGuard == "" {
		return nil
	}
	raw, err := readProtected(taskInitAnchor, 1024)
	if err != nil {
		return err
	}
	restored := bytes.Equal(raw, []byte("1\n"))
	if !restored && !bytes.Equal(raw, taskGuardBytes(plan.ID, planHash, plan.InstallerTaskGuard)) {
		return errors.New("historical task guard changed before floor")
	}
	if err := verifyGuardedTaskCore(plan); err != nil {
		return err
	}
	if restored {
		return nil
	}
	return writeActivationFile(taskInitAnchor, []byte("1\n"), 0600)
}

func verifyGuardedTaskCore(plan executionActivation) error {
	for _, f := range plan.Files {
		if f.Path == "/var/lib/fluxo-system-tasks/state.json" {
			return validateActivationIntended(f, plan.BinarySHA256)
		}
	}
	return errors.New("historical task guard requires its converted core")
}
