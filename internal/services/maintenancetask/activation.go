package maintenancetask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/scheduler"
	"github.com/google/uuid"
)

// PlanExecutionActivationLocked requires stopped-panel, exclusive lifecycle,
// package/native and both journal gates. It plans exact transitions, not writes.
func PlanExecutionActivationLocked(ctx context.Context, imageSHA, operation string) ([]executiondomain.ActivationFile, error) {
	return planExecutionActivationLocked(ctx, imageSHA, operation, nil)
}

// Installer-only conversion returns the exact retirement witnesses to archive.
func PlanInstallerHistoricalActivationLocked(ctx context.Context, imageSHA, operation string, retirements *[]scheduler.CronBackendRetirement) ([]executiondomain.ActivationFile, error) {
	return planExecutionActivationLocked(ctx, imageSHA, operation, retirements)
}

func planExecutionActivationLocked(ctx context.Context, imageSHA, operation string, retirements *[]scheduler.CronBackendRetirement) ([]executiondomain.ActivationFile, error) {

	a, err := absent()
	if err != nil {
		return nil, err
	}

	if a {
		return executionCreationPlan(operation), nil
	}
	if err = activationStateReady(); err != nil {
		return nil, err
	}
	j, err := read()
	if err != nil {
		return nil, err
	}
	if err = settled(ctx, &j); err != nil {
		return nil, err
	}
	for _, v := range j.Invocations {
		if v.Status == "submitted" || v.Status == "started" || v.Status == "failed" {
			return nil, errors.New("explicitly reconcile outstanding system-task execution before format activation")
		}
		if n, e := domainCount(v.Unit); e != nil || n != 0 {
			return nil, errors.New("system-task execution domain is not naturally empty")
		}
	}
	path := filepath.Join(stateRoot, "state.json")
	original, err := readRegular(path, 8<<20)
	if err != nil {
		return nil, err
	}
	// During the first core conversion, legacy receipt helpers can still read
	// this journal. Retain their exact canonical-encoding precondition. A task3
	// source already has a reader fence and may be the exact map-ordered output
	// of earlier MergeActivationJSON; compare its complete meaning without
	// changing any original bytes. Both gates stay held through the new fence.
	if err = validateActivationReceiptEncoding(original, j); err != nil {
		return nil, err
	}
	changes := []executiondomain.ActivationFile{}
	imagePath := filepath.Join(stateRoot, "helpers", imageSHA)
	imagePlanned := false
	for id, r := range j.Entries {
		if !r.Enabled {
			continue
		}
		if !r.Received || !r.Published {
			return nil, errors.New("wait for genuine current system-task loader acknowledgement before format activation")
		}
		if r.Database != "/var/lib/fluxo/fluxo.db" {
			return nil, errors.New("system-task registration belongs to another database; reconcile its scope before activation")
		}
		if err = verifyRegistrationFiles(r); err != nil {
			return nil, err
		}
		// The new exact trigger receives its own loader acknowledgement after
		// commit; old acknowledgement does not substitute for the new nonce.
		if !imagePlanned {
			if err = trustedDatabaseDirectory(filepath.Dir(imagePath)); err != nil {
				return nil, err
			}
			before := "absent"
			if data, e := readRegular(imagePath, 128<<20); e == nil {
				before = digest(data)
				if before != imageSHA {
					return nil, errors.New("activation helper image changed")
				}
			} else if !os.IsNotExist(e) {
				return nil, e
			}
			changes = append(changes, executiondomain.ActivationFile{Path: imagePath, Before: before, Image: true, Mode: 0700})
			imagePlanned = true
		}
		oldTrigger, e := readRegular(cronPath(id), 1<<20)
		if e != nil {
			return nil, e
		}
		r.RefreshFrom = &helperRefresh{Helper: r.Helper, Nonce: r.Nonce}
		r.Helper, r.Nonce, r.Received = imagePath, uuid.NewString(), false
		r.Boot, err = bootID()
		if err != nil {
			return nil, err
		}
		previousBackend := r.Backend
		r.Backend, err = scheduler.RequalifyCronBackend(ctx, previousBackend)
		if err != nil {
			return nil, err
		}
		if err = validateActivationBackendContinuity(j.Schema, previousBackend, r.Backend); err != nil {
			if retirements == nil || (j.Schema != 1 && j.Schema != 2) || previousBackend.Boot != r.Backend.Boot {
				return nil, err
			}
			if err = historicalInstallerInvocationsSettled(j); err != nil {
				return nil, err
			}
			proof := scheduler.CronBackendRetirement{Saved: previousBackend, Current: r.Backend}
			if err = scheduler.VerifyInstallerHistoricalRetirement(ctx, proof); err != nil {
				return nil, err
			}
			*retirements = append(*retirements, proof)
		}
		if r.Backend != previousBackend {
			if err = scheduler.VerifyCronBackendEmpty(ctx, r.Backend); err != nil {
				return nil, err
			}
		}
		// Publication is verified before the prepared floor can commit. Until
		// then every ordinary CLI refuses; afterward Received=false gates work.
		r.Published = true
		j.Entries[id] = r
		changes = append(changes, executiondomain.ActivationFile{Path: cronPath(id), Before: digest(oldTrigger), Data: []byte(cronContent(r)), Cron: true, Mode: 0644})
	}
	j.Schema = executiondomain.CurrentSystemTaskSchema
	for name, v := range j.Invocations {
		if v.Adapter == "" {
			v.Adapter = executiondomain.CgroupExitV1
		}
		j.Invocations[name] = v
	}
	converted, err := executiondomain.MergeActivationJSON(original, j)
	if err != nil {
		return nil, err
	}
	// Core authority is converted before trigger replacement. No new helper can
	// consume authority while the prepared floor blocks ordinary entrypoints.
	state := executiondomain.ActivationFile{Path: path, Before: digest(original), Data: converted, Mode: 0600}
	return append([]executiondomain.ActivationFile{state}, changes...), nil
}

