package bridge

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/phpupdate"
	"fluxo/internal/services/scheduler"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
	"golang.org/x/mod/semver"
	"golang.org/x/sys/unix"
)

const installerExecutionRecord = "execution-installer.json"

// InstallerExecutionStatus is read-only, including when the reader floor is
// prepared. It reveals only the exact artifact needed to resume orchestration.
func InstallerExecutionStatus(dataDir string) (any, error) {
	if os.Geteuid() != 0 || dataDir != "/var/lib/fluxo" {
		return nil, errors.New("installer status requires the installed root scope")
	}
	v, err := readInstallerExecutionIntent(filepath.Join(dataDir, installerExecutionRecord))
	if err != nil {
		return nil, err
	}
	policy, err := readInstallerSchedulerIntent(dataDir)
	if err != nil {
		return nil, err
	}
	if installerSchedulerPending(policy) {
		if v != nil && v.Phase != "complete" && (v.Image != policy.Image || v.Version != policy.Version) {
			return nil, errors.New("installer recovery records disagree about their required image")
		}
		return map[string]any{"pending": true, "image_sha256": policy.Image, "version": policy.Version}, nil
	}
	if v == nil {
		return map[string]any{"pending": false}, nil
	}
	return map[string]any{"pending": v.Phase != "complete", "image_sha256": v.Image, "version": v.Version}, nil
}

// This record is installer orchestration, not execution authority. Only the
// existing exact-image activation archive and committed reader floor grant
// format authority. In particular, completion does not certify this host.
type installerExecutionIntent struct {
	Format         int    `json:"format"`
	Image          string `json:"image_sha256"`
	Version        string `json:"version"`
	InstallationID string `json:"installation_id"`
	Service        string `json:"service_sha256"`
	WasActive      bool   `json:"was_active"`
	Operation      string `json:"operation_id"`
	Plan           string `json:"plan_sha256"`
	Phase          string `json:"phase"`
	TaskCore       string `json:"task_core_sha256,omitempty"`
	TaskBackend    string `json:"task_backend_sha256,omitempty"`
}

// InstallExecutionFormats is the normal installer's one-time format phase. It
// owns lifecycle admission while stopping the panel; activation still acquires
// package, native DPKG, coordination and journal gates in their ordinary order.
// The installer must have committed its binary/database rollback beforehand.
func InstallExecutionFormats(ctx context.Context, dataDir, version string, complete bool) (ExecutionActivationReport, error) {
	report, err := installExecutionFormats(ctx, dataDir, version, complete)
	if err != nil {
		return report, err
	}
	policy, err := readInstallerSchedulerIntent(dataDir)
	if err != nil {
		return report, err
	}
	if installerSchedulerPending(policy) {
		// Finish the recorded Scheduler operation against its original floor
		// before a fresh task review can replace that floor or require a panel
		// which this very recovery must first restore.
		if err = installSchedulerPolicy(ctx, dataDir, version, complete); err != nil {
			return report, err
		}
		if complete {
			// The shell has checked dashboard/domain health before completion.
			// Close an older committed reader intent now that Scheduler no longer
			// owns restart. A second ordinary pass may then renew changed tasks.
			return installExecutionFormats(ctx, dataDir, version, true)
		}
		return report, nil
	}
	if err = maintenancetask.WaitActivationAcknowledgements(ctx); err != nil {
		return report, err
	}
	return report, installSchedulerPolicy(ctx, dataDir, version, complete)
}

func installExecutionFormats(ctx context.Context, dataDir, version string, complete bool) (ExecutionActivationReport, error) {
	return retryInstallerReadOnlyChecks(ctx, func() (ExecutionActivationReport, error) {
		return installExecutionFormatsOnce(ctx, dataDir, version, complete)
	})
}

