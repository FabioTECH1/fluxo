package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/phpupdate"
	"golang.org/x/mod/semver"
	"golang.org/x/sys/unix"
)

// Only one of these names may own the original operation directory. Its inode
// survives same-filesystem rename and binds the proof across a process crash.
func withdrawalArchive(staged, published, retired string) (string, error) {
	found := ""
	for _, path := range []string{staged, published, retired} {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if found != "" || !info.IsDir() {
			return "", errors.New("ambiguous installer renewal archive")
		}
		found = path
	}
	if found == "" {
		return "", errors.New("missing installer renewal archive")
	}
	return found, nil
}

func withdrawalRename(src, dst string) error {
	if err := unix.Renameat2(unix.AT_FDCWD, src, unix.AT_FDCWD, dst, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	if err := syncActivationDirectory(filepath.Dir(dst)); err != nil {
		return err
	}
	return syncActivationDirectory(filepath.Dir(src))
}

func withdrawalRecord(path string, want []byte, create bool) error {
	raw, err := readProtected(path, 8<<20)
	if create && os.IsNotExist(err) {
		return writeActivationFence(path, want)
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, want) {
		return errors.New("retained withdrawal evidence differs from the original operation")
	}
	return nil
}

func validateWithdrawalTarget(f executiondomain.ActivationFile, raw []byte, mode os.FileMode, mtime int64) error {
	if f.Directory || f.Lock || f.Image || len(f.Fence) != 0 || f.Before == digest(f.Data) ||
		digest(raw) != f.Before || mode != os.FileMode(f.Mode) || f.Cron && mtime == f.CronMTime {
		return fmt.Errorf("installer renewal target does not establish its distinct original state: %s", f.Path)
	}
	return nil
}

type withdrawalObservation struct {
	Path                 string
	Device, Inode, Links uint64
	UID, GID, Mode       uint32
	Size, MTimeNS        int64
	SHA256               string `json:",omitempty"`
}

func readWithdrawalArchive(dir string, intent installerExecutionIntent, historical bool, observed *[]withdrawalObservation) (executionActivation, error) {
	var plan executionActivation
	raw, err := readProtected(filepath.Join(dir, "plan.json"), 128<<20)
	if err != nil {
		return plan, err
	}
	if err = validateInstallerPlan(intent, raw); err != nil {
		return plan, err
	}
	if err = json.Unmarshal(raw, &plan); err != nil {
		return plan, err
	}
	if plan.InstallerTaskGuard != "" || plan.Format != 4 || historical != installerHistoricalTaskKind(plan.Kind) || !historical && plan.Kind != activationInstallerTaskBackendRenewal {
		return plan, errors.New("withdrawal requires its original unfenced historical conversion or task renewal")
	}
	img, err := readProtected(filepath.Join(dir, "image"), 128<<20)
	if err != nil || digest(img) != intent.Image {
		return plan, errors.New("original installer image archive changed")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return plan, err
	}
	// Every expected member below must exist. This count also rejects any
	// verification/completion receipt or unrecognized artifact.
	expected := 2
	for _, f := range plan.Files {
		if f.Before != "absent" {
			expected++
		}
	}
	if len(entries) != expected {
		return plan, errors.New("installer renewal archive has missing or extra evidence")
	}
	if err = validateActivationSources(dir, plan); err != nil {
		return plan, err
	}
	for i, f := range plan.Files {
		if historical && (f.Lock || len(f.Fence) != 0 || f.Before == "absent" && !(f.Image || f.Directory)) {
			return plan, errors.New("withdrawal requires both original historical cores")
		}
		if historical && f.Before == "absent" {
			if observed != nil {
				if _, err := os.Lstat(f.Path); !os.IsNotExist(err) {
					return plan, fmt.Errorf("historical target already exists: %s", f.Path)
				}
			}
			continue
		}
		before, err := readProtected(filepath.Join(dir, fmt.Sprintf("before-%d", i)), 256<<20)
		if err != nil || digest(before) != f.Before {
			return plan, errors.New("original installer target backup changed")
		}
		if observed == nil {
			continue
		}
		info, err := protectedInfo(f.Path)
		if err != nil {
			return plan, err
		}
		current, err := readProtected(f.Path, 256<<20)
		if err != nil {
			return plan, err
		}
		after, err := protectedInfo(f.Path)
		if err != nil || !sameObservation(info, after) {
			return plan, errors.New("installer target metadata changed during withdrawal")
		}
		if historical {
			err = historicalWithdrawalTarget(f, before, current, info)
		} else {
			err = validateWithdrawalTarget(f, current, info.Mode(), info.ModTime().Unix())
		}
		if err != nil {
			return plan, err
		}
		st := info.Sys().(*syscall.Stat_t)
		sha := ""
		if historical {
			sha = digest(current)
		}
		*observed = append(*observed, withdrawalObservation{f.Path, uint64(st.Dev), st.Ino, uint64(st.Nlink), st.Uid, st.Gid, st.Mode, info.Size(), info.ModTime().UnixNano(), sha})
	}
	return plan, nil
}

func withdrawInstallerRenewal(ctx context.Context, dataDir, version string, a *maintenanceCandidateAdmission) (any, error) {
	h := a.Handoff
	release, err := packages.Lifecycle(ctx, false)
	if err != nil {
		return nil, err
	}
	defer release()
	path := filepath.Join(dataDir, installerExecutionRecord)
	raw, err := readProtected(path, 16<<10)
	if err != nil {
		return nil, err
	}
	intent, err := decodeInstallerExecutionIntent(raw)
	if err != nil {
		return nil, err
	}
	historical := h.Action == historicalWithdrawal
	newer := intent.Image != h.ArtifactSHA256 && semver.Compare("v"+version, "v"+intent.Version) > 0
	if intent.Phase != "pending" || (intent.TaskCore == "") != historical || intent.Operation != h.OperationID || digest(raw) != h.Expectations[1] ||
		intent.InstallationID != h.InstallationID || !newer {
		return nil, errors.New("withdrawal requires a newer authenticated candidate and its exact pending installer operation")
	}
	parent := filepath.Join(dataDir, "execution-installer-withdrawals")
	caseDir := filepath.Join(parent, intent.Operation)
	retired := filepath.Join(caseDir, "operation")
	staged := installerStagedArchive(dataDir, intent.Operation)
	published := filepath.Join(dataDir, "execution-activations", intent.Operation)
	// Check the service, installation, pending pointer and floor at each
	// publication boundary, including after a successful but interrupted start.
	binding := func() (bool, error) {
		if err := a.revalidate(version); err != nil {
			return false, err
		}
		if err := withdrawalRecord(path, raw, false); err != nil {
			return false, err
		}
		old, e := readProtected("/usr/local/bin/fluxo", 128<<20)
		if e != nil || digest(old) != intent.Image {
			return false, errors.New("installed original installer image changed")
		}
		id, e := installerInstallationID(ctx, dataDir)
		if e != nil || id != intent.InstallationID {
			return false, errors.New("installer installation identity changed")
		}
		service, active, e := installedPanelServiceBinding(ctx, dataDir, intent.Image)
		if e != nil {
			return false, e
		}
		if service != intent.Service {
			return false, errors.New("original installed service binding changed")
		}
		policy, e := readInstallerSchedulerIntent(dataDir)
		if e != nil {
			return false, e
		}
		if installerSchedulerPending(policy) {
			return false, errors.New("Scheduler installer recovery owns the service")
		}
		if historical {
			if _, e := os.Lstat(filepath.Join(dataDir, "execution-readers.json")); !os.IsNotExist(e) || h.SourceFloorSHA256 != "absent" {
				return false, errors.New("historical withdrawal requires original floor absence")
			}
			return active, nil
		}
		floorRaw, e := readProtected(filepath.Join(dataDir, "execution-readers.json"), 64<<10)
		if e != nil || digest(floorRaw) != h.SourceFloorSHA256 {
			return false, errors.New("original installer source floor changed")
		}
		f, e := executiondomain.ParseReaderFloor(floorRaw)
		if e != nil || f.InstallationID != intent.InstallationID || f.Phase != "committed" || f.ActivationID == intent.Operation {
			return false, errors.New("withdrawal requires the exact committed predecessor floor")
		}
		if _, e = executiondomain.InspectCompatibility(dataDir); e != nil {
			return false, e
		}
		if e = validateCommittedActivationArchive(dataDir, f); e != nil {
			return false, e
		}
		return active, nil
	}
	active, err := binding()
	if err != nil {
		return nil, err
	}
	dir, err := withdrawalArchive(staged, published, retired)
	if err != nil {
		return nil, err
	}
	if active && (dir != retired || !intent.WasActive) {
		return nil, errors.New("withdrawal requires its original stopped panel")
	}
	err = func() error {
		release, err := packages.Acquire(ctx)
		if err != nil {
			return err
		}
		defer release()
		release, err = packages.NativeDPKG(ctx)
		if err != nil {
			return err
		}
		defer release()
		var schedulerSHA string
		if dir != retired {
			if err = panelStopped(ctx); err != nil {
				return err
			}
			schedulerSHA, err = activationProtectedHash("/var/lib/fluxo-scheduler/state.json", 32<<20)
			if err != nil {
				return err
			}
			if err = phpupdate.CheckInstallerUpgradeDatabase(ctx, dataDir); err != nil {
				return err
			}
		}
		if err = activationGatesPresent(); err != nil {
			return err
		}
		if _, err = protectedInfo("/var/lib/fluxo-scheduler.init.lock"); err != nil {
			return err
		}
		locks, err := acquireActivationLocks(ctx)
		if err != nil {
			return err
		}
		defer locks.release()
		current, err := withdrawalArchive(staged, published, retired)
		if err != nil || current != dir {
			return errors.New("installer renewal archive moved during admission")
		}
		var observed []withdrawalObservation
		var observations *[]withdrawalObservation
		if dir != retired {
			observations = &observed
		}
		if historical {
			if e := trustedDirectory(filepath.Dir(published)); e != nil && !os.IsNotExist(e) {
				return e
			}
			if err := checkHistoricalArchiveNamespace(filepath.Dir(published), dir); err != nil {
				return err
			}
		}
		plan, err := readWithdrawalArchive(dir, *intent, historical, observations)
		if err != nil {
			return err
		}
		if historical && dir != retired {
			if dir == published && !historicalFirstWriteDistinct(plan) {
				return errors.New("ambiguous first historical transition write")
			}
			if err = validateHistoricalSourcesCurrent(plan); err != nil {
				return err
			}
			for i, f := range plan.Files {
				if f.Path == "/var/lib/fluxo-system-tasks/state.json" {
					before, e := readProtected(filepath.Join(dir, fmt.Sprintf("before-%d", i)), 8<<20)
					if e != nil {
						return e
					}
					if e = maintenancetask.ValidateHistoricalCompactionLocked(ctx, before); e != nil {
						return e
					}
				}
			}
			if err = verifyRecoveryInvocationsLocked(ctx, plan); err != nil {
				return err
			}
			if err = verifyHistoricalTaskRetirements(ctx, activationRequest{}, plan); err != nil {
				return err
			}
		}
		if !historical && plan.SourceFloorSHA256 != h.SourceFloorSHA256 {
			return errors.New("renewal plan requires its precise original floor")
		}
		if err = packages.TinkerDomainsIdle(); err != nil {
			return err
		}
		info, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		st := info.Sys().(*syscall.Stat_t)
		metadataPath := filepath.Join(caseDir, "observed-targets.json")
		var metadata []byte
		if dir == retired {
			metadata, err = readProtected(metadataPath, 8<<20)
		} else {
			metadata, err = json.Marshal(observed)
		}
		if err != nil {
			return err
		}
		proof, err := json.Marshal(struct {
			Operation, Intent, Plan, Floor, Observed string
			Device, Inode                            uint64
		}{intent.Operation, digest(raw), intent.Plan, plan.SourceFloorSHA256, digest(metadata), uint64(st.Dev), st.Ino})
		if err != nil {
			return err
		}
		if dir != retired {
			now, err := activationProtectedHash("/var/lib/fluxo-scheduler/state.json", 32<<20)
			if err != nil || now != schedulerSHA {
				return errors.New("Scheduler changed during withdrawal admission")
			}
			id, db, err := activationDatabase(ctx, dataDir, false)
			if err != nil {
				return err
			}

			aux, err := activationAuxiliary()
			if err != nil {
				return err
			}
			if err = validateWithdrawalSources(plan, id, db, aux); err != nil {
				return err
			}
			if err = ensureActivationParent(parent); err != nil {
				return err
			}
			if err = ensureActivationParent(caseDir); err != nil {
				return err
			}
		}
		if err = withdrawalRecord(filepath.Join(caseDir, "original-intent.json"), raw, dir != retired); err != nil {
			return err
		}
		// Before retirement, historical ticks can change an observed inode
		// between attempts. Recreate the snapshot and its seal only after
		// full readmission. After rename both records are verification-only.
		record := func(name string, value []byte) error {
			if historical && dir != retired {
				return writeActivationFile(name, value, 0600)
			}
			return withdrawalRecord(name, value, dir != retired)
		}
		if err = record(metadataPath, metadata); err != nil {
			return err
		}
		if err = record(filepath.Join(caseDir, "validated.json"), proof); err != nil {
			return err
		}
		if _, err = os.Lstat(filepath.Join(caseDir, "restored-intent.json")); !os.IsNotExist(err) {
			return errors.New("withdrawal already closed or final intent is ambiguous")
		}
		if err = locks.verify(); err != nil {
			return err
		}
		if dir != retired {
			if active, err := binding(); err != nil {
				return err
			} else if active {
				return errors.New("original panel resumed before withdrawal")
			}
			if err = panelStopped(ctx); err != nil {
				return err
			}
			if err = withdrawalRename(dir, retired); err != nil {
				return err
			}
			if historical {
				return finishHistoricalArchiveMove(filepath.Dir(published), retired)
			}
			return nil
		}
		// Repeat both parent syncs if the original process died after rename.
		if historical {
			if err := syncActivationDirectory(caseDir); err != nil {
				return err
			}
			return finishHistoricalArchiveMove(filepath.Dir(published), retired)
		}
		for _, p := range []string{caseDir, dataDir, filepath.Dir(published)} {
			if err := syncActivationDirectory(p); err != nil {
				return err
			}
		}
		return nil
	}()
	if err != nil {
		return nil, err
	}
	if err = finishWithdrawnRenewal(intent.WasActive, binding, func() error {
		_, err := run(ctx, "/usr/bin/systemctl", "start", "fluxo.service")
		return err
	}, func() error {
		return withdrawalRename(path, filepath.Join(caseDir, "restored-intent.json"))
	}); err != nil {
		return nil, err
	}
	return map[string]string{"operation_id": intent.Operation, "phase": "withdrawn"}, nil
}

// A crash after starting the service leaves the original intent active. The
// next pass recognizes its bound running PID and only closes the exact pointer.
func finishWithdrawnRenewal(wasActive bool, binding func() (bool, error), start, closeIntent func() error) error {
	active, err := binding()
	if err != nil {
		return err
	}
	if wasActive && !active {
		if err = start(); err != nil {
			return err
		}
	}
	active, err = binding()
	if err != nil {
		return err
	}
	if active != wasActive {
		return errors.New("original panel state has not been restored")
	}
	return closeIntent()
}

func validateWithdrawalSources(plan executionActivation, id string, db, aux map[string]string) error {
	if id != plan.InstallationID || !sameHashes(plan.Database, db) {
		return errors.New("installation database changed before withdrawal")
	}
	if !activationAuxiliaryMatches(plan, aux) {
		return errors.New("coordination evidence changed before withdrawal")
	}
	return nil
}
