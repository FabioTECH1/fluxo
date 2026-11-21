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
	"time"

	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/phpupdate"
	"fluxo/internal/services/scheduler"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
	"golang.org/x/mod/semver"
)

const installerSchedulerRecord = "scheduler-installer.json"

// This pointer binds installer orchestration to one immutable plan, installed
// image, service and existing floor. Execution authority remains in the normal
// Scheduler ledger/receipts; this record never grants launch permission.
type installerSchedulerIntent struct {
	Format         int    `json:"format"`
	Operation      string `json:"operation_id"`
	Plan           string `json:"plan_sha256"`
	Image          string `json:"image_sha256"`
	Version        string `json:"version"`
	InstallationID string `json:"installation_id"`
	Service        string `json:"service_sha256"`
	Floor          string `json:"floor_sha256"`
	WasActive      bool   `json:"was_active"`
	Phase          string `json:"phase"`
}

func readInstallerSchedulerIntent(dataDir string) (*installerSchedulerIntent, error) {
	raw, err := readProtected(filepath.Join(dataDir, installerSchedulerRecord), 16<<10)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeInstallerSchedulerIntent(raw)
}

func decodeInstallerSchedulerIntent(raw []byte) (*installerSchedulerIntent, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, errors.New("invalid installer Scheduler intent")
	}
	for _, key := range []string{"format", "operation_id", "plan_sha256", "image_sha256", "version", "installation_id", "service_sha256", "floor_sha256", "was_active", "phase"} {
		if len(fields[key]) == 0 || bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return nil, errors.New("installer Scheduler intent lacks explicit recovery fields")
		}
	}
	var v installerSchedulerIntent
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	if d.Decode(new(any)) != io.EOF || v.Format != 1 || !semver.IsValid("v"+v.Version) ||
		v.Phase != "preparing" && v.Phase != "unchanged" && v.Phase != "pending" && v.Phase != "published" && v.Phase != "complete" {
		return nil, errors.New("invalid installer Scheduler recovery phase")
	}
	if v.Phase == "preparing" || v.Phase == "unchanged" {
		if v.Operation != "" || v.Plan != "" {
			return nil, errors.New("unplanned Scheduler setup cannot claim a publication")
		}
	} else {
		if id, err := uuid.Parse(v.Operation); err != nil || id.String() != v.Operation || !installerSchedulerDigest(v.Plan) {
			return nil, errors.New("invalid installer Scheduler operation or plan")
		}
	}
	for _, value := range []string{v.Image, v.Service, v.Floor} {
		if !installerSchedulerDigest(value) {
			return nil, errors.New("invalid installer Scheduler recovery digest")
		}
	}
	if len(v.InstallationID) != 32 || !installerSchedulerDigest(v.InstallationID+v.InstallationID) {
		return nil, errors.New("invalid installer Scheduler installation")
	}
	return &v, nil
}

func installerSchedulerPending(v *installerSchedulerIntent) bool {
	return v != nil && v.Phase != "complete" && v.Phase != "unchanged"
}

// The restart binding is durable before any stop. A process death in stop or
// subsequent planning therefore remains visible to the normal early resumer.
// Every necessary stop, including a pending-plan retry, drains panel work.
func pauseInstallerSchedulerPanel(intent installerSchedulerIntent, active bool, drain, persist, stop func() error) error {
	if intent.Phase != "preparing" && intent.Phase != "pending" {
		return errors.New("only unpublished Scheduler setup may stop the panel")
	}
	if active {
		if err := drain(); err != nil {
			return err
		}
	}
	if intent.Phase == "preparing" {
		if err := persist(); err != nil {
			return err
		}
	}
	if active {
		return stop()
	}
	return nil
}

// Published recovery is read-only with respect to launch authority. Reuse an
// already active panel; a retry must not stop a command started after publication.
func finishPublishedInstallerScheduler(intent *installerSchedulerIntent, active, complete bool, verify, start, acknowledge, persist func() error) error {
	if intent.Phase != "published" {
		return errors.New("Scheduler setup has not published its exact plan")
	}
	if err := verify(); err != nil {
		return err
	}
	if intent.WasActive && !active {
		if err := start(); err != nil {
			return err
		}
	}
	if err := acknowledge(); err != nil {
		return err
	}
	if complete {
		intent.Phase = "complete"
		return persist()
	}
	return nil
}

