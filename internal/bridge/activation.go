package bridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/scheduler"
	"github.com/google/uuid"
	"golang.org/x/mod/semver"
	"golang.org/x/sys/unix"
)

type ExecutionActivationReport struct {
	OperationID     string `json:"operation_id"`
	Phase           string `json:"phase"`
	FormatActive    bool   `json:"format_active"`
	BridgeQualified bool   `json:"bridge_qualified"`
	RecoveryPolicy  string `json:"recovery_policy"`
}

type executionActivation struct {
	HistoricalTaskRetirements []scheduler.CronBackendRetirement `json:"installer_historical_task_retirements,omitempty"`
	TaskBackendSHA256         string                            `json:"task_backend_sha256,omitempty"`
	SchedulerSourceSHA256     string                            `json:"scheduler_source_sha256,omitempty"`
	HistoricalSources         map[string][]byte                 `json:"historical_sources,omitempty"`
	Kind                      string                            `json:"kind,omitempty"`
	TargetSchedulerSchema     int                               `json:"target_scheduler_schema,omitempty"`
	TargetSystemTaskSchema    int                               `json:"target_system_task_schema,omitempty"`
	SourceFloor               []byte                            `json:"source_floor,omitempty"`
	SourceFloorSHA256         string                            `json:"source_floor_sha256,omitempty"`
	Format                    int                               `json:"format"`
	ID                        string                            `json:"id"`
	InstallationID            string                            `json:"installation_id"`
	BinarySHA256              string                            `json:"binary_sha256"`
	Version                   string                            `json:"version"`
	Database                  map[string]string                 `json:"database_sha256"`
	Auxiliary                 map[string]string                 `json:"auxiliary_sha256"`
	Files                     []executiondomain.ActivationFile  `json:"files"`
}

// ActivateExecutionFormats changes execution readers only. It does not issue a
// bridge completion receipt, enable the 249 candidate or qualify new profiles.
// Recovery is scoped to an exact protected operation and the same executing
// installed image. Ordinary entrypoints never bypass a prepared reader floor.
func ActivateExecutionFormats(ctx context.Context, dataDir, version, resume string) (ExecutionActivationReport, error) {
	return activateExecutionFormats(ctx, dataDir, version, activationRequest{Resume: resume})
}

// UpgradeExecutionFormats is an explicit archived generation transition. It is
// never inferred from fresh activation or an ordinary daemon startup.
func UpgradeExecutionFormats(ctx context.Context, dataDir, version string) (ExecutionActivationReport, error) {
	return activateExecutionFormats(ctx, dataDir, version, activationRequest{Upgrade: true})
}

func RefreshTaskHelpers(ctx context.Context, dataDir, version, expectedTaskCore string) (ExecutionActivationReport, error) {
	decoded, err := hex.DecodeString(expectedTaskCore)
	if err != nil || len(decoded) != 32 || strings.ToLower(expectedTaskCore) != expectedTaskCore {
		return ExecutionActivationReport{}, errors.New("task helper refresh requires an exact lowercase task-core SHA256")
	}
	return activateExecutionFormats(ctx, dataDir, version, activationRequest{ExpectedTaskCore: expectedTaskCore})
}

func ActivateHistoricalRetirement(ctx context.Context, dataDir, version string) (ExecutionActivationReport, error) {
	return activateExecutionFormats(ctx, dataDir, version, activationRequest{Historical: true})
}

type activationRequest struct {
	Candidate *maintenanceCandidateAdmission
	// Installer orchestration owns the exclusive lifecycle lock itself while
	// stopping/restarting the panel. These hooks are not public CLI authority.
	LifecycleOwned bool
	// Installer archives are built outside the reader authority namespace. The
	// callback durably records exact recovery identity before atomic publication.
	InstallerPrepared        func(executionActivation, []byte) error
	Resume                   string
	Upgrade                  bool
	ExpectedTaskCore         string
	Historical               bool
	TaskBackendSHA256        string
	InstallerTaskRenewal     bool
	InstallerHistoricalTasks bool
}

