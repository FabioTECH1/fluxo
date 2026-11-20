package bridge

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"fluxo/internal/services/maintenancetask"
	"golang.org/x/sys/unix"
)

const (
	installerBackendRenewal = "backend-renewal"
	installerHelperRefresh  = "helper-refresh"
)

// This is a preparation retry, never replay of a published operation. The
// activation callback records its UUID before any target can be written.
var errInstallerPreparationChanged = errors.New("installer task preparation changed before publication; reviewing again")

type installerTaskSelection struct{ mode, core, backend string }

func validInstallerHash(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && strings.ToLower(value) == value
}

func selectInstallerTaskTransition(ctx context.Context,
	backend func(context.Context) (*maintenancetask.RegistrationBackendReview, error),
	helper func(context.Context) (*maintenancetask.HelperRefreshReview, error),
) (installerTaskSelection, error) {
	var selection installerTaskSelection
	review, err := backend(ctx)
	if err != nil {
		return selection, err
	}
	if review != nil {
		if !validInstallerHash(review.TaskCoreSHA256) || !validInstallerHash(review.CurrentBackendSHA256) {
			return selection, errors.New("invalid installer backend review binding")
		}
		return installerTaskSelection{installerBackendRenewal, review.TaskCoreSHA256, review.CurrentBackendSHA256}, nil
	}
	refresh, err := helper(ctx)
	if err != nil {
		return selection, err
	}
	if refresh == nil {
		return selection, nil
	}
	if !validInstallerHash(refresh.TaskCoreSHA256) {
		return selection, errors.New("invalid installer helper review binding")
	}
	return installerTaskSelection{mode: installerHelperRefresh, core: refresh.TaskCoreSHA256}, nil
}

func reviewInstallerTaskTransition(ctx context.Context) (installerTaskSelection, error) {
	coordinate := func(inspect func() error) error {
		unlock, err := lockCoordination(ctx)
		if err != nil {
			return err
		}
		defer unlock()
		return inspect()
	}
	return reviewInstallerTaskWithWait(ctx, coordinate, maintenancetask.InstallerRegistrationReviewLocked,
		maintenancetask.InstallerHelperRefreshReviewLocked, func() error { return maintenancetask.WaitActivationAcknowledgements(ctx) })
}

func reviewInstallerTaskWithWait(ctx context.Context, coordinate func(func() error) error,
	inspectBackend func(context.Context) (*maintenancetask.RegistrationBackendReview, error),
	inspectHelper func(context.Context) (*maintenancetask.HelperRefreshReview, error), await func() error,
) (installerTaskSelection, error) {
	var backend *maintenancetask.RegistrationBackendReview
	err := coordinate(func() error {
		var err error
		backend, err = inspectBackend(ctx)
		return err
	})
	if err != nil {
		return installerTaskSelection{}, err
	}
	if backend != nil {
		return selectInstallerTaskTransition(ctx, func(context.Context) (*maintenancetask.RegistrationBackendReview, error) { return backend, nil }, nil)
	}
	// Fresh helpers may still be waiting for their first real receipt. Do not
	// hold the task gate while waiting, and do not wait on an obsolete provider
	// before its separately reviewed backend renewal above.
	if err = await(); err != nil {
		return installerTaskSelection{}, err
	}
	var selected installerTaskSelection
	err = coordinate(func() error {
		var err error
		selected, err = selectInstallerTaskTransition(ctx, inspectBackend, inspectHelper)
		return err
	})
	return selected, err
}

// Before an operation UUID exists these expectations may be refreshed by a
// new gated inspection. They grant no target-write authority. Once an archive
// is bound, only exact image/UUID/plan replay is allowed.
func bindInstallerTaskSelection(intent *installerExecutionIntent, selected installerTaskSelection) error {
	if intent.Operation != "" || intent.Plan != "" || intent.Phase != "pending" || selected.mode == "" {
		return errors.New("cannot replace a durable installer operation with a task review")
	}
	next := *intent
	next.Format, next.TaskTransition = 2, selected.mode
	next.TaskCore, next.TaskBackend = selected.core, selected.backend
	if err := validateInstallerTaskFields(next); err != nil {
		return err
	}
	*intent = next
	return nil
}

func validateInstallerTaskFields(v installerExecutionIntent) error {
	if v.Format == 1 {
		if v.TaskTransition != "" {
			return errors.New("historical installer intent cannot select a new task transition")
		}
		if v.TaskCore != "" || v.TaskBackend != "" {
			if !validInstallerHash(v.TaskCore) || !validInstallerHash(v.TaskBackend) {
				return errors.New("invalid installer task-backend expectations")
			}
		}
		return nil
	}
	if v.Format != 2 || !validInstallerHash(v.TaskCore) {
		return errors.New("invalid current installer task intent")
	}
	switch v.TaskTransition {
	case installerBackendRenewal:
		if !validInstallerHash(v.TaskBackend) {
			return errors.New("installer backend renewal lost its qualification")
		}
	case installerHelperRefresh:
		if v.TaskBackend != "" {
			return errors.New("helper refresh cannot switch backend qualification")
		}
	default:
		return errors.New("unknown installer task transition")
	}
	return nil
}

// The panel is restored before retiring a preparation with no UUID. No
// activation directory or target can have been published by that preparation;
// complete orphaned staging evidence is retained. The original orchestration
// bytes are archived, not used as an execution or acknowledgement receipt.
func archiveUnpreparedInstallerIntent(dataDir string, want installerExecutionIntent) error {
	if want.Format != 2 || want.Phase != "pending" || want.Operation != "" || want.Plan != "" || validateInstallerTaskFields(want) != nil {
		return errors.New("only an unprepared current task intent can be retired")
	}
	path := filepath.Join(dataDir, installerExecutionRecord)
	raw, err := readProtected(path, 16<<10)
	if err != nil {
		return err
	}
	actual, err := decodeInstallerExecutionIntent(raw)
	if err != nil || *actual != want {
		return errors.New("installer preparation changed before retirement")
	}
	dir := filepath.Join(dataDir, "execution-installer-preparations")
	if err = ensureActivationParent(dir); err != nil {
		return err
	}
	dest := filepath.Join(dir, digest(raw)+".json")
	if saved, e := readProtected(dest, 16<<10); e == nil {
		if !bytes.Equal(saved, raw) {
			return errors.New("archived installer preparation changed")
		}
		if err = os.Remove(path); err != nil {
			return err
		}
	} else if !os.IsNotExist(e) {
		return e
	} else if err = unix.Renameat2(unix.AT_FDCWD, path, unix.AT_FDCWD, dest, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	if err = syncActivationDirectory(dir); err != nil {
		return err
	}
	return syncActivationDirectory(dataDir)
}
