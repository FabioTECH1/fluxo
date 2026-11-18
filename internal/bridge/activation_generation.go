package bridge

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"fluxo/internal/services/executiondomain"
)

const activationFresh = "fresh"
const activationReaderUpgrade = "reader-generation-upgrade"
const activationTaskHelperRefresh = "task-helper-refresh"
const activationHistoricalRetirement = "historical-retirement"
const activationReader7Upgrade = "reader7-upgrade"
const activationTaskBackendRequalification = "task-backend-requalification"
const activationInstallerTaskBackendRenewal = "installer-task-backend-renewal"

func taskBackendTransitionKind(kind string) bool {
	return kind == activationTaskBackendRequalification || kind == activationInstallerTaskBackendRenewal
}

// Format2 is frozen historical schema4/task3, regardless of today's Current.
// Format3 explicitly records the new target and an exact previous floor for a
// reviewed upgrade. Original activation archives are never edited or reused.
func activationTargetSchemas(p executionActivation) (int, int, error) {
	if p.Format == 2 {
		if p.Kind != "" || p.TargetSchedulerSchema != 0 || p.TargetSystemTaskSchema != 0 || len(p.SourceFloor) != 0 || p.SourceFloorSHA256 != "" {
			return 0, 0, errors.New("historical format2 activation acquired new semantics")
		}
		return 4, 3, nil
	}
	if (p.Format != 3 && p.Format != 4) || p.TargetSchedulerSchema != 5 || p.TargetSystemTaskSchema != 3 {
		return 0, 0, errors.New("unsupported explicit activation target")
	}
	if p.Format == 3 && (p.Kind == activationHistoricalRetirement || p.Kind == activationReader7Upgrade || taskBackendTransitionKind(p.Kind)) {
		return 0, 0, errors.New("reader7 operation cannot reinterpret a format3 archive")
	}
	switch p.Kind {
	case activationFresh, activationHistoricalRetirement:
		if len(p.SourceFloor) != 0 || p.SourceFloorSHA256 != "" {
			return 0, 0, errors.New("fresh activation cannot replace an existing floor")
		}
	case activationReaderUpgrade, activationTaskHelperRefresh, activationReader7Upgrade, activationTaskBackendRequalification, activationInstallerTaskBackendRenewal:
		if len(p.SourceFloor) == 0 || digest(p.SourceFloor) != p.SourceFloorSHA256 {
			return 0, 0, errors.New("reader upgrade lost its exact original floor bytes")
		}
		source, e := executiondomain.ParseReaderFloor(p.SourceFloor)
		if e != nil {
			return 0, 0, e
		}
		if p.Kind == activationTaskHelperRefresh {
			e = validateTaskHelperSourceFloorForFormat(source, p.Format)
		} else if taskBackendTransitionKind(p.Kind) {
			e = validateTaskRequalificationSourceFloor(source)
		} else if p.Kind == activationReader7Upgrade {
			e = validateReader6SourceFloor(source)
		} else {
			e = validatePrototypeSourceFloor(source)
		}
		if e != nil {
			return 0, 0, e
		}
		if source.InstallationID != p.InstallationID || source.ActivationID == p.ID {
			return 0, 0, errors.New("reader upgrade source installation/operation changed")
		}
	default:
		return 0, 0, errors.New("unknown activation operation kind")
	}
	return p.TargetSchedulerSchema, p.TargetSystemTaskSchema, nil
}
func validateTaskHelperSourceFloor(f executiondomain.ReaderFloor) error {
	return validateTaskHelperSourceFloorForFormat(f, 4)
}
func validateTaskHelperSourceFloorForFormat(f executiondomain.ReaderFloor, format int) error {
	if f.Phase != "committed" || f.SchedulerSchema != 5 || f.SystemTaskSchema != 3 || !activationFloorContractMatches(f, format) {
		return errors.New("task helper refresh requires its exact frozen committed reader contract")
	}
	return nil
}
func validateReader6SourceFloor(f executiondomain.ReaderFloor) error {
	if f.Phase != "committed" || f.SchedulerSchema != 5 || f.SystemTaskSchema != 3 || !activationFloorContractMatches(f, 3) {
		return errors.New("reader7 upgrade requires an exact committed reader6 contract")
	}
	return nil
}
func validatePrototypeSourceFloor(f executiondomain.ReaderFloor) error {
	if f.Phase != "committed" || f.SchedulerSchema != 4 || f.SystemTaskSchema != 3 || f.SchedulerSupport != 5 || f.SystemTaskSupport != 3 {
		return errors.New("only a reviewed committed schema4/task3 prototype can upgrade to schema5")
	}
	if len(f.RoleContracts["scheduler-loader-reader"]) != 0 {
		return errors.New("prototype source already claims unsupported loader authority")
	}
	for _, v := range f.RoleContracts["scheduler-execute"] {
		if v == executiondomain.SchedulerLoaderExecutionContract || v == executiondomain.SchedulerBootstrapContract {
			return errors.New("prototype source floor cannot claim schema5 execution")
		}
	}
	return nil
}
func activationSourceFloorMatches(p executionActivation, f *executiondomain.ReaderFloor) bool {
	if (p.Kind != activationReaderUpgrade && p.Kind != activationTaskHelperRefresh && p.Kind != activationReader7Upgrade && !taskBackendTransitionKind(p.Kind)) || f == nil {
		return false
	}
	source, e := executiondomain.ParseReaderFloor(p.SourceFloor)
	if e != nil {
		return false
	}
	// Caller additionally compares the original protected raw checksum before
	// replacing the source floor; this predicate classifies exact recovery phase.
	return source.ActivationID == f.ActivationID && source.InstallationID == f.InstallationID && source.BinarySHA256 == f.BinarySHA256 && source.PlanSHA256 == f.PlanSHA256 && source.Phase == f.Phase && source.SchedulerSchema == f.SchedulerSchema && source.SystemTaskSchema == f.SystemTaskSchema && source.SchedulerSupport == f.SchedulerSupport && source.SystemTaskSupport == f.SystemTaskSupport
}