func retryInstallerReadOnlyChecks(ctx context.Context, attempt func() (ExecutionActivationReport, error)) (ExecutionActivationReport, error) {
	for {
		report, err := attempt()
		if !errors.Is(err, phpupdate.ErrInstallerChecksPending) {
			return report, err
		}
		// The attempt's deferred lifecycle release has completed before this
		// wait, allowing a queued Node check to acquire its shared admission.
		select {
		case <-ctx.Done():
			return report, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func installExecutionFormatsOnce(ctx context.Context, dataDir, version string, complete bool) (ExecutionActivationReport, error) {
	report := ExecutionActivationReport{RecoveryPolicy: "retain-current-and-historical-live-recovery"}
	if os.Geteuid() != 0 || dataDir != "/var/lib/fluxo" || !semver.IsValid("v"+version) {
		return report, errors.New("installer reader setup requires root, the installed production directory and a versioned image")
	}
	if exe, err := os.Executable(); err != nil || exe != "/usr/local/bin/fluxo" {
		return report, errors.New("installer reader setup must use the installed /usr/local/bin/fluxo")
	}
	image, err := readCertifyingExecutable()
	if err != nil {
		return report, err
	}
	imageSHA := digest(image)
	if floor, e := executiondomain.ReadFloor(dataDir); e != nil {
		return report, e
	} else if floor == nil {
		// First-boot acknowledgements do not require lifecycle admission. Keep
		// normal startup check workers free to finish during this read-only wait.
		if err = maintenancetask.WaitActivationAcknowledgements(ctx); err != nil {
			return report, err
		}
	}
	release, err := packages.Lifecycle(ctx, false)
	if err != nil {
		return report, err
	}
	defer release()
	service, active, err := installerPanelBinding(ctx, dataDir, imageSHA)
	if err != nil {
		return report, err
	}
	installationID, err := installerInstallationID(ctx, dataDir)
	if err != nil {
		return report, err
	}
	path := filepath.Join(dataDir, installerExecutionRecord)
	intent, err := readInstallerExecutionIntent(path)
	if err != nil {
		return report, err
	}
	policy, err := readInstallerSchedulerIntent(dataDir)
	if err != nil {
		return report, err
	}
	if installerSchedulerPending(policy) {
		if policy.Image != imageSHA || policy.Version != version || policy.Service != service || policy.InstallationID != installationID {
			return report, errors.New("pending Scheduler recovery requires its exact installed image, service and installation")
		}
		if _, err = executiondomain.InspectCompatibility(dataDir); err != nil {
			return report, err
		}
		floor, e := executiondomain.ReadFloor(dataDir)
		if e != nil || floor == nil || floor.Phase != "committed" || floor.InstallationID != installationID {
			return report, errors.New("pending Scheduler recovery requires its existing committed reader floor")
		}
		raw, e := readProtected(filepath.Join(dataDir, "execution-readers.json"), 64<<10)
		if e != nil || digest(raw) != policy.Floor {
			return report, errors.New("pending Scheduler recovery cannot replace its original reader floor")
		}
		if e = validateCommittedActivationArchive(dataDir, *floor); e != nil {
			return report, e
		}
		if intent != nil && intent.Phase != "complete" {
			if intent.Phase != "committed" || intent.Image != imageSHA || intent.Version != version || intent.Service != service || intent.InstallationID != installationID {
				return report, errors.New("reader and Scheduler recovery records conflict")
			}
			if e = verifyInstallerCommitted(dataDir, *intent); e != nil {
				return report, e
			}
		}
		return ExecutionActivationReport{OperationID: floor.ActivationID, Phase: "committed", FormatActive: true, RecoveryPolicy: report.RecoveryPolicy}, nil
	}
	if intent != nil && intent.Phase != "complete" {
		if intent.Image != imageSHA || intent.Version != version || intent.Service != service || intent.InstallationID != installationID {
			return report, fmt.Errorf("unfinished installer reader setup requires its original image %s (%s) and service; rerun that exact release", intent.Image, intent.Version)
		}
	} else {
		var taskReview *maintenancetask.RegistrationBackendReview
		if _, err = executiondomain.InspectCompatibility(dataDir); err != nil {
			return report, err
		}
		floor, e := executiondomain.ReadFloor(dataDir)
		if e != nil {
			return report, e
		}
		// Retain the exact reader6/reader7 contract of an already current floor.
		// Supported task requalification retains that reader contract through
		// its existing archived transition; it never advances the generation.
		if floor != nil && floor.Phase == "committed" && floor.SchedulerSchema == 5 && floor.SystemTaskSchema == 3 {
			if floor.InstallationID != installationID {
				return report, errors.New("reader floor belongs to another installation")
			}
			if e = validateCommittedActivationArchive(dataDir, *floor); e != nil {
				return report, e
			}
			if !complete {
				unlock, e := lockCoordination(ctx)
				if e != nil {
					return report, e
				}
				taskReview, e = maintenancetask.InstallerRegistrationReviewLocked(ctx)
				unlock()
				if e != nil {
					return report, e
				}
			}
			if taskReview == nil {
				return ExecutionActivationReport{OperationID: floor.ActivationID, Phase: "committed", FormatActive: true, RecoveryPolicy: report.RecoveryPolicy}, nil
			}
		}
		if complete {
			return report, errors.New("no committed installer reader setup to complete")
		}
		if floor != nil && taskReview == nil {
			return report, errors.New("existing reader generation requires its explicit reviewed upgrade; installer preserves its floor")
		}
		if !active {
			return report, errors.New("start the compatible installed panel before first installer reader setup")
		}
		// Initial Noble registrations can publish before the first reader floor.
		// Observe their genuine cron receipts before readiness admission requires
		// those receipts; lifecycle exclusion prevents new registration writers.
		if e = installerExecutionAdmission(taskReview == nil,
			func() error { return maintenancetask.WaitActivationAcknowledgements(ctx) },
			func() error { return phpupdate.CheckInstallerUpgradeDatabase(ctx, dataDir) }); e != nil {
			return report, e
		}
		if e = scheduler.VerifyPanel(ctx); e != nil {
			return report, e
		}
		intent = &installerExecutionIntent{Format: 1, Image: imageSHA, Version: version, InstallationID: installationID, Service: service, WasActive: active, Phase: "pending"}
		if taskReview != nil {
			intent.TaskCore, intent.TaskBackend = taskReview.TaskCoreSHA256, taskReview.CurrentBackendSHA256
		}
		if e = writeActivationJSON(path, intent); e != nil {
			return report, e
		}
	}
	report.OperationID = intent.Operation
	if complete {
		if intent.Phase != "committed" || intent.WasActive && !active {
			return report, errors.New("installer reader setup has not restored its running panel")
		}
		if err = verifyInstallerCommitted(dataDir, *intent); err != nil {
			return report, err
		}
		intent.Phase = "complete"
		if err = writeActivationJSON(path, intent); err != nil {
			return report, err
		}
		report.Phase, report.FormatActive = "committed", true
		return report, nil
	}
	if intent.Phase == "committed" {
		if err = verifyInstallerCommitted(dataDir, *intent); err != nil {
			return report, err
		}
	} else {
		if intent.Operation == "" {
			if _, err = executiondomain.InspectCompatibility(dataDir); err != nil {
				return report, err
			}
			floor, e := executiondomain.ReadFloor(dataDir)
			if e != nil {
				return report, e
			}
			if floor != nil && intent.TaskCore == "" || floor == nil && intent.TaskCore != "" {
				return report, errors.New("reader floor changed before installer operation preparation")
			}
			if err = installerExecutionAdmission(intent.TaskCore == "",
				func() error { return maintenancetask.WaitActivationAcknowledgements(ctx) },
				func() error { return phpupdate.CheckInstallerUpgradeDatabase(ctx, dataDir) }); err != nil {
				return report, err
			}
			if active {
				if err = scheduler.VerifyPanel(ctx); err != nil {
					return report, err
				}
			}
		} else if active {
			// An exact pending operation never authorizes stopping an unrelated
			// running panel. Drain its known manual boundary before the stop.
			if _, err = scheduler.PanelBeforeStop(ctx); err != nil {
				return report, err
			}
		}
		if active {
			if _, err = syscmd.Run(ctx, 30*time.Second, "/usr/bin/systemctl", "stop", "fluxo.service"); err != nil {
				return report, err
			}
		}
		if err = panelStopped(ctx); err != nil {
			return report, err
		}
		// Before an operation is published, failure may restart the unchanged
		// compatible panel. Once the exact UUID is durable, leave it stopped.
		defer func() {
			if err != nil && intent.Operation == "" && intent.WasActive {
				_, _ = syscmd.Run(context.Background(), 30*time.Second, "/usr/bin/systemctl", "start", "fluxo.service")
			}
		}()
		request := activationRequest{LifecycleOwned: true, Resume: intent.Operation, InstallerHistoricalTasks: true}
		if intent.Operation != "" {
			if err = prepareInstallerRecovery(dataDir, *intent); err != nil {
				return report, err
			}
		} else {
			if intent.TaskCore != "" {
				request.ExpectedTaskCore, request.TaskBackendSHA256 = intent.TaskCore, intent.TaskBackend
				request.InstallerTaskRenewal = true
			} else {
				request.Historical, err = installerHistoricalSource()
				if err != nil {
					return report, err
				}
			}
			request.InstallerPrepared = func(plan executionActivation, raw []byte) error {
				if plan.InstallationID != intent.InstallationID {
					return errors.New("installation changed during installer activation planning")
				}
				intent.Operation, intent.Plan, intent.InstallationID = plan.ID, digest(raw), plan.InstallationID
				return writeActivationJSON(path, intent)
			}
		}
		report, err = activateExecutionFormats(ctx, dataDir, version, request)
		if err != nil {
			return report, err
		}
		intent.Phase = "committed"
		if err = writeActivationJSON(path, intent); err != nil {
			return report, err
		}
	}
	policy, policyErr := readInstallerSchedulerIntent(dataDir)
	if policyErr != nil {
		return report, policyErr
	}
	if installerSchedulerPending(policy) {
		// The next installer phase owns restart after its exact pending loader
		// publication. Do not start background reconciliation between phases.
		report.Phase, report.FormatActive = "committed", true
		return report, nil
	}
	if intent.WasActive {
		// Revalidate named/executing image and effective unit before restarting.
		binding, _, e := installerPanelBinding(ctx, dataDir, imageSHA)
		if e != nil || binding != intent.Service {
			return report, errors.New("installed image or service changed before restart")
		}
		if _, err = syscmd.Run(ctx, 30*time.Second, "/usr/bin/systemctl", "start", "fluxo.service"); err != nil {
			return report, err
		}
	}
	report.Phase, report.FormatActive = "committed", true
	return report, nil
}

// Waiting is read-only. The complete existing domain/journal/database admission
// still runs afterward and may refuse live, uncertain or changed authority.
// An explicit task-backend review retains its separate receipt/retirement path.
func installerExecutionAdmission(needsReceipts bool, await, check func() error) error {
	if needsReceipts {
		if err := await(); err != nil {
			return err
		}
	}
	return check()
}

func readInstallerExecutionIntent(path string) (*installerExecutionIntent, error) {
	raw, err := readProtected(path, 16<<10)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeInstallerExecutionIntent(raw)
}

func decodeInstallerExecutionIntent(raw []byte) (*installerExecutionIntent, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	for _, key := range []string{"format", "image_sha256", "version", "installation_id", "service_sha256", "was_active", "operation_id", "plan_sha256", "phase"} {
		if len(fields[key]) == 0 || bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return nil, errors.New("installer reader intent lacks explicit authority fields")
		}
	}
	for _, key := range []string{"task_core_sha256", "task_backend_sha256"} {
		if value, present := fields[key]; present && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, errors.New("installer task-backend expectation cannot be null")
		}
	}
	var v installerExecutionIntent
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errors.New("installer reader intent has trailing data")
	}
	validHash := func(s string) bool {
		b, e := hex.DecodeString(s)
		return e == nil && len(b) == 32 && strings.ToLower(s) == s
	}
	if v.Format != 1 || !validHash(v.Image) || !validHash(v.Service) || !semver.IsValid("v"+v.Version) || v.Phase != "pending" && v.Phase != "committed" && v.Phase != "complete" {
		return nil, errors.New("invalid installer reader intent")
	}
	installation, installationErr := hex.DecodeString(v.InstallationID)
	if installationErr != nil || len(installation) != 16 || strings.ToLower(v.InstallationID) != v.InstallationID {
		return nil, errors.New("invalid installer installation identity")
	}
	if v.Operation == "" {
		if v.Plan != "" || v.Phase != "pending" {
			return nil, errors.New("installer intent lost its operation")
		}
	} else {
		id, e := uuid.Parse(v.Operation)
		installation, e2 := hex.DecodeString(v.InstallationID)
		if e != nil || id.String() != v.Operation || !validHash(v.Plan) || e2 != nil || len(installation) != 16 || strings.ToLower(v.InstallationID) != v.InstallationID {
			return nil, errors.New("invalid exact installer recovery identity")
		}
	}
	if v.TaskCore != "" || v.TaskBackend != "" {
		if !validHash(v.TaskCore) || !validHash(v.TaskBackend) {
			return nil, errors.New("invalid installer task-backend expectations")
		}
	}
	return &v, nil
}