func activateExecutionFormats(ctx context.Context, dataDir, version string, request activationRequest) (ExecutionActivationReport, error) {
	resume, upgrade, expectedTaskCore, historical := request.Resume, request.Upgrade, request.ExpectedTaskCore, request.Historical
	taskBackend := request.TaskBackendSHA256 != ""
	report := ExecutionActivationReport{RecoveryPolicy: "retain-current-and-historical-live-recovery"}
	if os.Geteuid() != 0 || dataDir != "/var/lib/fluxo" || !semver.IsValid("v"+version) {
		return report, errors.New("execution activation requires root, the default installed data directory and a versioned bridge image")
	}
	if err := trustedDirectory(dataDir); err != nil {
		return report, err
	}
	executable, err := os.Executable()
	if err != nil || executable != "/usr/local/bin/fluxo" && request.Candidate == nil {
		return report, errors.New("stage and run the compatible installed /usr/local/bin/fluxo before execution activation")
	}
	image, err := readCertifyingExecutable()
	if err != nil {
		return report, err
	}
	if len(image) > 128<<20 {
		return report, errors.New("executing image exceeds the retained-helper size budget")
	}
	imageSHA := digest(image)
	if request.Candidate != nil {
		if err = request.Candidate.revalidate(version); err != nil {
			return report, err
		}
	}
	// Fresh activation is an ordinary new writer until its own exact plan has
	// been established. It cannot repair missing floors, partial anchors or a
	// lost operation archive by allocating another UUID. Exact recovery uses
	// the separate UUID/image/plan validation below.
	if resume == "" {
		if _, err = executiondomain.InspectCompatibility(dataDir); err != nil {
			return report, err
		}
	}
	if resume != "" {
		if _, err = uuid.Parse(resume); err != nil {
			return report, errors.New("recovery requires an exact activation operation UUID")
		}
	}
	beforeLocks, err := executiondomain.ReadFloor(dataDir)
	if err != nil {
		return report, err
	}
	if request.Candidate != nil && request.Candidate.Handoff.Action == "requalify" {
		raw, e := readProtected(filepath.Join(dataDir, "execution-readers.json"), 64<<10)
		if e != nil {
			return report, e
		}
		if digest(raw) != request.Candidate.Handoff.SourceFloorSHA256 {
			return report, errors.New("installer reviewed source floor changed before admission")
		}
	}

	if expectedTaskCore != "" {
		if beforeLocks == nil {
			return report, errors.New("task helper refresh requires retained current activation evidence")
		}
		if taskBackend {
			err = validateTaskRequalificationSourceFloor(*beforeLocks)
		} else {
			err = validateTaskHelperSourceFloor(*beforeLocks)
		}
		if err != nil {
			return report, err
		}
		if err = checkTaskRefreshSource(expectedTaskCore); err != nil {
			return report, err
		}
	}
	if upgrade {
		if executiondomain.CurrentSchedulerSchema != executiondomain.SchedulerLoaderSchema || executiondomain.SchedulerCompiledReaderSupport != executiondomain.SchedulerHistoricalRetirementSupport {
			return report, errors.New("schema5 reader-generation upgrade is not compiled as current")
		}
		if beforeLocks == nil {
			return report, errors.New("reader upgrade requires its existing protected source floor; use exact recovery for lost state")
		}
		if err = validateReaderUpgradeSourceFloor(*beforeLocks); err != nil {
			return report, err
		}
	}
	if !request.LifecycleOwned {
		release, e := packages.Lifecycle(ctx, false)
		if e != nil {
			return report, e
		}
		defer release()
	}
	releasePackages, err := packages.Acquire(ctx)
	if err != nil {
		return report, err
	}
	defer releasePackages()
	releaseNative, err := packages.NativeDPKG(ctx)
	if err != nil {
		return report, err
	}
	defer releaseNative()
	if err = activationGatesPresent(); err != nil {
		return report, err
	}

	if err = panelStopped(ctx); err != nil {
		return report, err
	}
	if err = packages.TinkerDomainsIdle(); err != nil {
		return report, err
	}
	earlyFloor, e := executiondomain.ReadFloor(dataDir)
	if e != nil {
		return report, e
	}
	if resume == "" && earlyFloor != nil && earlyFloor.Phase == "committed" {
		if _, e = executiondomain.InspectCompatibility(dataDir); e != nil {
			return report, e
		}
		if earlyFloor.SchedulerSchema != executiondomain.CurrentSchedulerSchema || earlyFloor.SystemTaskSchema != executiondomain.CurrentSystemTaskSchema {
			if !upgrade {
				return report, errors.New("existing prototype floor requires explicit execution-compatibility upgrade; preserve its evidence")
			}
		}
	}
	if resume == "" {
		if _, err = executiondomain.InspectCompatibility(dataDir); err != nil {
			return report, err
		}
	}
	// Recheck after lifecycle/package exclusion and before initializer locks.
	locks, err := acquireActivationLocks(ctx)
	if err != nil {
		return report, err
	}
	defer locks.release()
	if err = panelStopped(ctx); err != nil {
		return report, err
	}
	if resume == "" {
		if _, err = executiondomain.InspectCompatibility(dataDir); err != nil {
			return report, err
		}
	}
	closingSourceRows := resume == "" && (earlyFloor == nil || earlyFloor.SchedulerSchema != executiondomain.CurrentSchedulerSchema || earlyFloor.SystemTaskSchema != executiondomain.CurrentSystemTaskSchema)
	installationID, dbHashes, err := activationDatabase(ctx, dataDir, closingSourceRows)
	if err != nil {
		return report, err
	}
	floor, err := executiondomain.ReadFloor(dataDir)
	if err != nil {
		return report, err
	}
	if request.Candidate != nil && request.Candidate.Handoff.InstallationID != installationID {
		return report, errors.New("installer handoff installation identity changed")
	}
	if request.Candidate != nil && request.Candidate.Handoff.Action == "requalify" {
		raw, e := readProtected(filepath.Join(dataDir, "execution-readers.json"), 64<<10)
		if e != nil {
			return report, e
		}
		if digest(raw) != request.Candidate.Handoff.SourceFloorSHA256 {
			return report, errors.New("installer source floor changed while acquiring admission")
		}
	}

	if !upgrade && !historical && reuseCommittedActivation(floor, resume, expectedTaskCore) {
		if resume != "" && resume != floor.ActivationID {
			return report, errors.New("committed activation operation does not match recovery UUID")
		}
		if floor.InstallationID != installationID {
			return report, errors.New("activation floor belongs to another installation")
		}
		if err = validateCommittedActivationArchive(dataDir, *floor); err != nil {
			return report, err
		}
		if resume != "" && imageSHA != floor.BinarySHA256 {
			return report, errors.New("exact activation recovery requires its original installed image")
		}
		if err = verifyCommittedActivationOperation(ctx, dataDir, *floor, resume); err != nil {
			return report, err
		}
		if _, err = executiondomain.InspectCompatibility(dataDir); err != nil {
			return report, err
		}
		complete := ExecutionActivationReport{OperationID: floor.ActivationID, Phase: "committed", FormatActive: true, RecoveryPolicy: report.RecoveryPolicy}
		if resume != "" {
			if err = writeActivationJSON(filepath.Join(dataDir, "execution-activations", floor.ActivationID, "completed.json"), complete); err != nil {
				return report, err
			}
		}
		return complete, nil
	}
	parent := filepath.Join(dataDir, "execution-activations")
	var plan executionActivation
	var planBytes []byte
	var operation string
	if resume == "" {
		if floor != nil {
			if !upgrade && expectedTaskCore == "" {
				return report, fmt.Errorf("activation %s is incomplete; run execution-compatibility recover %s using its exact installed image", floor.ActivationID, floor.ActivationID)
			}
			if expectedTaskCore != "" {
				if taskBackend {
					err = validateTaskRequalificationSourceFloor(*floor)
				} else {
					err = validateTaskHelperSourceFloor(*floor)
				}
			} else {
				err = validateReaderUpgradeSourceFloor(*floor)
			}
			if err != nil {
				return report, err
			}
			if err = validateCommittedActivationArchive(dataDir, *floor); err != nil {
				return report, err
			}
		}
		if _, err = executiondomain.InspectCompatibility(dataDir); err != nil {
			return report, err
		}
		operationID := uuid.NewString()
		if request.Candidate != nil {
			operationID = request.Candidate.Handoff.OperationID
		}
		var one []executiondomain.ActivationFile
		if expectedTaskCore != "" {
			if err = checkTaskRefreshSource(expectedTaskCore); err == nil {
				if taskBackend {
					err = scheduler.ValidateTaskRequalificationLocked(ctx)
				} else {
					err = scheduler.VerifyExecutionActivationLocked(ctx, "")
				}
			}
		} else if historical {
			one, err = scheduler.PlanHistoricalExecutionActivationLocked(ctx, operationID)
		} else {
			one, err = scheduler.PlanExecutionActivationLocked(ctx, operationID)
		}
		if err != nil {
			return report, err
		}
		var two []executiondomain.ActivationFile
		var taskRetirements []scheduler.CronBackendRetirement
		if taskBackend {
			if request.InstallerTaskRenewal {
				two, err = maintenancetask.PlanInstallerRegistrationRenewalLocked(ctx, expectedTaskCore, request.TaskBackendSHA256)
			} else {
				two, err = maintenancetask.PlanRegistrationRequalificationLocked(ctx, expectedTaskCore, request.TaskBackendSHA256)
			}
		} else if request.InstallerHistoricalTasks {
			two, err = maintenancetask.PlanInstallerHistoricalActivationLocked(ctx, imageSHA, operationID, &taskRetirements)
		} else {
			two, err = maintenancetask.PlanExecutionActivationLocked(ctx, imageSHA, operationID)
		}
		if err != nil {
			return report, err
		}
		if expectedTaskCore != "" {
			matched := false
			for _, change := range two {
				if change.Path == "/var/lib/fluxo-system-tasks/state.json" {
					matched = change.Before == expectedTaskCore
				}
			}
			if !matched {
				return report, errors.New("task helper refresh source changed during planning")
			}
		}
		plan = executionActivation{HistoricalTaskRetirements: taskRetirements, Format: 2, ID: operationID, InstallationID: installationID, BinarySHA256: imageSHA, Version: version, Database: dbHashes, Files: append(one, two...)}
		if executiondomain.CurrentSchedulerSchema >= executiondomain.SchedulerLoaderSchema {
			plan.Format, plan.Kind = 4, activationFresh
			plan.TargetSchedulerSchema, plan.TargetSystemTaskSchema = executiondomain.CurrentSchedulerSchema, executiondomain.CurrentSystemTaskSchema
			if expectedTaskCore == "" {
				pinned, e := planSchedulerPinnedHelper(imageSHA)
				if e != nil {
					return report, e
				}
				plan.Files = append(plan.Files, pinned...)
			}
			if historical {
				plan.Kind = activationHistoricalRetirement
			}
			if upgrade || expectedTaskCore != "" {
				plan.Kind = activationReaderUpgrade
				if floor.SchedulerSchema == 5 {
					plan.Kind = activationReader7Upgrade
				}
				if expectedTaskCore != "" {
					plan.Kind = activationTaskHelperRefresh
					if taskBackend {
						plan.Kind = activationTaskBackendRequalification
						if request.InstallerTaskRenewal {
							plan.Kind = activationInstallerTaskBackendRenewal
						}
					}
				}
				plan.SourceFloor, err = readProtected(filepath.Join(dataDir, "execution-readers.json"), 64<<10)
				if err != nil {
					return report, err
				}
				plan.SourceFloorSHA256 = digest(plan.SourceFloor)
			}
		}

		if len(taskRetirements) > 0 {
			plan.Kind = activationInstallerHistoricalTasks
			if historical {
				plan.Kind = activationInstallerHistoricalCombined
			}
		}
		if taskBackend {
			plan.TaskBackendSHA256 = request.TaskBackendSHA256
			plan.SchedulerSourceSHA256, err = activationProtectedHash("/var/lib/fluxo-scheduler/state.json", 32<<20)
			if err != nil {
				return report, err
			}
		}
		if historical {
			plan.HistoricalSources, err = scheduler.HistoricalActivationSourcesLocked()
			if err != nil {
				return report, err
			}
		}
		plan.Auxiliary, err = activationAuxiliary()
		if err != nil {
			return report, err
		}
		for i := range plan.Files {
			if plan.Files[i].Cron {
				info, e := protectedInfo(plan.Files[i].Path)
				if e != nil {
					return report, e
				}
				at := time.Now().Unix()
				if at == info.ModTime().Unix() {
					at = info.ModTime().Unix() + 1
				}
				plan.Files[i].CronMTime = at
			}
		}
		if err = validateActivationPlan(plan); err != nil {
			return report, err
		}
		operation = filepath.Join(parent, plan.ID)
		if request.InstallerPrepared != nil {
			operation = installerStagedArchive(dataDir, plan.ID)
		} else if err = ensureActivationParent(parent); err != nil {
			return report, err
		}
		if err = os.Mkdir(operation, 0700); err != nil {
			return report, err
		}
		if err = syncActivationDirectory(filepath.Dir(operation)); err != nil {
			return report, err
		}
		// Archive exact original bytes before the immutable intent is published.
		for i, change := range plan.Files {
			if change.Before == "absent" {
				continue
			}
			old, e := readProtected(change.Path, 256<<20)
			if e != nil {
				return report, e
			}
			if digest(old) != change.Before {
				return report, errors.New("activation source changed before its backup")
			}
			if e = writeActivationFile(filepath.Join(operation, fmt.Sprintf("before-%d", i)), old, 0600); e != nil {
				return report, e
			}
		}
		if err = writeActivationFile(filepath.Join(operation, "image"), image, 0700); err != nil {
			return report, err
		}
		planBytes, err = json.Marshal(plan)
		if err != nil {
			return report, err
		}
		if err = writeActivationFile(filepath.Join(operation, "plan.json"), planBytes, 0600); err != nil {
			return report, err
		}
		if request.InstallerPrepared != nil {
			if err = request.InstallerPrepared(plan, planBytes); err != nil {
				return report, err
			}
			if err = publishInstallerArchive(dataDir, plan.ID); err != nil {
				return report, err
			}
			operation = filepath.Join(parent, plan.ID)
		}
	} else {
		operation = filepath.Join(parent, resume)
		planBytes, err = readProtected(filepath.Join(operation, "plan.json"), 128<<20)
		if err != nil {
			return report, err
		}
		decoder := json.NewDecoder(bytes.NewReader(planBytes))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&plan); err != nil {
			return report, err
		}
		var extra any
		if err = decoder.Decode(&extra); err != io.EOF {
			return report, errors.New("activation plan has unexpected trailing data")
		}
		if err = validateActivationPlan(plan); err != nil {
			return report, err
		}
		if err = validateActivationRecovery(plan, resume, installationID, imageSHA, version, floor, planBytes); err != nil {
			return report, err
		}
	}
	if request.Candidate != nil {
		h := request.Candidate.Handoff
		if plan.Kind != activationTaskBackendRequalification || plan.ID != h.OperationID || plan.SourceFloorSHA256 != h.SourceFloorSHA256 {
			return report, errors.New("installer handoff cannot authorize another operation or source floor")
		}
	}
	targetScheduler, targetTask, err := activationTargetSchemas(plan)
	if err != nil {
		return report, err
	}
	if targetScheduler != executiondomain.CurrentSchedulerSchema || targetTask != executiondomain.CurrentSystemTaskSchema || plan.Format != 4 {
		return report, errors.New("activation recovery requires the original reader generation; this image cannot retarget its immutable plan")
	}
	if plan.Kind == activationTaskHelperRefresh || taskBackendTransitionKind(plan.Kind) {
		source, e := executiondomain.ParseReaderFloor(plan.SourceFloor)
		if e != nil {
			return report, e
		}
		if e = validateCommittedActivationArchive(dataDir, source); e != nil {
			return report, e
		}
	}
	if err = validateActivationFloorTransition(dataDir, plan, planBytes, floor); err != nil {
		return report, err
	}
	report.OperationID, report.Phase = plan.ID, "prepared"
	if !sameHashes(plan.Database, dbHashes) {
		return report, errors.New("installation database changed since activation intent; preserve its evidence")
	}
	aux, err := activationAuxiliary()
	if err != nil {
		return report, err
	}
	if err = validateRequalificationSchedulerSource(plan); err != nil {
		return report, err
	}
	if err = validateHistoricalSourcesCurrent(plan); err != nil {
		return report, err
	}
	if !activationAuxiliaryMatches(plan, aux) {
		return report, errors.New("historic coordination evidence changed since activation intent")
	}
	archivedImage, err := readProtected(filepath.Join(operation, "image"), 256<<20)
	if err != nil || digest(archivedImage) != imageSHA {
		return report, errors.New("activation image archive changed")
	}
	for i, change := range plan.Files {
		if change.Before != "absent" {
			old, e := readProtected(filepath.Join(operation, fmt.Sprintf("before-%d", i)), 256<<20)
			if e != nil || digest(old) != change.Before {
				return report, errors.New("activation source backup changed")
			}
		}
		if err = validateActivationCurrent(change, imageSHA); err != nil {
			return report, err
		}
	}
	if err = validateActivationSources(operation, plan); err != nil {
		return report, err
	}
	// Revalidate the archived retirement before staging any helper/core bytes,
	// including an exact recovery after an interrupted publication.
	for _, r := range plan.HistoricalTaskRetirements {
		if err = scheduler.VerifyInstallerHistoricalRetirement(ctx, r); err != nil {
			return report, err
		}
	}
	prepared, err := activationTargetFloor(plan, planBytes, "prepared")
	if err != nil {
		return report, err
	}
	applyTransitions := applyExecutionTransitions
	if plan.Kind == activationReader7Upgrade {
		applyTransitions = applyReader7UpgradeTransitions
	}
	if plan.Kind == activationTaskHelperRefresh || taskBackendTransitionKind(plan.Kind) {
		applyTransitions = applyTaskHelperRefreshTransitions
	}
	if err = applyTransitions(ctx, plan.Files, func(change executiondomain.ActivationFile) error {
		if change.Lock {
			if err := validateActivationCurrent(change, imageSHA); err != nil {
				return err
			}
			_, err := locks.acquire(ctx, change.Path, true)
			return err
		}
		return applyActivationFile(change, image)
	}, func() error {
		prepareFloor := func() error {
			for _, r := range plan.HistoricalTaskRetirements {
				if err := scheduler.VerifyInstallerHistoricalRetirement(ctx, r); err != nil {
					return err
				}
			}

			if request.Candidate != nil {
				if err := request.Candidate.revalidate(version); err != nil {
					return err
				}
			}
			actual, err := executiondomain.ReadFloor(dataDir)
			if err != nil {
				return err
			}
			if err = validateActivationFloorTransition(dataDir, plan, planBytes, actual); err != nil {
				return err
			}
			return writeActivationJSON(filepath.Join(dataDir, "execution-readers.json"), prepared)
		}
		if !taskBackendTransitionKind(plan.Kind) {
			return prepareFloor()
		}
		if err := validateRequalificationSchedulerSource(plan); err != nil {
			return err
		}
		return prepareTaskRequalificationFloor(plan.Kind, prepareFloor, func() error {
			return maintenancetask.VerifyRegistrationRequalificationTargetLocked(ctx, taskRequalificationIntended(plan))
		})
	}, func() error { return validateActivationTargets(plan, imageSHA) }); err != nil {
		return report, err
	}
	if err = verifyActivationOperation(ctx, plan, imageSHA); err != nil {
		return report, err
	}
	if err = panelStopped(ctx); err != nil {
		return report, err
	}
	if request.Candidate != nil {
		if err = request.Candidate.revalidate(version); err != nil {
			return report, err
		}
	}
	if current, err := readCertifyingExecutable(); err != nil || digest(current) != imageSHA {
		return report, errors.New("activation executable changed before commit")
	}
	_, afterDB, err := activationDatabase(ctx, dataDir, false)
	if err != nil {
		return report, err
	}
	if !sameHashes(afterDB, plan.Database) {
		return report, errors.New("database changed before execution format commit")
	}
	for _, change := range plan.Files {
		if err = validateActivationIntended(change, imageSHA); err != nil {
			return report, err
		}
	}

	if err = validateActivationTargets(plan, imageSHA); err != nil {
		return report, err
	}
	if err = validateRequalificationSchedulerSource(plan); err != nil {
		return report, err
	}
	if err = validateHistoricalSourcesCurrent(plan); err != nil {
		return report, err
	}
	if err = locks.verify(); err != nil {
		return report, err
	}
	if err = writeActivationJSON(filepath.Join(operation, "verified.json"), map[string]any{"operation_id": plan.ID, "plan_sha256": digest(planBytes), "format_readback_verified": true}); err != nil {
		return report, err
	}
	if taskBackendTransitionKind(plan.Kind) {
		if err = validateRequalificationSchedulerSource(plan); err != nil {
			return report, err
		}
		if err = maintenancetask.VerifyRegistrationRequalificationTargetLocked(ctx, taskRequalificationIntended(plan)); err != nil {
			return report, err
		}
	}
	if request.Candidate != nil {
		if err = request.Candidate.revalidate(version); err != nil {
			return report, err
		}
	}
	for _, r := range plan.HistoricalTaskRetirements {
		if err = scheduler.VerifyInstallerHistoricalRetirement(ctx, r); err != nil {
			return report, err
		}
	}
	prepared.Phase = "committed"
	if err = ctx.Err(); err != nil {
		return report, err
	}
	if err = writeActivationJSON(filepath.Join(dataDir, "execution-readers.json"), prepared); err != nil {
		return report, err
	}
	if _, err = executiondomain.InspectCompatibility(dataDir); err != nil {
		return report, err
	}
	report.Phase, report.FormatActive = "committed", true
	if err = writeActivationJSON(filepath.Join(operation, "completed.json"), report); err != nil {
		return report, err
	}
	return report, nil
}

