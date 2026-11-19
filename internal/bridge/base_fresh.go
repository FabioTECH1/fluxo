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
	"os/user"
	"path/filepath"
	"strings"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/scheduler"
	"github.com/google/uuid"
	"golang.org/x/mod/semver"
)

const baseFreshRecord = "base-fresh.json"

// BaseFreshIntent proves only that this exact installer observed an absent
// installation before provisioning. Execution authority remains exclusively in
// the archived activation, committed floor and genuine loader receipts.
type BaseFreshIntent struct {
	Format         int    `json:"format"`
	ImageSHA256    string `json:"image_sha256"`
	Version        string `json:"version"`
	Phase          string `json:"phase"`
	InstallationID string `json:"installation_id"`
	OperationID    string `json:"operation_id"`
	PlanSHA256     string `json:"plan_sha256"`
}

// BaseFreshStatus is read-only, including for interrupted initialization.
func BaseFreshStatus(dataDir string) (*BaseFreshIntent, error) {
	raw, err := readProtected(filepath.Join(dataDir, baseFreshRecord), 16<<10)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeBaseFreshIntent(raw)
}

func decodeBaseFreshIntent(raw []byte) (*BaseFreshIntent, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	for _, key := range []string{"format", "image_sha256", "version", "phase", "installation_id", "operation_id", "plan_sha256"} {
		if len(fields[key]) == 0 || bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return nil, errors.New("fresh base intent lacks explicit recovery fields")
		}
	}
	var v BaseFreshIntent
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	if d.Decode(new(any)) != io.EOF || v.Format != 1 || !installerSchedulerDigest(v.ImageSHA256) || !semver.IsValid("v"+v.Version) {
		return nil, errors.New("invalid fresh base intent")
	}
	if v.Phase == "pending" {
		if v.InstallationID != "" || v.OperationID != "" || v.PlanSHA256 != "" {
			return nil, errors.New("unplanned fresh base intent claims activation authority")
		}
	} else if v.Phase == "activating" || v.Phase == "complete" {
		id, err := uuid.Parse(v.OperationID)
		installation, e := hex.DecodeString(v.InstallationID)
		if err != nil || id.String() != v.OperationID || e != nil || len(installation) != 16 || strings.ToLower(v.InstallationID) != v.InstallationID || !installerSchedulerDigest(v.PlanSHA256) {
			return nil, errors.New("fresh base intent lost exact activation identity")
		}
	} else {
		return nil, errors.New("unknown fresh base initialization phase")
	}
	return &v, nil
}

func requireFreshImage(intent BaseFreshIntent, imageSHA, version string) error {
	if intent.ImageSHA256 != imageSHA || intent.Version != version {
		return fmt.Errorf("unfinished fresh base installation requires its original release %s (%s); retain its state and rerun that exact installer", intent.Version, intent.ImageSHA256)
	}
	return nil
}

// BaseValidateFreshImage must run before database/key initialization on retry.
// Merely finding a marker is insufficient permission to create missing state.
func BaseValidateFreshImage(dataDir, version string) error {
	if os.Geteuid() != 0 || dataDir != "/var/lib/fluxo" {
		return errors.New("fresh base initialization requires the installed root scope")
	}
	intent, err := BaseFreshStatus(dataDir)
	if err != nil {
		return err
	}
	if intent == nil {
		return errors.New("fresh base initialization requires its protected pre-provisioning origin")
	}
	image, err := readCertifyingExecutable()
	if err != nil {
		return err
	}
	return requireFreshImage(*intent, digest(image), version)
}