func installerInstallationID(ctx context.Context, dataDir string) (string, error) {
	db, err := open(ctx, dataDir, false)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var id string
	err = db.QueryRowContext(ctx, "SELECT value FROM system_metadata WHERE key='installation_id'").Scan(&id)
	return id, err
}

func installerHistoricalSource() (bool, error) {
	raw, err := readProtected("/var/lib/fluxo-scheduler/state.json", 32<<20)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var header struct {
		Schema int               `json:"schema"`
		Legacy map[string]string `json:"legacy"`
	}
	if err = json.Unmarshal(raw, &header); err != nil {
		return false, err
	}
	return header.Schema < 5 && len(header.Legacy) != 0, nil
}

func installerPanelBinding(ctx context.Context, dataDir, imageSHA string) (string, bool, error) {
	image, err := readCertifyingExecutable()
	if err != nil || digest(image) != imageSHA {
		return "", false, errors.New("installed executable changed during installer reader setup")
	}
	return installedPanelServiceBinding(ctx, dataDir, imageSHA)
}

// The caller authenticates the named installed executable. A staged recovery
// candidate additionally authenticates its own executing image via its handoff.
func installedPanelServiceBinding(ctx context.Context, dataDir, imageSHA string) (string, bool, error) {
	out, err := run(ctx, "/usr/bin/systemctl", "show", "fluxo.service", "--property=LoadState,NeedDaemonReload,User,DynamicUser,RootDirectory,RootImage,BindPaths,BindReadOnlyPaths,TemporaryFileSystem,MountImages,ExtensionImages,ExtensionDirectories,InaccessiblePaths,ReadOnlyPaths,ReadWritePaths,ProtectHome,ProtectSystem,PrivateUsers,FragmentPath,DropInPaths,ExecStart,Environment,EnvironmentFiles,PassEnvironment,UnsetEnvironment,ActiveState,MainPID")
	if err != nil {
		return "", false, err
	}
	p := properties(out)
	if err = serviceContext(p, dataDir, os.Getenv("FLUXO_USE_HTTP") == "1"); err != nil {
		return "", false, err
	}
	active := p["ActiveState"] == "active"
	if !active && (p["ActiveState"] != "inactive" || p["MainPID"] != "0") {
		return "", false, errors.New("panel service is transitioning or failed; preserve its state")
	}
	if active {
		pid, e := strconv.Atoi(p["MainPID"])
		if e != nil || pid <= 1 {
			return "", false, errors.New("invalid running panel identity")
		}
		file, e := os.Open(fmt.Sprintf("/proc/%d/exe", pid))
		if e != nil {
			return "", false, e
		}
		raw, e := io.ReadAll(io.LimitReader(file, 128<<20))
		file.Close()
		if e != nil || digest(raw) != imageSHA {
			return "", false, errors.New("running panel must use the installed installer image")
		}
	}
	unit, err := readProtected("/etc/systemd/system/fluxo.service", 64<<10)
	if err != nil {
		return "", false, err
	}
	// ExecStart's systemctl representation includes runtime timestamps; its
	// admitted executable and exact protected unit bytes bind the static part.
	delete(p, "ExecStart")
	delete(p, "ActiveState")
	delete(p, "MainPID")
	p["unit_sha256"] = digest(unit)
	raw, err := json.Marshal(p)
	return digest(raw), active, err
}

