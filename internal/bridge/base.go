package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fluxo/internal/services/phpupdate"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/scheduler"
)

const BaseContract = "fluxo.base.v1"
const baseRecord = "base-admission.json"
const BridgeInstallCommand = "curl -fsSL https://github.com/FabioTECH1/fluxo/releases/download/v0.4.51/install.sh | sudo env FLUXO_VERSION=v0.4.51 bash"

type BaseAdmission struct {
	Contract       string `json:"contract"`
	Mode           string `json:"mode"`
	InstallationID string `json:"installation_id,omitempty"`
	KeySHA256      string `json:"key_sha256,omitempty"`
}

// BaseInspect is read-only. Completion is scoped to the supported data and
// execution contracts, not a certification of every host/runtime profile.
func BaseInspect(ctx context.Context, dataDir string) (BaseAdmission, error) {
	return retryBaseInspection(ctx, func() (BaseAdmission, error) { return baseInspectOnce(ctx, dataDir) }, scheduler.InstallerCronDrainPending)
}

// Only the existing typed natural-drain observation is retryable. Every retry
// repeats the complete read-only admission; no pending authority is cleared.
func retryBaseInspection(ctx context.Context, attempt func() (BaseAdmission, error), retryable func(error) bool) (BaseAdmission, error) {
	for {
		report, err := attempt()
		if !retryable(err) {
			return report, err
		}
		select {
		case <-ctx.Done():
			return report, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func baseInspectOnce(ctx context.Context, dataDir string) (BaseAdmission, error) {
	r := BaseAdmission{Contract: BaseContract}
	if os.Geteuid() != 0 {
		return r, errors.New("base admission requires root")
	}
	raw, err := readProtected(filepath.Join(dataDir, baseRecord), 4096)
	if err == nil {
		r, err = decodeBaseAdmission(raw)
		if err != nil {
			return r, err
		}
		// A rolled-back or interrupted handoff still running the predecessor
		// must repeat bridge admission, even if the identity receipt survived.
		if verifyPublishedBridge() == nil {
			r.Mode = "bridge"
			return r, validateBaseState(ctx, dataDir, r, true, "")
		}
		if e := validateBaseState(ctx, dataDir, r, false, ""); e != nil {
			if recoveryErr := validateBaseRecovery(ctx, dataDir, r); recoveryErr != nil {
				return r, e
			}
			r.Mode = "base-recovery"
		}
		return r, nil
	}
	if !os.IsNotExist(err) {
		return r, err
	}
	if intent, e := BaseFreshStatus(dataDir); e != nil {
		return r, e
	} else if intent != nil {
		image, e := readCertifyingExecutable()
		if e != nil {
			return r, e
		}
		if e = validatePendingFreshOrigin(ctx, dataDir, *intent); e != nil {
			return r, e
		}
		if intent.Phase != "pending" {
			if e = requireFreshImage(*intent, digest(image), intent.Version); e != nil {
				return r, e
			}
		}
		r.Mode = "fresh-pending"
		return r, nil
	}
	if baseArtifactsExist(dataDir) {
		r.Mode = "bridge"
		return r, validateBaseState(ctx, dataDir, r, true, "")
	}
	r.Mode = "fresh"
	return r, nil
}

// Absence includes dangling links and incomplete subsystem/service remnants.
// A damaged installation must never acquire a new identity or replacement key.
func baseArtifactsExist(dataDir string) bool {
	if freshRuntimeAbsent(os.Lstat, filepath.Glob) != nil {
		return true
	}
	if freshAccountAbsent() != nil {
		return true
	}
	for _, p := range []string{dataDir, "/usr/local/bin/fluxo", "/etc/systemd/system/fluxo.service", "/etc/systemd/system/fluxo.service.d", "/var/lib/fluxo-scheduler", "/var/lib/fluxo-scheduler.initialized", "/var/lib/fluxo-scheduler.init.lock", "/var/lib/fluxo-system-tasks", "/var/lib/fluxo-system-tasks.initialized", "/var/lib/fluxo-system-tasks.lock", "/home/fluxo", "/etc/sudoers.d/fluxo"} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			return true
		}
	}
	return false
}

func validateBaseState(ctx context.Context, dataDir string, saved BaseAdmission, handoff bool, lease string) error {
	path, cleanup, err := snapshot(ctx, dataDir)
	if err != nil {
		return err
	}
	defer cleanup()
	db, err := openSQLite(ctx, path, false)
	if err != nil {
		return err
	}
	defer db.Close()
	if err = checkSchema(ctx, db); err != nil {
		return err
	}
	var id string
	if err = db.QueryRowContext(ctx, "SELECT value FROM system_metadata WHERE key='installation_id'").Scan(&id); err != nil {
		return err
	}
	if len(id) != 32 || !installerSchedulerDigest(id+id) {
		return errors.New("restore the original installation identity")
	}
	key, err := readKey(dataDir)
	if err != nil {
		return err
	}
	if !handoff && (saved.InstallationID != id || saved.KeySHA256 != digest(key)) {
		return errors.New("base installation identity or encryption key changed")
	}
	if _, err = executiondomain.InspectCompatibility(dataDir); err != nil {
		return err
	}
	floor, err := executiondomain.ReadFloor(dataDir)
	if err != nil {
		return err
	}
	if floor == nil || floor.Phase != "committed" || floor.InstallationID != id || floor.SchedulerSchema != 5 || floor.SystemTaskSchema != 3 {
		return errors.New("complete the supported bridge's execution setup before installing the base")
	}
	if err = validateCommittedActivationArchive(dataDir, *floor); err != nil {
		return err
	}
	if !handoff {
		return nil
	} // Ordinary boots must remain able to recover interrupted work.
	if err = verifyPublishedBridge(); err != nil {
		return err
	}
	out, e := run(ctx, "/usr/bin/systemctl", "show", "fluxo.service", "--property=Environment")
	if e != nil {
		return e
	}
	assignments, e := serviceAssignments(properties(out)["Environment"])
	if e != nil {
		return e
	}
	http := false
	for _, a := range assignments {
		if a == "FLUXO_USE_HTTP=1" {
			http = true
		}
	}
	// The installed predecessor must own the effective service and any live PID.
	originalHTTP := os.Getenv("FLUXO_USE_HTTP")
	if http {
		os.Setenv("FLUXO_USE_HTTP", "1")
	} else {
		os.Unsetenv("FLUXO_USE_HTTP")
	}
	_, _, e = installedPanelServiceBinding(ctx, dataDir, publishedBridgeSHA())
	os.Setenv("FLUXO_USE_HTTP", originalHTTP)
	if e != nil {
		return e
	}
	intent, err := readInstallerExecutionIntent(filepath.Join(dataDir, installerExecutionRecord))
	if err != nil {
		return err
	}
	intent, guarded, err := guardedInstallerIntent(dataDir, intent)
	if err != nil {
		return err
	}
	if guarded || intent != nil && (intent.Phase != "complete" || intent.InstallationID != id) {
		return errors.New("rerun the exact bridge installer to finish interrupted execution setup")
	}
	if intent != nil {
		// The current floor can have advanced through an explicit retained transition.
		// Validate the completion's own archive as well as the live archive chain.
		if _, err = inspectInstallerRecoveryArchive(dataDir, *intent); err != nil {
			return err
		}
	}
	policy, err := readInstallerSchedulerIntent(dataDir)
	if err != nil {
		return err
	}
	if installerSchedulerPending(policy) {
		return errors.New("rerun the exact bridge installer to finish interrupted Scheduler setup")
	}
	if policy != nil && policy.InstallationID != id {
		return errors.New("Scheduler installer belongs to another installation")
	}
	if policy != nil && policy.Phase == "complete" {
		if _, err = readInstallerSchedulerPlan(dataDir, *policy); err != nil {
			return err
		}
	}
	if err = checkSecrets(ctx, db, key); err != nil {
		return err
	}
	if err = checkBaseConversions(ctx, db); err != nil {
		return err
	}
	if err = checkPending(ctx, db); err != nil {
		return err
	}
	if lease == "" {
		if err = scheduler.BridgeReadyForInstaller(ctx, filepath.Join(dataDir, "fluxo.db")); err != nil {
			return err
		}
	} else if err = phpupdate.VerifyInstallerUpgradeAfterStop(ctx, dataDir, lease, os.Stderr); err != nil {
		return err
	}
	if err = maintenancetask.BridgeReadyForInstaller(ctx); err != nil {
		return err
	}
	return nil
}

func publishedBridgeSHA() string {
	return map[string]string{"amd64": "6917de23068d56295e7fce8566e14cbc3af9a1defbbb6fc4314f1d88828a8921", "arm64": "5106d520c278e07c0286e0d950f7aa6fefa5cad8563a278b47ffa7661b58540b"}[runtime.GOARCH]
}
func verifyPublishedBridge() error {
	expected := publishedBridgeSHA()
	raw, err := readProtected("/usr/local/bin/fluxo", 128<<20)
	if err != nil {
		return err
	}
	if expected == "" || digest(raw) != expected {
		return errors.New("existing installations must complete the published v0.4.51 bridge first")
	}
	return nil
}

// BaseAccept records identity only after stopped-panel handoff admission. The
// installer owns lifecycle exclusion and rollback before calling this command.
// A receipt never replaces current floor/archive/identity checks on later boots.
func BaseAccept(ctx context.Context, dataDir, lease string) (BaseAdmission, error) {
	// A final profile observation can catch a cron launcher between the earlier
	// drain and the receipt write. Retain exclusion and repeat read-only checks;
	// the only persistent write remains after successful admission.
	return retryBaseInspection(ctx, func() (BaseAdmission, error) { return baseAcceptOnce(ctx, dataDir, lease) }, scheduler.InstallerCronDrainPending)
}

func baseAcceptOnce(ctx context.Context, dataDir, lease string) (BaseAdmission, error) {
	var r BaseAdmission
	var err error
	if lease != "" {
		r = BaseAdmission{Contract: BaseContract, Mode: "bridge"}
		if err = validateBaseState(ctx, dataDir, r, true, lease); err != nil {
			return r, err
		}
	} else {
		r, err = BaseInspect(ctx, dataDir)
		if err != nil {
			return r, err
		}
	}
	if r.Mode == "base" {
		return r, nil
	}
	if r.Mode == "bridge" && lease == "" {
		return r, errors.New("bridge-to-base acceptance requires the verified exclusive installer lease")
	}
	if r.Mode != "bridge" && r.Mode != "fresh-pending" {
		return r, errors.New("base admission needs a completed bridge or exact fresh setup")
	}
	if err = panelStopped(ctx); err != nil {
		return r, err
	}
	if r.Mode == "fresh-pending" {
		intent, e := BaseFreshStatus(dataDir)
		if e != nil || intent == nil || intent.Phase != "complete" {
			return r, errors.New("fresh base initialization must finish before admission")
		}
		if e = verifyBaseFreshFloor(dataDir, *intent); e != nil {
			return r, e
		}
		// Fresh initializer must have committed the exact current floor first.
		floor, e := executiondomain.ReadFloor(dataDir)
		if e != nil || floor == nil || floor.Phase != "committed" {
			return r, errors.New("fresh base execution initialization is incomplete")
		}
	}
	id, err := installerInstallationID(ctx, dataDir)
	if err != nil {
		return r, err
	}
	key, err := readKey(dataDir)
	if err != nil {
		return r, err
	}
	r = BaseAdmission{Contract: BaseContract, Mode: "base", InstallationID: id, KeySHA256: digest(key)}
	if err = validateBaseState(ctx, dataDir, r, false, ""); err != nil {
		return r, fmt.Errorf("base state: %w", err)
	}
	return r, writeActivationJSON(filepath.Join(dataDir, baseRecord), r)
}

// An already admitted base may retain an exact interrupted installer operation.
// This path permits only the existing authenticated resumer, never provisioning.
func validateBaseRecovery(ctx context.Context, dataDir string, saved BaseAdmission) error {
	intent, err := readInstallerExecutionIntent(filepath.Join(dataDir, installerExecutionRecord))
	if err != nil || intent == nil || intent.Phase == "complete" {
		return errors.New("no exact pending base installer operation")
	}
	image, err := readProtected("/usr/local/bin/fluxo", 128<<20)
	if err != nil {
		return err
	}
	if digest(image) != intent.Image || intent.InstallationID != saved.InstallationID {
		return errors.New("pending base installer identity changed")
	}
	key, err := readKey(dataDir)
	if err != nil || digest(key) != saved.KeySHA256 {
		return errors.New("pending base key identity changed")
	}
	path, cleanup, err := snapshot(ctx, dataDir)
	if err != nil {
		return err
	}
	defer cleanup()
	db, err := openSQLite(ctx, path, false)
	if err != nil {
		return err
	}
	defer db.Close()
	if err = checkSchema(ctx, db); err != nil {
		return err
	}
	var id string
	if err = db.QueryRowContext(ctx, "SELECT value FROM system_metadata WHERE key='installation_id'").Scan(&id); err != nil || id != saved.InstallationID {
		return errors.New("pending base database identity changed")
	}
	if intent.Operation != "" {
		_, err = inspectInstallerRecoveryArchive(dataDir, *intent)
		return err
	}
	floor, err := executiondomain.ReadFloor(dataDir)
	if err != nil {
		return err
	}
	if floor == nil || floor.Phase != "committed" {
		return errors.New("unplanned base recovery lost its committed floor")
	}
	return validateCommittedActivationArchive(dataDir, *floor)
}

func decodeBaseAdmission(raw []byte) (BaseAdmission, error) {
	var r BaseAdmission
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF || r.Contract != BaseContract || r.Mode != "base" || !installerSchedulerDigest(r.KeySHA256) || len(r.InstallationID) != 32 || !installerSchedulerDigest(r.InstallationID+r.InstallationID) {
		return r, errors.New("invalid base admission record; restore its original evidence")
	}
	return r, nil
}