// First conversion retains the old registration in its exact source archive
// and gives the replacement helper a new nonce. An earlier-boot identity can be
// renewed under the unchanged saved verifier/configuration; current-schema
// helper refresh and all same-boot drift still require explicit requalification.
func validateActivationBackendContinuity(sourceSchema int, saved, current scheduler.CronBackendIdentity) error {
	if err := scheduler.ValidateBackendIdentity(saved); err != nil {
		return err
	}
	if err := scheduler.ValidateBackendIdentity(current); err != nil {
		return err
	}
	if current == saved {
		return nil
	}
	if (sourceSchema != 1 && sourceSchema != 2) || current.Boot == saved.Boot || current.Backend != saved.Backend || current.Group != saved.Group || current.Configuration != saved.Configuration {
		return errors.New("backend identity changes require explicit maintenance registration requalification, not helper refresh")
	}
	return nil
}

// Reconstruct the sole permitted changes from archived original evidence.
// New nonce/backend observations are operation-bound; application intent and
// invocation snapshots may not be replaced by recovery metadata.
func ValidateExecutionActivationPlan(original, intended []byte, imageSHA string, changes []executiondomain.ActivationFile) error {
	if len(original) == 0 {
		expected, _ := json.Marshal(emptyExecutionJournal())
		if !bytes.Equal(expected, intended) {
			return errors.New("system-task absence creation contains unproven history or intent")
		}
		for _, change := range changes {
			if change.Cron {
				return errors.New("absence of a task journal cannot authorize cron trigger replacement")
			}
		}
		return nil
	}
	var old, next journal
	if err := json.Unmarshal(original, &old); err != nil {
		return err
	}
	if err := json.Unmarshal(intended, &next); err != nil {
		return err
	}
	if len(old.Entries) != len(next.Entries) || len(old.Invocations) != len(next.Invocations) {
		return errors.New("system-task activation changed retained identities")
	}
	expectedTriggers := map[string]string{}
	for id, r := range old.Entries {
		if !r.Enabled {
			continue
		}
		target, ok := next.Entries[id]
		if !ok {
			return errors.New("activation removed a scheduled registration")
		}
		if _, err := uuid.Parse(target.Nonce); err != nil || target.Nonce == r.Nonce {
			return errors.New("activation nonce is invalid or reused")
		}
		if _, err := uuid.Parse(target.Boot); err != nil {
			return err
		}
		if scheduler.ValidateBackendIdentity(target.Backend) != nil || target.Backend.Boot != target.Boot || target.Backend.Backend != r.Backend.Backend {
			return errors.New("unqualified activation cron identity")
		}
		r.RefreshFrom = &helperRefresh{Helper: r.Helper, Nonce: r.Nonce}
		r.Helper = filepath.Join(stateRoot, "helpers", imageSHA)
		r.Nonce, r.Boot, r.Backend = target.Nonce, target.Boot, target.Backend
		r.Received, r.Published = false, true
		old.Entries[id] = r
		expectedTriggers[cronPath(id)] = cronContent(r)
	}
	old.Schema = executiondomain.CurrentSystemTaskSchema
	for key, v := range old.Invocations {
		if v.Adapter == "" {
			v.Adapter = executiondomain.CgroupExitV1
		}
		old.Invocations[key] = v
	}
	expected, err := executiondomain.MergeActivationJSON(original, old)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected, intended) {
		return errors.New("system-task activation would change command, enabled intent, identity or history")
	}
	for _, change := range changes {
		if !change.Cron {
			continue
		}
		expected, ok := expectedTriggers[change.Path]
		if !ok || string(change.Data) != expected {
			return errors.New("activation trigger is not the exact refreshed registration")
		}
		delete(expectedTriggers, change.Path)
	}
	if len(expectedTriggers) != 0 {
		return errors.New("activation helper refresh is missing a registered trigger")
	}
	return nil
}