func activationSchedulerTarget(p executionActivation) int {
	s, _, _ := activationTargetSchemas(p)
	return s
}

// Before publishing a prepared replacement, bind the actual protected bytes,
// including every role contract, to the archived source. Scalar version checks
// classify a phase but never authorize replacing a floor.
func validateActivationFloorTransition(dataDir string, p executionActivation, planBytes []byte, f *executiondomain.ReaderFloor) error {
	return validateActivationFloorTransitionWithRead(dataDir, p, planBytes, f, readProtected)
}
func validateActivationFloorTransitionWithRead(dataDir string, p executionActivation, planBytes []byte, f *executiondomain.ReaderFloor, read func(string, int64) ([]byte, error)) error {
	if activationSourceFloorMatches(p, f) {
		raw, err := read(filepath.Join(dataDir, "execution-readers.json"), 64<<10)
		if err != nil {
			return err
		}
		if digest(raw) != p.SourceFloorSHA256 {
			return errors.New("source reader floor differs from the exact archived bytes")
		}
		return nil
	}
	if f == nil {
		if p.Kind == activationReaderUpgrade || p.Kind == activationTaskHelperRefresh || p.Kind == activationReader7Upgrade || taskBackendTransitionKind(p.Kind) {
			return errors.New("reader upgrade source floor disappeared; preserve exact recovery evidence")
		}
		return nil
	}
	if f.Phase != "prepared" || f.ActivationID != p.ID || f.InstallationID != p.InstallationID || f.BinarySHA256 != p.BinarySHA256 || f.PlanSHA256 != digest(planBytes) {
		return errors.New("reader floor is not this exact activation's prepared transition")
	}
	target, task, err := activationTargetSchemas(p)
	if err != nil {
		return err
	}
	if f.SchedulerSchema != target || f.SystemTaskSchema != task || !activationPlanFloorContractMatches(*f, p) {
		return errors.New("prepared reader floor target changed")
	}
	return nil
}

func validateReaderUpgradeSourceFloor(f executiondomain.ReaderFloor) error {
	if f.SchedulerSchema == 5 {
		return validateReader6SourceFloor(f)
	}
	return validatePrototypeSourceFloor(f)
}

func validateHistoricalSourcesCurrent(p executionActivation) error {
	if p.Kind != activationHistoricalRetirement {
		return nil
	}
	for path, expected := range p.HistoricalSources {
		current, err := readProtected(path, 1<<20)
		if expected == nil && os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !bytes.Equal(current, expected) {
			return errors.New("historical source changed since exact activation intent")
		}
	}
	return nil
}

// Requalification preserves the exact known source generation. It cannot
// downgrade reader7 or drop its permanent historical-retirement reader role.
func validateTaskRequalificationSourceFloor(f executiondomain.ReaderFloor) error {
	if f.Phase != "committed" || f.SchedulerSchema != 5 || f.SystemTaskSchema != 3 || !(activationFloorContractMatches(f, 3) || activationFloorContractMatches(f, 4)) {
		return errors.New("task requalification requires an exact committed reader6 or reader7 contract")
	}
	return nil
}