func installerSchedulerDigest(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}

func installerSchedulerPlanPath(dataDir, operation string) string {
	return filepath.Join(dataDir, "scheduler-installations", operation, "plan.json")
}

func readInstallerSchedulerPlan(dataDir string, intent installerSchedulerIntent) (scheduler.LoaderPublicationPlan, error) {
	raw, err := readProtected(installerSchedulerPlanPath(dataDir, intent.Operation), 64<<20)
	if err != nil || digest(raw) != intent.Plan {
		return scheduler.LoaderPublicationPlan{}, errors.New("exact installer Scheduler plan is missing or changed")
	}
	plan, err := scheduler.DecodeInstallerLoaderPlan(raw)
	if err != nil || plan.ID != intent.Operation {
		return plan, errors.New("installer Scheduler plan belongs to another operation")
	}
	return plan, nil
}

// Normal installer compatibility includes the qualified same-contract policy
// transition. Completed reader floors are never rewritten for this operation.
func installSchedulerPolicyOnce(ctx context.Context, dataDir, version string, complete bool) (err error) {
	image, err := readCertifyingExecutable()
	if err != nil {
		return err
	}
	imageSHA := digest(image)
	release, err := packages.Lifecycle(ctx, false)
	if err != nil {
		return err
	}
	defer release()
	service, active, err := installerPanelBinding(ctx, dataDir, imageSHA)
	if err != nil {
		return err
	}
	id, err := installerInstallationID(ctx, dataDir)
	if err != nil {
		return err
	}
	floor, err := readProtected(filepath.Join(dataDir, "execution-readers.json"), 64<<10)
	if err != nil {
		return err
	}
	intent, err := readInstallerSchedulerIntent(dataDir)
	if err != nil {
		return err
	}
	pending := installerSchedulerPending(intent)
	if pending && (intent.Image != imageSHA || intent.Version != version || intent.Service != service || intent.InstallationID != id || intent.Floor != digest(floor)) {
		return fmt.Errorf("unfinished Scheduler setup requires its exact original release %s (%s), installation, service and reader floor", intent.Version, intent.Image)
	}
	startPanel := func() error {
		binding, _, e := installerPanelBinding(ctx, dataDir, imageSHA)
		if e != nil || binding != service {
			return errors.New("installed image or service changed before Scheduler setup restart")
		}
		_, e = syscmd.Run(ctx, 30*time.Second, "/usr/bin/systemctl", "start", "fluxo.service")
		return e
	}
	if complete && !pending {
		return scheduler.WaitInstallerLoaderAcknowledgements(ctx)
	}
	if pending && intent.Phase == "published" {
		return finishPublishedInstallerScheduler(intent, active, complete, func() error {
			plan, e := readInstallerSchedulerPlan(dataDir, *intent)
			if e != nil {
				return e
			}
			unlock, e := lockCoordination(ctx)
			if e != nil {
				return e
			}
			defer unlock()
			return scheduler.VerifyInstallerLoaderPolicyLocked(plan)
		}, startPanel, func() error { return scheduler.WaitInstallerLoaderAcknowledgements(ctx) }, func() error {
			return writeActivationJSON(filepath.Join(dataDir, installerSchedulerRecord), intent)
		})
	}
	if !pending {
		unlock, e := lockCoordination(ctx)
		if e != nil {
			return e
		}
		needed, e := scheduler.InstallerLoaderPolicyNeededLocked(ctx)
		unlock()
		if e != nil || !needed {
			return e
		}
		if e = scheduler.ReconcileInstallerDomains(ctx); e != nil {
			return e
		}
		if e = phpupdate.CheckInstallerDatabase(ctx, dataDir); e != nil {
			return e
		}
		// No plan is synthesized while the panel can still mutate its core.
		// This exact restart binding is sufficient for early normal recovery.
		intent = &installerSchedulerIntent{Format: 1, Image: imageSHA, Version: version, InstallationID: id, Service: service, Floor: digest(floor), WasActive: active, Phase: "preparing"}
	}
	if err = pauseInstallerSchedulerPanel(*intent, active, func() error {
		_, e := scheduler.PanelBeforeStop(ctx)
		return e
	}, func() error {
		return writeActivationJSON(filepath.Join(dataDir, installerSchedulerRecord), intent)
	}, func() error {
		_, e := syscmd.Run(ctx, 30*time.Second, "/usr/bin/systemctl", "stop", "fluxo.service")
		return e
	}); err != nil {
		return err
	}
	if err = panelStopped(ctx); err != nil {
		return err
	}
	wasActive := intent.WasActive
	defer func() {
		if err != nil && intent.Phase == "preparing" && wasActive {
			_ = startPanel()
		}
	}()
	noPublication := false
	err = withInstallerSchedulerGates(ctx, dataDir, intent.Operation, func() error {
		var plan scheduler.LoaderPublicationPlan
		if intent.Phase == "pending" {
			var e error
			plan, e = readInstallerSchedulerPlan(dataDir, *intent)
			if e != nil {
				return e
			}
		} else {
			next, e := scheduler.PlanInstallerLoaderPolicyLocked(ctx)
			if e != nil {
				return e
			}
			if next == nil {
				noPublication = true
				return nil
			}
			plan = *next
			raw, e := json.Marshal(plan)
			if e != nil {
				return e
			}
			parent := filepath.Join(dataDir, "scheduler-installations")
			if e = ensureActivationParent(parent); e != nil {
				return e
			}
			directory := filepath.Dir(installerSchedulerPlanPath(dataDir, plan.ID))
			if e = os.Mkdir(directory, 0700); e != nil {
				return e
			}
			if e = writeActivationJSON(filepath.Join(directory, "plan.json"), plan); e != nil {
				return e
			}
			if e = syncActivationDirectory(parent); e != nil {
				return e
			}
			intent.Operation, intent.Plan, intent.Phase = plan.ID, digest(raw), "pending"
			if e = writeActivationJSON(filepath.Join(dataDir, installerSchedulerRecord), intent); e != nil {
				return e
			}
		}
		binding, running, e := installerPanelBinding(ctx, dataDir, imageSHA)
		if e != nil || running || binding != service {
			return errors.New("installed image or stopped panel service changed before Scheduler publication")
		}
		currentFloor, e := readProtected(filepath.Join(dataDir, "execution-readers.json"), 64<<10)
		if e != nil || digest(currentFloor) != intent.Floor {
			return errors.New("reader floor changed before Scheduler publication")
		}
		if e := scheduler.ApplyInstallerLoaderPolicyLocked(ctx, plan); e != nil {
			return e
		}
		intent.Phase = "published"
		return writeActivationJSON(filepath.Join(dataDir, installerSchedulerRecord), intent)
	})
	if err != nil {
		return err
	}
	if wasActive {
		if err = startPanel(); err != nil {
			return err
		}
	}
	if noPublication || complete {
		if err = scheduler.WaitInstallerLoaderAcknowledgements(ctx); err != nil {
			return err
		}
		intent.Phase = "complete"
		if noPublication {
			// Close a stop/restart-only operation only after restart and genuine
			// current readiness. No empty digest can masquerade as a publication.
			intent.Phase = "unchanged"
		}
		return writeActivationJSON(filepath.Join(dataDir, installerSchedulerRecord), intent)
	}
	return nil
}

func withInstallerSchedulerGates(ctx context.Context, dataDir, publication string, apply func() error) error {
	release, err := packages.Acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	native, err := packages.NativeDPKG(ctx)
	if err != nil {
		return err
	}
	defer native()
	if err = panelStopped(ctx); err != nil {
		return err
	}
	if err = packages.TinkerDomainsIdle(); err != nil {
		return err
	}
	if publication == "" {
		err = scheduler.Quiescent(ctx)
	} else {
		err = scheduler.InstallerLoaderRecoveryQuiescent(ctx, publication)
	}
	if err != nil {
		return err
	}
	if err = maintenancetask.ExecutionDomainsQuiescent(ctx); err != nil {
		return err
	}
	unlock, err := lockCoordination(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	db, err := open(ctx, dataDir, true)
	if err != nil {
		return err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	if err = checkPending(ctx, conn); err != nil {
		return err
	}
	return apply()
}
