package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"fluxo/internal/services/executiondomain"
)

// PlanExecutionActivationLocked performs no writes. The bridge owns exclusive
// lifecycle/package/native admission, both journal gates and a stopped panel.
func PlanExecutionActivationLocked(ctx context.Context, operation string) ([]executiondomain.ActivationFile, error) {
	return planExecutionActivationLocked(ctx, operation, false)
}

// Explicit migration planning retains historical authority and does not infer
// readiness from conversion. The caller archives and fences this exact intent.
func PlanHistoricalExecutionActivationLocked(ctx context.Context, operation string) ([]executiondomain.ActivationFile, error) {
	return planExecutionActivationLocked(ctx, operation, true)
}

func planExecutionActivationLocked(ctx context.Context, operation string, historical bool) ([]executiondomain.ActivationFile, error) {
	if absent() {
		if err := validateLegacyLoaderAbsence(); err != nil {
			return nil, err
		}
		if historical {
			return nil, errors.New("historical retirement requires existing Scheduler authority")
		}
		return executionCreationPlan(operation), nil
	}
	if !historical {
		if err := BridgeReady(ctx, databasePath); err != nil {
			return nil, err
		}
	}
	if err := activationStateReady(); err != nil {
		return nil, err
	}
	j, err := read()
	if err != nil {
		return nil, err
	}
	if j.Schema < 5 {
		if err = validateLegacyLoaderAbsence(); err != nil {
			return nil, err
		}
	}
	if historical {
		if err = historicalActivationReady(ctx, j); err != nil {
			return nil, err
		}
	}
	if j.Upgrade != nil || len(j.Holds) != 0 {
		return nil, errors.New("finish Scheduler installer/maintenance holds before execution format activation")
	}
	for _, v := range j.Invocations {
		if err = invocationQuiescent(ctx, v); err != nil {
			return nil, err
		}
	}
	for _, e := range j.Entries {
		if e.Enabled {
			if e.Database != databasePath {
				return nil, errors.New("Scheduler registration belongs to another database; preserve and reconcile its scope before activation")
			}
			if !historical && !e.Ready {
				return nil, errors.New("finish Scheduler migration before execution format activation")
			}
			if err = bridgeTrigger(e); err != nil {
				return nil, err
			}
		}
	}
	path := filepath.Join(stateDir, "state.json")
	original, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	j.Schema = executiondomain.CurrentSchedulerSchema
	if historical {
		j.HistoricalRetirement = &historicalRetirement{Contract: executiondomain.SchedulerHistoricalRetirementContract, Operation: operation, SourceSHA256: hashBytes(original), Phase: "pending"}
	}
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
	if historical {
		err = ValidateHistoricalExecutionActivationPlan(original, converted, operation)
	} else {
		err = ValidateExecutionActivationPlan(original, converted)
	}
	if err != nil {
		return nil, err
	}
	changes := []executiondomain.ActivationFile{}
	dispatcher := filepath.Join(stateDir, "dispatcher")
	if _, err = os.Lstat(dispatcher); err == nil {
		if err = trustedFile(dispatcher); err != nil {
			return nil, err
		}
		info, e := os.Stat(dispatcher)
		if e != nil {
			return nil, e
		}
		if info.Size() > 256<<20 {
			return nil, errors.New("old Scheduler dispatcher exceeds its preservation budget")
		}
		hash, e := fileDigest(dispatcher)
		if e != nil {
			return nil, e
		}
		changes = append(changes, executiondomain.ActivationFile{Path: dispatcher, Before: hash, Image: true, Mode: 0700})
	} else if len(j.Entries) > 0 || !os.IsNotExist(err) {
		return nil, errors.New("Scheduler dispatcher is missing; preserve its registration evidence")
	} else {
		changes = append(changes, executiondomain.ActivationFile{Path: dispatcher, Before: "absent", Image: true, Mode: 0700})
	}
	changes = append(changes, executiondomain.ActivationFile{Path: path, Before: hashBytes(original), Data: converted, Mode: 0600})
	return changes, nil
}

// VerifyExecutionActivationLocked allows receipts/history to evolve after floor
// commit. It verifies current format and exact compatible retained dispatcher.
func VerifyExecutionActivationLocked(ctx context.Context, imageSHA string) error {
	if absent() {
		return errors.New("activation requires an explicit current Scheduler core even for originally absent state")
	}
	if err := activationStateReady(); err != nil {
		return err
	}
	j, err := readJournal(false)
	if err != nil {
		return err
	}
	if j.Schema != executiondomain.CurrentSchedulerSchema {
		return errors.New("Scheduler execution format was not converted")
	}
	{
		if err := trustedFile(filepath.Join(stateDir, "dispatcher")); err != nil {
			return err
		}
		if got, e := fileDigest(filepath.Join(stateDir, "dispatcher")); e != nil || imageSHA != "" && got != imageSHA {
			return errors.New("compatible Scheduler dispatcher readback failed")
		}
		have, e := executiondomain.ReadHelperCapabilities(ctx, filepath.Join(stateDir, "dispatcher"), imageSHA)
		if e != nil {
			return e
		}
		need := executiondomain.Capabilities()
		need.SystemTaskSupport = 0
		need.SystemTaskSchemas = nil
		need.RoleContracts = map[string][]string{"scheduler-execute": need.RoleContracts["scheduler-execute"]}
		if err = executiondomain.RequireCapabilities(have, need); err != nil {
			return err
		}
	}
	if j.Schema == executiondomain.SchedulerLoaderSchema && imageSHA != "" {
		path := filepath.Join(stateDir, "helpers", imageSHA)
		if err := trustedFile(path); err != nil {
			return err
		}
		have, e := executiondomain.ReadHelperCapabilities(ctx, path, imageSHA)
		if e != nil {
			return e
		}
		need := executiondomain.Capabilities()
		need.RoleContracts = map[string][]string{"scheduler-execute": need.RoleContracts["scheduler-execute"], "scheduler-loader-reader": need.RoleContracts["scheduler-loader-reader"]}
		if e = executiondomain.RequireCapabilities(have, need); e != nil {
			return e
		}
	}
	return nil
}