func reuseCommittedActivation(floor *executiondomain.ReaderFloor, resume, expectedTaskCore string) bool {
	return floor != nil && floor.Phase == "committed" && floor.SchedulerSchema == executiondomain.CurrentSchedulerSchema && floor.SystemTaskSchema == executiondomain.CurrentSystemTaskSchema && expectedTaskCore == "" && (resume == "" || resume == floor.ActivationID)
}

func activationGatesPresent() error {
	for state, gate := range map[string]string{"/var/lib/fluxo-scheduler/state.json": "/var/lib/fluxo-scheduler/gate.lock", "/var/lib/fluxo-system-tasks/state.json": "/var/lib/fluxo-system-tasks.lock"} {
		if _, err := os.Lstat(state); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if _, err := protectedInfo(gate); err != nil {
			return errors.New("execution journal gate is missing or unsafe; reconcile it before activation")
		}
	}
	return nil
}

func verifyActivatedFormats(ctx context.Context, imageSHA string) error {
	if err := scheduler.VerifyExecutionActivationLocked(ctx, imageSHA); err != nil {
		return err
	}
	return maintenancetask.VerifyExecutionActivationLocked(ctx, imageSHA)
}

func verifyActivationOperation(ctx context.Context, plan executionActivation, imageSHA string) error {
	if taskBackendTransitionKind(plan.Kind) {
		for _, f := range plan.Files {
			if f.Path == "/var/lib/fluxo-system-tasks/state.json" {
				return maintenancetask.VerifyRegistrationRequalificationTargetLocked(ctx, f.Data)
			}
		}
		return errors.New("task requalification lost its target")
	}
	if plan.Kind == activationTaskHelperRefresh {
		if err := scheduler.VerifyExecutionActivationLocked(ctx, ""); err != nil {
			return err
		}
		return maintenancetask.VerifyExecutionActivationLocked(ctx, imageSHA)
	}
	return verifyActivatedFormats(ctx, imageSHA)
}