// BasePrepareFresh runs from the authenticated candidate before provisioning.
// A partial or damaged installation cannot acquire a new fresh origin marker.
func BasePrepareFresh(dataDir, version string) error {
	if os.Geteuid() != 0 || dataDir != "/var/lib/fluxo" || !semver.IsValid("v"+version) {
		return errors.New("fresh base preparation requires root, the installed data directory and a versioned image")
	}
	image, err := readCertifyingExecutable()
	if err != nil {
		return err
	}
	intent, err := BaseFreshStatus(dataDir)
	if err != nil {
		return err
	}
	if intent != nil {
		if err = validatePendingFreshOrigin(context.Background(), dataDir, *intent); err != nil {
			return err
		}
		if requireFreshImage(*intent, digest(image), version) == nil {
			return nil
		}
		if intent.Phase != "pending" {
			return requireFreshImage(*intent, digest(image), version)
		}
		// No execution operation exists. Preserve the exact failed origin before
		// selecting a corrected authenticated artifact for the same empty host.
		archive := filepath.Join(dataDir, "base-fresh-origins")
		if err = ensureActivationParent(archive); err != nil {
			return err
		}
		old, e := readProtected(filepath.Join(dataDir, baseFreshRecord), 16<<10)
		if e != nil {
			return e
		}
		target := filepath.Join(archive, digest(old)+".json")
		if existing, e := readProtected(target, 16<<10); e == nil {
			if !bytes.Equal(existing, old) {
				return errors.New("fresh origin archive changed")
			}
		} else if os.IsNotExist(e) {
			if e = writeActivationFile(target, old, 0600); e != nil {
				return e
			}
		} else {
			return e
		}
		intent.ImageSHA256, intent.Version = digest(image), version
		return writeActivationJSON(filepath.Join(dataDir, baseFreshRecord), intent)
	}
	for _, path := range []string{"/usr/local/bin/fluxo", "/etc/systemd/system/fluxo.service", "/etc/systemd/system/fluxo.service.d", "/home/fluxo", "/etc/sudoers.d/fluxo"} {
		if _, e := os.Lstat(path); !os.IsNotExist(e) {
			return fmt.Errorf("existing Fluxo artifact at %s requires the supported bridge or recovery", path)
		}
	}
	if err = freshAccountAbsent(); err != nil {
		return err
	}
	if err = freshRuntimeAbsent(os.Lstat, filepath.Glob); err != nil {
		return err
	}
	if entries, e := os.ReadDir(dataDir); e == nil {
		if len(entries) != 0 {
			return errors.New("existing Fluxo data requires the supported bridge; it cannot initialize as fresh")
		}
		if err = trustedDirectory(dataDir); err != nil {
			return err
		}
	} else if !os.IsNotExist(e) {
		return e
	} else {
		if err = trustedDirectory(filepath.Dir(dataDir)); err != nil {
			return err
		}
		if err = os.Mkdir(dataDir, 0700); err != nil {
			return err
		}
		if err = syncActivationDirectory(filepath.Dir(dataDir)); err != nil {
			return err
		}
	}
	intent = &BaseFreshIntent{Format: 1, ImageSHA256: digest(image), Version: version, Phase: "pending"}
	raw, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	// Exclusive creation never lets a competing installer overwrite an origin.
	f, err := os.OpenFile(filepath.Join(dataDir, baseFreshRecord), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return syncActivationDirectory(dataDir)
}

func freshAccountAbsent() error {
	if _, err := user.Lookup("fluxo"); err == nil {
		return errors.New("existing Fluxo account requires the supported bridge or recovery")
	} else {
		var unknown user.UnknownUserError
		if !errors.As(err, &unknown) {
			return fmt.Errorf("cannot establish absence of the Fluxo account: %w", err)
		}
	}
	return nil
}

// The same conservative artifact inventory is used before provisioning and
// before the first current-core publication. Any retained origin wins over an
// inference of emptiness, even if its principal journal/binary is missing.
func freshRuntimeAbsent(lstat func(string) (os.FileInfo, error), glob func(string) ([]string, error)) error {
	for _, path := range []string{
		"/var/lib/fluxo-scheduler", "/var/lib/fluxo-scheduler.initialized", "/var/lib/fluxo-scheduler.init.lock",
		"/var/lib/fluxo-system-tasks", "/var/lib/fluxo-system-tasks.initialized", "/var/lib/fluxo-system-tasks.lock",
	} {
		if _, err := lstat(path); !os.IsNotExist(err) {
			return fmt.Errorf("existing or partial execution state at %s requires its original bridge/recovery", path)
		}
	}
	for _, pattern := range []string{"/etc/cron.d/fluxo*", "/etc/systemd/system/fluxo-scheduler*", "/etc/systemd/system/fluxo-system-task*"} {
		matches, err := glob(pattern)
		if err != nil {
			return err
		}
		if len(matches) != 0 {
			return errors.New("existing Fluxo execution triggers require their original bridge/recovery")
		}
	}
	return nil
}

func freshInitializationRuntimeAbsent(lstat func(string) (os.FileInfo, error), glob func(string) ([]string, error), read func(string, int64) ([]byte, error)) error {
	return freshRuntimeAbsent(func(path string) (os.FileInfo, error) {
		if path == "/var/lib/fluxo-scheduler.init.lock" {
			// The initializer may die before archiving its plan. This empty
			// inode-stable coordination lock grants no execution authority. The
			// fresh origin already proved its absence before provisioning.
			raw, err := read(path, 32)
			if err == nil && len(raw) == 0 {
				return nil, os.ErrNotExist
			}
			if err != nil {
				return nil, err
			}
		}
		return lstat(path)
	}, glob)
}

// BaseInitializeFresh runs after the current database/key and inactive service
// have been installed, before the first panel start. It publishes current empty
// cores directly through the existing durable absent-state activation protocol.
// First startup establishes loader epochs; no cron acknowledgement is invented.
func BaseInitializeFresh(ctx context.Context, dataDir, version string) (ExecutionActivationReport, error) {
	var empty ExecutionActivationReport
	if os.Geteuid() != 0 || dataDir != "/var/lib/fluxo" {
		return empty, errors.New("fresh base initialization requires the installed root scope")
	}
	intent, err := BaseFreshStatus(dataDir)
	if err != nil || intent == nil {
		return empty, errors.New("fresh base initialization requires its protected pre-provisioning origin")
	}
	image, err := readCertifyingExecutable()
	if err != nil {
		return empty, err
	}
	if err = requireFreshImage(*intent, digest(image), version); err != nil {
		return empty, err
	}
	request := activationRequest{}
	if intent.Phase == "pending" {
		if err = freshDatabaseEmpty(ctx, dataDir); err != nil {
			return empty, err
		}
		if err = freshInitializationRuntimeAbsent(os.Lstat, filepath.Glob, readProtected); err != nil {
			return empty, err
		}
		for _, path := range []string{filepath.Join(dataDir, "execution-readers.json"), filepath.Join(dataDir, "execution-activations")} {
			if _, err = os.Lstat(path); !os.IsNotExist(err) {
				return empty, errors.New("fresh base initialization cannot adopt existing activation history")
			}
		}
		staged, e := filepath.Glob(filepath.Join(dataDir, ".execution-installer-*"))
		if e != nil || len(staged) != 0 {
			return empty, errors.New("incomplete fresh activation archive requires retained exact recovery evidence")
		}
		request.InstallerPrepared = func(plan executionActivation, raw []byte) error {
			if err := validateFreshCreationPlan(plan); err != nil {
				return err
			}
			intent.InstallationID, intent.OperationID, intent.PlanSHA256 = plan.InstallationID, plan.ID, digest(raw)
			intent.Phase = "activating"
			return writeActivationJSON(filepath.Join(dataDir, baseFreshRecord), intent)
		}
	} else {
		// Archive authentication precedes any publication/repair. This reuses the
		// exact same plan and image; a missing floor never means a new operation.
		recovery := installerExecutionIntent{Image: intent.ImageSHA256, Version: intent.Version, InstallationID: intent.InstallationID, Operation: intent.OperationID, Plan: intent.PlanSHA256}
		if _, err = inspectInstallerRecoveryArchive(dataDir, recovery); err != nil {
			return empty, err
		}
		if err = prepareInstallerRecovery(dataDir, recovery); err != nil {
			return empty, err
		}
		request.Resume = intent.OperationID
	}
	report, err := activateExecutionFormats(ctx, dataDir, version, request)
	if err != nil {
		return report, err
	}
	if err = verifyBaseFreshFloor(dataDir, *intent); err != nil {
		return report, err
	}
	intent.Phase = "complete"
	return report, writeActivationJSON(filepath.Join(dataDir, baseFreshRecord), intent)
}

func freshDatabaseEmpty(ctx context.Context, dataDir string) error {
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
	if err = db.QueryRowContext(ctx, "SELECT value FROM system_metadata WHERE key='installation_id'").Scan(&id); err != nil || len(id) != 32 || !installerSchedulerDigest(id+id) {
		return errors.New("fresh database lost its original installation identity")
	}
	return checkFreshDatabaseEmpty(ctx, db)
}

func checkFreshDatabaseEmpty(ctx context.Context, q queryer) error {
	rows, err := q.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var table string
		if err = rows.Scan(&table); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, table)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, table := range tables {
		query := `SELECT count(*) FROM "` + strings.ReplaceAll(table, `"`, `""`) + `"`
		switch table {
		case "system_metadata":
			query += " WHERE key!='installation_id'"
		case "panel_domain":
			query += " WHERE id!=1 OR domain!='' OR ssl_provider!='' OR cert_path!='' OR key_path!='' OR source_certificate_id!=0"
		case "php_update_schedule":
			query += " WHERE id!=1 OR next_check!=0"
		case "node_update_snapshot":
			query += " WHERE id!=1 OR payload!='{}' OR next_check!=0 OR dismissed_candidate!=''"
		}
		var n int
		if err = q.QueryRowContext(ctx, query).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return errors.New("fresh base initialization cannot adopt existing application data or history")
		}
	}
	return nil
}