func VerifyExecutionActivationLocked(ctx context.Context, imageSHA string) error {
	a, err := absent()
	if err != nil {
		return err
	}
	if a {
		return errors.New("activation requires an explicit current task core even for originally absent state")
	}
	if err = activationStateReady(); err != nil {
		return err
	}
	j, err := read()
	if err != nil {
		return err
	}
	if j.Schema != executiondomain.CurrentSystemTaskSchema {
		return errors.New("system-task execution format was not converted")
	}
	for _, r := range j.Entries {
		if !r.Enabled {
			continue
		}
		if imageSHA != "" && r.Helper != filepath.Join(stateRoot, "helpers", imageSHA) {
			return errors.New("system-task helper was not refreshed to the compatible image")
		}
		if err = verifyRegistrationFiles(r); err != nil {
			return err
		}
		if err = verifyHelperReaderContracts(ctx, r.Helper); err != nil {
			return err
		}
		if r.RefreshFrom != nil {
			old, err := readRegular(r.RefreshFrom.Helper, 128<<20)
			if err != nil || digest(old) != filepath.Base(r.RefreshFrom.Helper) {
				return errors.New("historic helper image was not retained")
			}
		}
	}
	return nil
}

func activationStateReady() error {
	anchor, err := readRegular(stateRoot+".initialized", 32)
	if err != nil {
		return err
	}
	phase, err := readRegular(stateRoot+".lock", 32)
	if err != nil {
		return err
	}
	if string(anchor) != "1\n" || string(phase) != "" && string(phase) != readyGate {
		return errors.New("system-task initialization is incomplete")
	}
	return nil
}

func emptyExecutionJournal() journal {
	return journal{Schema: executiondomain.CurrentSystemTaskSchema, Entries: map[int]registration{}, Invocations: map[string]invocation{}}
}
func executionCreationPlan(operation string) []executiondomain.ActivationFile {
	raw, _ := json.Marshal(emptyExecutionJournal())
	return []executiondomain.ActivationFile{
		{Path: stateRoot + ".initialized", Before: "absent", Data: []byte("1\n"), Fence: []byte("activation:" + operation + "\n"), Mode: 0600},
		{Path: stateRoot, Before: "absent", Directory: true, Mode: 0700},
		{Path: filepath.Join(stateRoot, "helpers"), Before: "absent", Directory: true, Mode: 0700},
		{Path: stateRoot + ".lock", Before: "absent", Lock: true, Mode: 0600},
		{Path: filepath.Join(stateRoot, "state.json"), Before: "absent", Data: raw, Mode: 0600},
	}
}

// Ordering/whitespace in a current core carries no additional authority. Its
// exact original bytes remain the plan's Before checksum and archived source.
// Legacy first-conversion records retain the stricter old-writer byte fence.
func validateActivationReceiptEncoding(original []byte, j journal) error {
	if len(original) > 8<<20 {
		return errors.New("system-task receipt exceeds activation read budget")
	}
	canonical, err := json.Marshal(j)
	if err != nil {
		return err
	}
	if j.Schema < 3 {
		if !bytes.Equal(canonical, original) {
			return errors.New("legacy system-task receipt encoding is not replay-stable; preserve its history before first conversion")
		}
		return nil
	}
	before, err := normalizeActivationReceiptJSON(original)
	if err != nil {
		return err
	}
	after, err := normalizeActivationReceiptJSON(canonical)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, after) {
		return errors.New("current system-task receipt meaning changed or contains unknown fields; preserve its exact history")
	}
	return nil
}

// Token decoding retains exact numbers and rejects duplicate object keys at
// every depth before canonical comparison. Last-key-wins is not whitespace or
// ordering equivalence. The size/depth bounds apply before recursive parsing.
func normalizeActivationReceiptJSON(raw []byte) ([]byte, error) {
	if len(raw) > 8<<20 {
		return nil, errors.New("system-task receipt exceeds activation read budget")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value func(int) (any, error)
	value = func(depth int) (any, error) {
		if depth > 128 {
			return nil, errors.New("system-task receipt nesting exceeds activation budget")
		}
		token, err := d.Token()
		if err != nil {
			return nil, err
		}
		delimiter, compound := token.(json.Delim)
		if !compound {
			return token, nil
		}
		switch delimiter {
		case '{':
			object := map[string]any{}
			for d.More() {
				keyToken, e := d.Token()
				if e != nil {
					return nil, e
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, errors.New("invalid system-task receipt object key")
				}
				if _, duplicate := object[key]; duplicate {
					return nil, errors.New("duplicate system-task receipt object key")
				}
				child, e := value(depth + 1)
				if e != nil {
					return nil, e
				}
				object[key] = child
			}
			end, e := d.Token()
			if e != nil || end != json.Delim('}') {
				return nil, errors.New("invalid system-task receipt object boundary")
			}
			return object, nil
		case '[':
			array := []any{}
			for d.More() {
				child, e := value(depth + 1)
				if e != nil {
					return nil, e
				}
				array = append(array, child)
			}
			end, e := d.Token()
			if e != nil || end != json.Delim(']') {
				return nil, errors.New("invalid system-task receipt array boundary")
			}
			return array, nil
		default:
			return nil, errors.New("unexpected system-task receipt delimiter")
		}
	}
	decoded, err := value(0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errors.New("trailing system-task receipt data")
	}
	return json.Marshal(decoded)
}