func verifyCommittedActivationOperation(ctx context.Context, dataDir string, floor executiondomain.ReaderFloor, resume string) error {
	if resume != "" {
		raw, err := readProtected(filepath.Join(dataDir, "execution-activations", floor.ActivationID, "plan.json"), 128<<20)
		if err != nil || digest(raw) != floor.PlanSHA256 {
			return errors.New("committed activation intent changed")
		}
		var plan executionActivation
		if err = json.Unmarshal(raw, &plan); err != nil {
			return err
		}
		if taskBackendTransitionKind(plan.Kind) {
			return maintenancetask.VerifyRegistrationRequalificationCommittedLocked(ctx, taskRequalificationIntended(plan))
		}
		if plan.Kind == activationTaskHelperRefresh {
			return verifyActivationOperation(ctx, plan, floor.BinarySHA256)
		}
	}
	return verifyActivatedFormats(ctx, "")
}

func checkTaskRefreshSource(expected string) error {
	raw, err := readProtected("/var/lib/fluxo-system-tasks/state.json", 8<<20)
	if err != nil {
		return err
	}
	var header struct {
		Schema int `json:"schema"`
	}
	if digest(raw) != expected || json.Unmarshal(raw, &header) != nil || header.Schema != executiondomain.CurrentSystemTaskSchema {
		return errors.New("task helper refresh source changed or is not current; inspect its SHA256 again")
	}
	return nil
}