func validateFreshCreationPlan(plan executionActivation) error {
	if plan.Kind != activationFresh || plan.Format != 4 || len(plan.SourceFloor) != 0 {
		return errors.New("fresh base initialization cannot convert an existing reader generation")
	}
	if err := validateActivationPlan(plan); err != nil {
		return err
	}
	for _, f := range plan.Files {
		if f.Before != "absent" || f.Cron {
			return errors.New("fresh base initialization cannot adopt or replace existing execution state")
		}
		if f.Path == "/var/lib/fluxo-scheduler/state.json" {
			if err := scheduler.ValidateExecutionActivationPlanForSchema(nil, f.Data, executiondomain.CurrentSchedulerSchema); err != nil {
				return err
			}
		}
		if f.Path == "/var/lib/fluxo-system-tasks/state.json" {
			if err := maintenancetask.ValidateExecutionActivationPlan(nil, f.Data, plan.BinarySHA256, plan.Files); err != nil {
				return err
			}
		}
	}
	return nil
}

func verifyBaseFreshFloor(dataDir string, intent BaseFreshIntent) error {
	floor, err := executiondomain.ReadFloor(dataDir)
	if err != nil {
		return err
	}
	if floor == nil || floor.Phase != "committed" || floor.ActivationID != intent.OperationID || floor.PlanSHA256 != intent.PlanSHA256 || floor.InstallationID != intent.InstallationID || floor.BinarySHA256 != intent.ImageSHA256 {
		return errors.New("fresh base completion requires its exact committed activation")
	}
	return validateCommittedActivationArchive(dataDir, *floor)
}