func installerStagedArchive(dataDir, id string) string {
	return filepath.Join(dataDir, ".execution-installer-"+id)
}

func ensureActivationParent(parent string) error {
	if err := os.Mkdir(parent, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	if err := trustedDirectory(parent); err != nil {
		return err
	}
	return syncActivationDirectory(filepath.Dir(parent))
}

func publishInstallerArchive(dataDir, id string) error {
	parent := filepath.Join(dataDir, "execution-activations")
	if err := ensureActivationParent(parent); err != nil {
		return err
	}
	if err := unix.Renameat2(unix.AT_FDCWD, installerStagedArchive(dataDir, id), unix.AT_FDCWD, filepath.Join(parent, id), unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	if err := syncActivationDirectory(parent); err != nil {
		return err
	}
	return syncActivationDirectory(dataDir)
}

// An archive is fully durable before its pointer is recorded. Moving that
// complete directory is atomic, so recovery never invents missing source bytes,
// nonces or a replacement operation after a crash during archive construction.
func prepareInstallerRecovery(dataDir string, intent installerExecutionIntent) error {
	final := filepath.Join(dataDir, "execution-activations", intent.Operation)
	stage := installerStagedArchive(dataDir, intent.Operation)
	dir := final
	if _, err := os.Lstat(final); os.IsNotExist(err) {
		dir = stage
	} else if err != nil {
		return err
	}
	if err := trustedDirectory(dir); err != nil {
		return err
	}
	raw, err := readProtected(filepath.Join(dir, "plan.json"), 128<<20)
	if err != nil {
		return err
	}
	if err = validateInstallerPlan(intent, raw); err != nil {
		return err
	}
	image, err := readProtected(filepath.Join(dir, "image"), 128<<20)
	if err != nil || digest(image) != intent.Image {
		return errors.New("installer activation image archive changed")
	}
	if dir == stage {
		return publishInstallerArchive(dataDir, intent.Operation)
	}
	if _, err = os.Lstat(stage); !os.IsNotExist(err) {
		return errors.New("conflicting staged and published installer archives")
	}
	return nil
}

func validateInstallerPlan(intent installerExecutionIntent, raw []byte) error {
	var plan executionActivation
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&plan); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("installer archive has trailing data")
	}
	if digest(raw) != intent.Plan || plan.ID != intent.Operation || plan.BinarySHA256 != intent.Image || plan.Version != intent.Version || plan.InstallationID != intent.InstallationID || plan.Kind != activationFresh && !historicalSchedulerKind(plan.Kind) && !installerHistoricalTaskKind(plan.Kind) && !taskBackendTransitionKind(plan.Kind) {
		return errors.New("installer recovery cannot adopt another image, installation or operation")
	}
	if (taskBackendTransitionKind(plan.Kind)) != (intent.TaskCore != "") || plan.TaskBackendSHA256 != intent.TaskBackend {
		return errors.New("installer task recovery kind or qualification changed")
	}
	if intent.TaskCore != "" {
		matched := false
		for _, f := range plan.Files {
			if f.Path == "/var/lib/fluxo-system-tasks/state.json" {
				matched = f.Before == intent.TaskCore
			}
		}
		if !matched {
			return errors.New("installer task recovery source core changed")
		}
	}
	return validateActivationPlan(plan)
}

func verifyInstallerCommitted(dataDir string, intent installerExecutionIntent) error {
	if _, err := executiondomain.InspectCompatibility(dataDir); err != nil {
		return err
	}
	floor, err := executiondomain.ReadFloor(dataDir)
	if err != nil {
		return err
	}
	if floor == nil || floor.Phase != "committed" || floor.ActivationID != intent.Operation || floor.PlanSHA256 != intent.Plan || floor.BinarySHA256 != intent.Image || floor.InstallationID != intent.InstallationID {
		return errors.New("installer completion requires its exact committed floor")
	}
	return validateCommittedActivationArchive(dataDir, *floor)
}