func activationDatabase(ctx context.Context, dataDir string, requireClosedSchedulerRows bool) (string, map[string]string, error) {
	before, err := activationDatabaseHashes(dataDir)
	if err != nil {
		return "", nil, err
	}
	path, cleanup, err := snapshot(ctx, dataDir)
	if err != nil {
		return "", nil, err
	}
	defer cleanup()
	db, err := openSQLite(ctx, path, false)
	if err != nil {
		return "", nil, err
	}
	defer db.Close()
	if requireClosedSchedulerRows {
		if err = scheduler.ValidateActivationSchedulerRows(ctx, db); err != nil {
			return "", nil, err
		}
	}
	var id string
	if err = db.QueryRowContext(ctx, "SELECT value FROM system_metadata WHERE key='installation_id'").Scan(&id); err != nil {
		return "", nil, err
	}
	decoded, e := hex.DecodeString(id)
	if e != nil || len(decoded) != 16 || strings.ToLower(id) != id {
		return "", nil, errors.New("preserve the existing installation identity before activation")
	}
	for table, predicate := range map[string]string{"php_update_operations": "status IN ('queued','running')", "php_update_generations": "state NOT IN ('succeeded','rolled_back')", "php_update_barriers": "1", "php_repository_migrations": "state='pending'", "node_update_operations": "status IN ('queued','running')", "node_update_generations": "state NOT IN ('succeeded','rolled_back')", "node_update_barriers": "1", "node_update_php_barriers": "1"} {
		var exists, count int
		if err = db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&exists); err != nil {
			return "", nil, err
		}
		if exists == 0 {
			continue
		}
		if err = db.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE "+predicate).Scan(&count); err != nil {
			return "", nil, err
		}
		if count != 0 {
			return "", nil, errors.New("finish PHP/Node maintenance and recovery before format activation")
		}
	}
	after, err := activationDatabaseHashes(dataDir)
	if err != nil {
		return "", nil, err
	}
	if !sameHashes(before, after) {
		return "", nil, errors.New("database changed during activation inspection")
	}
	return id, before, nil
}