// Before an activation UUID is published, a failed fresh installer may select a
// corrected artifact only on the same still-empty installation. This is not a
// conversion or an authority receipt. Every superseded origin remains archived.
func validatePendingFreshOrigin(ctx context.Context, dataDir string, intent BaseFreshIntent) error {
	if intent.Phase != "pending" {
		return nil
	}
	if err := freshInitializationRuntimeAbsent(os.Lstat, filepath.Glob, readProtected); err != nil {
		return err
	}
	for _, name := range []string{"execution-readers.json", "execution-activations", "execution-installer.json", "scheduler-installer.json"} {
		if _, e := os.Lstat(filepath.Join(dataDir, name)); !os.IsNotExist(e) {
			return errors.New("fresh origin already has execution evidence; retain its exact recovery image")
		}
	}
	if _, e := os.Lstat(filepath.Join(dataDir, "fluxo.db")); e == nil {
		if e = freshDatabaseEmpty(ctx, dataDir); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if _, e := os.Lstat(filepath.Join(dataDir, "encryption.key")); e == nil {
		if _, e = readKey(dataDir); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if _, e := os.Lstat("/usr/local/bin/fluxo"); e == nil {
		raw, e := readProtected("/usr/local/bin/fluxo", 128<<20)
		if e != nil {
			return e
		}
		if digest(raw) != intent.ImageSHA256 {
			if err := archivedPendingFreshImage(dataDir, digest(raw)); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if _, e := os.Lstat("/etc/systemd/system/fluxo.service"); e == nil {
		return panelStopped(ctx)
	} else if !os.IsNotExist(e) {
		return e
	}
	return nil
}

// A replacement intent may have been saved while its archived predecessor is
// still installed. Only an exact protected pending origin can explain that gap.
func archivedPendingFreshImage(dataDir, image string) error {
	dir := filepath.Join(dataDir, "base-fresh-origins")
	if err := trustedDirectory(dir); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > 128 {
		return errors.New("too many fresh origin transitions; preserve and inspect them")
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, e := readProtected(filepath.Join(dir, entry.Name()), 16<<10)
		if e != nil {
			return e
		}
		if entry.Name() != digest(raw)+".json" {
			return errors.New("fresh origin archive name changed")
		}
		original, e := decodeBaseFreshIntent(raw)
		if e != nil {
			return e
		}
		if original.Phase == "pending" && original.ImageSHA256 == image {
			return nil
		}
	}
	return errors.New("fresh install's original binary changed without its archived origin")
}