func ValidateExecutionActivationPlan(original, intended []byte) error {
	return ValidateExecutionActivationPlanForSchema(original, intended, executiondomain.CurrentSchedulerSchema)
}

// Format2 activation archives forever target schema4; advancing the compiled
// current format must not reinterpret their historical conversion projection.
func ValidateExecutionActivationPlanForSchema(original, intended []byte, target int) error {
	return validateExecutionActivationProjection(original, intended, target, "")
}
func ValidateHistoricalExecutionActivationPlan(original, intended []byte, operation string) error {
	if len(original) == 0 || !loaderUUID(operation) {
		return errors.New("historical projection requires exact source and operation")
	}
	return validateExecutionActivationProjection(original, intended, 5, operation)
}
func validateExecutionActivationProjection(original, intended []byte, target int, retirementOperation string) error {
	if target != 4 && target != executiondomain.SchedulerLoaderSchema {
		return errors.New("unknown Scheduler activation target schema")
	}
	if target == executiondomain.SchedulerLoaderSchema {
		var projected journal
		d := json.NewDecoder(bytes.NewReader(intended))
		d.DisallowUnknownFields()
		if err := d.Decode(&projected); err != nil {
			return err
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(intended, &fields); err != nil {
			return err
		}
		wantLoader := "false"
		var sourceHeader struct {
			Schema      int  `json:"schema"`
			Established bool `json:"loader_epochs_established"`
		}
		if len(original) > 0 {
			if err := json.Unmarshal(original, &sourceHeader); err != nil {
				return err
			}
		}
		if sourceHeader.Schema == 5 && sourceHeader.Established {
			wantLoader = "true"
		}
		if projected.Schema != target || string(fields["loader_epochs_established"]) != wantLoader {
			return errors.New("schema5 activation must publish only the explicit false loader fence")
		}
		for _, inv := range projected.Invocations {
			if !inv.Started {
				return errors.New("schema5 activation cannot fence pending historical Scheduler launch authority")
			}
			if err := validateBootstrapInvocation(inv); err != nil {
				return err
			}
		}
	}
	var j journal
	if len(original) == 0 {
		expected, _ := json.Marshal(emptyExecutionJournalForSchema(target))
		if !bytes.Equal(expected, intended) {
			return errors.New("Scheduler absent-state creation contains unproven history or intent")
		}
		return nil
	}
	if err := json.Unmarshal(original, &j); err != nil {
		return err
	}
	if j.Schema == 1 && j.Migrations == nil {
		j.Migrations = map[string]legacyMigration{}
	}
	if retirementOperation != "" {
		if j.HistoricalRetirement != nil {
			return errors.New("historical retirement cannot replace an existing migration contract")
		}
		j.HistoricalRetirement = &historicalRetirement{Contract: executiondomain.SchedulerHistoricalRetirementContract, Operation: retirementOperation, SourceSHA256: hashBytes(original), Phase: "pending"}
	}
	j.Schema = target
	for key, v := range j.Invocations {
		if v.Adapter == "" {
			v.Adapter = executiondomain.CgroupExitV1
		}
		j.Invocations[key] = v
	}
	expected, err := executiondomain.MergeActivationJSON(original, j)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected, intended) {
		return errors.New("Scheduler activation would change intent or retained history")
	}
	return nil
}

func activationStateReady() error {
	path := stateDir + ".initialized"
	if err := trustedFile(path); err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if string(raw) != "1\n" {
		return errors.New("Scheduler initialization is incomplete")
	}
	return nil
}

func emptyExecutionJournal() journal {
	return emptyExecutionJournalForSchema(executiondomain.CurrentSchedulerSchema)
}
func emptyExecutionJournalForSchema(schema int) journal {
	return journal{Schema: schema, LastSweep: map[string]int64{}, Entries: map[int]Entry{}, Invocations: map[string]invocation{}, Holds: map[string]string{}, Released: map[string]bool{}, Legacy: map[string]string{}, Migrations: map[string]legacyMigration{}}
}
func executionCreationPlan(operation string) []executiondomain.ActivationFile {
	raw, _ := json.Marshal(emptyExecutionJournal())
	return []executiondomain.ActivationFile{
		{Path: stateDir + ".initialized", Before: "absent", Data: []byte("1\n"), Fence: []byte("activation:" + operation + "\n"), Mode: 0600},
		{Path: stateDir, Before: "absent", Directory: true, Mode: 0700},
		{Path: filepath.Join(stateDir, "gate.lock"), Before: "absent", Lock: true, Mode: 0600},
		{Path: filepath.Join(stateDir, "dispatcher"), Before: "absent", Image: true, Mode: 0700},
		{Path: filepath.Join(stateDir, "state.json"), Before: "absent", Data: raw, Mode: 0600},
	}
}