func activationDatabaseHashes(dataDir string) (map[string]string, error) {
	result := map[string]string{}
	for _, name := range []string{"fluxo.db", "fluxo.db-wal"} {
		path := filepath.Join(dataDir, name)
		hash, err := activationProtectedHash(path, 2<<30)
		if os.IsNotExist(err) && name != "fluxo.db" {
			result[path] = "absent"
			continue
		}
		if err != nil {
			return nil, err
		}
		result[path] = hash
	}
	return result, nil
}

func activationAuxiliary() (map[string]string, error) {
	result := map[string]string{}
	for _, path := range []string{"/var/lib/fluxo-scheduler.initialized", "/var/lib/fluxo-system-tasks.initialized", "/var/lib/fluxo-scheduler/backend-proofs.json", "/var/lib/fluxo-scheduler/backend-proofs.initialized", "/var/lib/fluxo-scheduler/backend-proofs.initializing"} {
		raw, err := readProtected(path, 32<<20)
		if os.IsNotExist(err) {
			result[path] = "absent"
			continue
		}
		if err != nil {
			return nil, err
		}
		result[path] = digest(raw)
	}
	return result, nil
}

func sameHashes(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func validateActivationPlan(p executionActivation) error {
	if installerHistoricalTaskKind(p.Kind) != (len(p.HistoricalTaskRetirements) > 0) {
		return errors.New("historical task retirement lost its explicit archive kind or witnesses")
	}
	if len(p.HistoricalTaskRetirements) > 0 {
		if p.Format != 4 {
			return errors.New("historical task retirement requires a new installer conversion archive")
		}
		for _, r := range p.HistoricalTaskRetirements {
			if err := scheduler.ValidateInstallerHistoricalRetirement(r); err != nil {
				return err
			}
		}
	}

	if (p.Format != 2 && p.Format != 3 && p.Format != 4) || p.ID == "" || len(p.InstallationID) != 32 || len(p.BinarySHA256) != 64 || len(p.Files) > 4096 {
		return errors.New("invalid execution activation plan")
	}
	if taskBackendTransitionKind(p.Kind) {
		if p.Format != 4 || !validSHA256(p.TaskBackendSHA256) || !validSHA256(p.SchedulerSourceSHA256) {
			return errors.New("task requalification lost its exact reviewed backend/scheduler hashes")
		}
	} else if p.TaskBackendSHA256 != "" || p.SchedulerSourceSHA256 != "" {
		return errors.New("unrelated activation acquired backend requalification intent")
	}
	if !historicalSchedulerKind(p.Kind) && len(p.HistoricalSources) != 0 {
		return errors.New("only explicit historical activation may archive historical sources")
	}
	if _, _, err := activationTargetSchemas(p); err != nil {
		return err
	}
	if _, err := uuid.Parse(p.ID); err != nil {
		return err
	}
	parsed, err := uuid.Parse(p.ID)
	if err != nil || parsed.String() != p.ID {
		return errors.New("activation operation UUID is not canonical")
	}
	for _, value := range []string{p.InstallationID, p.BinarySHA256} {
		raw, e := hex.DecodeString(value)
		if e != nil || len(raw) != len(value)/2 || strings.ToLower(value) != value {
			return errors.New("invalid activation identity checksum")
		}
	}
	if !semver.IsValid("v"+p.Version) || len(p.Database) != 2 || len(p.Auxiliary) != 5 {
		return errors.New("incomplete activation source inventory")
	}
	for _, path := range []string{"/var/lib/fluxo/fluxo.db", "/var/lib/fluxo/fluxo.db-wal"} {
		value, ok := p.Database[path]
		if !ok || value != "absent" && len(value) != 64 || path == "/var/lib/fluxo/fluxo.db" && value == "absent" {
			return errors.New("invalid activation database source inventory")
		}
	}
	for _, path := range []string{"/var/lib/fluxo-scheduler.initialized", "/var/lib/fluxo-system-tasks.initialized", "/var/lib/fluxo-scheduler/backend-proofs.json", "/var/lib/fluxo-scheduler/backend-proofs.initialized", "/var/lib/fluxo-scheduler/backend-proofs.initializing"} {
		value, ok := p.Auxiliary[path]
		if !ok || value != "absent" && len(value) != 64 {
			return errors.New("invalid activation coordination source inventory")
		}
	}
	if err := validateActivationFilePlan(p); err != nil {
		return err
	}
	return nil
}

func validateCommittedActivationArchive(dataDir string, floor executiondomain.ReaderFloor) error {
	return validateCommittedActivationArchiveChain(dataDir, floor, map[string]bool{})
}

func validateCommittedActivationArchiveChain(dataDir string, floor executiondomain.ReaderFloor, seen map[string]bool) error {
	if seen[floor.ActivationID] {
		return errors.New("activation source-floor history is cyclic")
	}
	seen[floor.ActivationID] = true
	operation := filepath.Join(dataDir, "execution-activations", floor.ActivationID)
	raw, err := readProtected(filepath.Join(operation, "plan.json"), 128<<20)
	if err != nil {
		return err
	}
	if digest(raw) != floor.PlanSHA256 {
		return errors.New("committed activation plan checksum changed")
	}
	var plan executionActivation
	if err = json.Unmarshal(raw, &plan); err != nil {
		return err
	}
	if err = validateActivationPlan(plan); err != nil {
		return err
	}
	targetScheduler, targetTask, err := activationTargetSchemas(plan)
	if err != nil {
		return err
	}
	if targetScheduler != floor.SchedulerSchema || targetTask != floor.SystemTaskSchema || !activationPlanFloorContractMatches(floor, plan) {
		return errors.New("committed floor changed its activation target schemas")
	}
	if plan.ID != floor.ActivationID || plan.InstallationID != floor.InstallationID || plan.BinarySHA256 != floor.BinarySHA256 {
		return errors.New("committed activation archive identity changed")
	}
	image, err := activationProtectedHash(filepath.Join(operation, "image"), 256<<20)
	if err != nil || image != floor.BinarySHA256 {
		return errors.New("committed activation image was not retained")
	}
	for i, change := range plan.Files {
		if change.Before != "absent" {
			old, e := activationProtectedHash(filepath.Join(operation, fmt.Sprintf("before-%d", i)), 256<<20)
			if e != nil || old != change.Before {
				return errors.New("committed activation source snapshot was not retained")
			}
		}
	}
	if err = validateActivationSources(operation, plan); err != nil {
		return err
	}
	if plan.Kind == activationReaderUpgrade || plan.Kind == activationTaskHelperRefresh || plan.Kind == activationReader7Upgrade || taskBackendTransitionKind(plan.Kind) {
		source, err := executiondomain.ParseReaderFloor(plan.SourceFloor)
		if err != nil {
			return err
		}
		return validateCommittedActivationArchiveChain(dataDir, source, seen)
	}
	return nil
}

func validateActivationSources(operation string, p executionActivation) error {
	var tasksOriginal, tasksIntended []byte
	tasksPlanned := false
	for i, change := range p.Files {
		if change.Path != "/var/lib/fluxo-scheduler/state.json" && change.Path != "/var/lib/fluxo-system-tasks/state.json" {
			continue
		}
		var original []byte
		if change.Before != "absent" {
			var err error
			original, err = readProtected(filepath.Join(operation, fmt.Sprintf("before-%d", i)), 32<<20)
			if err != nil {
				return err
			}
		}
		if change.Path == "/var/lib/fluxo-scheduler/state.json" {
			var err error
			if historicalSchedulerKind(p.Kind) {
				err = scheduler.ValidateHistoricalExecutionActivationPlan(original, change.Data, p.ID)
				if err == nil {
					err = scheduler.ValidateHistoricalActivationSources(original, p.HistoricalSources)
				}
			} else {
				err = scheduler.ValidateExecutionActivationPlanForSchema(original, change.Data, activationSchedulerTarget(p))
			}
			if err != nil {
				return err
			}
		} else {
			tasksOriginal, tasksIntended = original, change.Data
			tasksPlanned = true
		}
	}
	if !tasksPlanned && len(p.HistoricalTaskRetirements) > 0 {
		return errors.New("historical task retirement lost its original core")
	}
	if tasksPlanned {
		if taskBackendTransitionKind(p.Kind) {
			if p.Kind == activationInstallerTaskBackendRenewal {
				return maintenancetask.ValidateInstallerRegistrationRenewalPlan(tasksOriginal, tasksIntended, p.TaskBackendSHA256, p.Files)
			}
			return maintenancetask.ValidateRegistrationRequalificationPlan(tasksOriginal, tasksIntended, p.TaskBackendSHA256, p.Files)
		}
		if p.Kind == activationTaskHelperRefresh {
			var header struct {
				Schema int `json:"schema"`
			}
			if len(tasksOriginal) == 0 || json.Unmarshal(tasksOriginal, &header) != nil || header.Schema != executiondomain.CurrentSystemTaskSchema {
				return errors.New("task helper refresh cannot initialize or convert historical task state")
			}
		}
		if len(p.HistoricalTaskRetirements) > 0 {
			return maintenancetask.ValidateInstallerHistoricalActivationPlan(tasksOriginal, tasksIntended, p.BinarySHA256, p.Files, p.HistoricalTaskRetirements)
		}
		return maintenancetask.ValidateExecutionActivationPlan(tasksOriginal, tasksIntended, p.BinarySHA256, p.Files)
	}
	if p.Kind == activationTaskHelperRefresh || taskBackendTransitionKind(p.Kind) {
		return errors.New("task helper refresh lost its required task-core transition")
	}
	for _, change := range p.Files {
		if change.Cron || filepath.Dir(change.Path) == "/var/lib/fluxo-system-tasks/helpers" {
			return errors.New("activation has helpers/triggers without their owning journal transition")
		}
	}
	return nil
}

func activationIntended(change executiondomain.ActivationFile, imageSHA string) string {
	if change.Image {
		return imageSHA
	}
	return digest(change.Data)
}
func validateActivationCurrent(change executiondomain.ActivationFile, imageSHA string) error {
	if change.Directory {
		info, err := os.Lstat(change.Path)
		if os.IsNotExist(err) && change.Before == "absent" {
			return nil
		}
		if err != nil {
			return err
		}
		if err = trustedDirectory(change.Path); err != nil {
			return err
		}
		if info.Mode().Perm() != os.FileMode(change.Mode) {
			return errors.New("activation created directory mode changed")
		}
		return nil
	}

	raw, err := readProtected(change.Path, 256<<20)
	if os.IsNotExist(err) && change.Before == "absent" {
		return nil
	}
	if err != nil {
		return err
	}
	hash := digest(raw)
	if hash != change.Before && hash != activationIntended(change, imageSHA) && (len(change.Fence) == 0 || hash != digest(change.Fence)) {
		return fmt.Errorf("activation target changed outside its exact operation: %s", change.Path)
	}
	return nil
}
func validateActivationIntended(change executiondomain.ActivationFile, imageSHA string) error {
	if change.Directory {
		if _, err := os.Lstat(change.Path); err != nil {
			return err
		}
		return validateActivationCurrent(change, imageSHA)
	}

	raw, err := readProtected(change.Path, 256<<20)
	if err != nil {
		return err
	}
	if digest(raw) != activationIntended(change, imageSHA) {
		return errors.New("activation intended bytes failed readback")
	}
	if change.Cron {
		info, err := protectedInfo(change.Path)
		if err != nil {
			return err
		}
		if info.ModTime().Unix() != change.CronMTime {
			return errors.New("cron trigger mtime was not durably replaced")
		}
	}
	return nil
}
func applyActivationFile(change executiondomain.ActivationFile, image []byte) error {
	if change.Directory {
		if err := validateActivationCurrent(change, digest(image)); err != nil {
			return err
		}
		if err := os.Mkdir(change.Path, os.FileMode(change.Mode)); err != nil && !os.IsExist(err) {
			return err
		}
		return syncActivationDirectory(filepath.Dir(change.Path))
	}

	if err := validateActivationCurrent(change, digest(image)); err != nil {
		return err
	}
	if change.PublishFence {
		raw, err := readProtected(change.Path, 4096)
		if err == nil && (bytes.Equal(raw, change.Data) || bytes.Equal(raw, change.Fence)) {
			return nil
		}
		if !os.IsNotExist(err) {
			if err != nil {
				return err
			}
			return errors.New("activation creation anchor changed outside exact operation")
		}
		return writeActivationFence(change.Path, change.Fence)
	}
	data := change.Data
	if change.Image {
		data = image
	}
	if err := writeActivationFile(change.Path, data, os.FileMode(change.Mode)); err != nil {
		return err
	}
	if change.Cron {
		at := time.Unix(change.CronMTime, 0)
		if err := os.Chtimes(change.Path, at, at); err != nil {
			return err
		}
		f, err := os.Open(change.Path)
		if err != nil {
			return err
		}
		err = f.Sync()
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
func writeActivationJSON(path string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeActivationFile(path, raw, 0600)
}
func writeActivationFile(path string, data []byte, mode os.FileMode) error {
	if err := trustedDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".execution-activation-*")
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
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncActivationDirectory(filepath.Dir(path))
}
func syncActivationDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func activationProtectedHash(path string, limit int64) (string, error) {
	if err := trustedDirectory(filepath.Dir(path)); err != nil {
		return "", err
	}
	before, err := protectedInfo(path)
	if err != nil {
		return "", err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !sameObservation(before, opened) {
		return "", errors.New("activation hash source changed before read")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(file, limit+1))
	if err != nil {
		return "", err
	}
	if n > limit {
		return "", errors.New("activation hash source exceeded read budget")
	}
	after, err := file.Stat()
	if err != nil {
		return "", err
	}
	named, err := protectedInfo(path)
	if err != nil || !sameObservation(before, after) || !sameObservation(before, named) {
		return "", errors.New("activation hash source changed during read")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Recovery grants authority only to this installation, operation, executable and
// immutable intent. Even a newer otherwise-compatible helper cannot resume an
// interrupted conversion under a different binary identity.
func validateActivationRecovery(plan executionActivation, resume, installationID, imageSHA, version string, floor *executiondomain.ReaderFloor, planBytes []byte) error {
	if plan.ID != resume || plan.InstallationID != installationID || plan.BinarySHA256 != imageSHA || plan.Version != version {
		return errors.New("activation recovery identity or executing image changed; preserve the original operation")
	}
	if activationSourceFloorMatches(plan, floor) {
		return nil
	}
	if floor != nil && (floor.Phase != "prepared" || floor.ActivationID != plan.ID || floor.InstallationID != installationID || floor.BinarySHA256 != imageSHA || floor.PlanSHA256 != digest(planBytes)) {
		return errors.New("prepared reader floor does not match the exact activation plan")
	}
	return nil
}
