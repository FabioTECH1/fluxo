package maintenancetask

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"fluxo/internal/services/scheduler"
	"github.com/google/uuid"
)

// Receipt is a review-bound stable contract. It contains no transient job PIDs.
var ErrUnsupportedProfile = errors.New("This scheduled-job implementation needs additional maintenance qualification")

type Receipt struct {
	Name     string   `json:"name"`
	Domain   string   `json:"domain,omitempty"`
	Contract Contract `json:"contract"`
	Identity string   `json:"identity"`
	Database string   `json:"database"`
	Helper   string   `json:"helper"`
	Profile  string   `json:"profile"`
}

func cronPath(id int) string { return filepath.Join("/etc/cron.d", "fluxo-cron-"+strconv.Itoa(id)) }
func LegacyContent(c Contract) string {
	prefix := ""
	if c.SiteID > 0 {
		prefix = "cd " + c.Directory + " && "
	}
	return fmt.Sprintf("# Fluxo Cron ID: %d\n%s %s %s%s >> /var/log/fluxo/cron-%d.log 2>&1\n", c.ID, c.Expression, c.User, prefix, c.Command, c.ID)
}
func cronContent(r registration) string {
	return fmt.Sprintf("# Fluxo Cron ID: %d\n%s root %s system-task-dispatch %d %s >> /var/log/fluxo/cron-%d.log 2>&1\n* * * * * root %s system-task-receipt %d %s\n", r.Contract.ID, r.Contract.Expression, r.Helper, r.Contract.ID, r.Identity, r.Contract.ID, r.Helper, r.Contract.ID, r.Nonce)
}

func installHelper() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	b, err := readRegular(exe, 128<<20)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(stateRoot, "helpers")
	if err = os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return "", err
	}
	path := filepath.Join(dir, digest(b))
	if old, e := readRegular(path, 128<<20); e == nil {
		if digest(old) != digest(b) {
			return "", errors.New("Retained system-task helper changed")
		}
		return path, nil
	} else if !os.IsNotExist(e) {
		return "", e
	}
	if err = atomicFile(path, b, 0700); err != nil {
		return "", err
	}
	return path, nil
}

// Materialize never invents enabled intent. Startup reconciliation passes
// create=false, so an absent legacy trigger remains absent. Read-only reviews
// use Inspect and never enter this path.
func Materialize(ctx context.Context, c Contract, database string, create bool) error {
	unlock, err := gate(ctx, true)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	if old, ok := j.Entries[c.ID]; ok {
		if old.Contract != c || old.Database != database {
			return errors.New("System-task registration changed; reconcile the existing registration before replacing it")
		}
		if !old.Enabled {
			return errors.New("System-task registration is retired")
		}
		if err = verifyRegistration(ctx, old); err == nil {
			return nil
		}
		// Publication can be retried only from the exact saved predecessor.
		if old.Published {
			return err
		}
		return publish(&j, old)
	}
	r := registration{Contract: c, Database: database, Identity: uuid.NewString(), Nonce: uuid.NewString(), Enabled: true, Predecessor: LegacyContent(c)}
	if err = withDatabase(ctx, r, false); err != nil {
		return err
	}
	if _, err = qualifyContractProfile(ctx, c); err != nil {
		return fmt.Errorf("%w: %v", ErrUnsupportedProfile, err)
	}
	raw, err := readRegular(cronPath(c.ID), 1<<20)
	if os.IsNotExist(err) {
		if !create {
			return nil
		}
		r.PredecessorAbsent = true
	} else if err != nil {
		return err
	} else if string(raw) != r.Predecessor {
		return errors.New("System-task cron file differs from its exact generated command")
	}
	r.Boot, err = bootID()
	if err != nil {
		return err
	}
	r.Backend, err = scheduler.QualifyCronBackend(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnsupportedProfile, err)
	}
	r.Helper, err = installHelper()
	if err != nil {
		return err
	}
	j.Entries[c.ID] = r
	if err = save(j); err != nil {
		return err
	}
	return publish(&j, r)
}

func publish(j *journal, r registration) error {
	if err := verifyRetainedHelper(r); err != nil {
		return err
	}
	predecessors := [][]byte{[]byte(r.Predecessor), []byte(cronContent(r))}
	// Nil is accepted only for a positively new registration whose create=true
	// call saved its intent before publication. Reconciliation does not recreate.
	if !r.Published && r.PredecessorAbsent {
		predecessors = append(predecessors, nil)
	}
	if err := scheduler.PublishSystemTaskCron("fluxo-cron-"+strconv.Itoa(r.Contract.ID), predecessors, []byte(cronContent(r))); err != nil {
		return err
	}
	r.Published = true
	j.Entries[r.Contract.ID] = r
	return save(*j)
}

func verifyRegistration(ctx context.Context, r registration) error {
	if err := withDatabase(ctx, r, false); err != nil {
		return err
	}
	return verifyRegistrationFiles(r)
}

func verifyRegistrationFiles(r registration) error {
	if !r.Enabled || !r.Published {
		return errors.New("System-task publication is incomplete")
	}
	b, err := readRegular(cronPath(r.Contract.ID), 1<<20)
	if err != nil {
		return err
	}
	if string(b) != cronContent(r) {
		return errors.New("System-task cron trigger was changed")
	}
	return verifyRetainedHelper(r)
}

func verifyRetainedHelper(r registration) error {
	if filepath.Dir(r.Helper) != filepath.Join(stateRoot, "helpers") {
		return errors.New("System-task helper namespace changed")
	}
	b, err := readRegular(r.Helper, 128<<20)
	if err != nil {
		return err
	}
	if digest(b) != filepath.Base(r.Helper) {
		return errors.New("System-task helper content changed")
	}
	return nil
}

func ReceiveReceipt(ctx context.Context, id int, nonce string) error {
	unlock, err := gate(ctx, false)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	r, ok := j.Entries[id]
	if !ok || nonce != r.Nonce {
		return errors.New("System-task receipt does not match its registration")
	}
	// A reload receipt proves provider observation, not execution admission.
	// Row/layout drift must still reject launches, but cannot prevent the old
	// exact guarded authority from being safely revoked after natural drain.
	if err = verifyRegistrationFiles(r); err != nil {
		return err
	}
	host := r.Backend
	if boot, e := bootID(); e != nil {
		return e
	} else if boot != r.Boot {
		host, e = scheduler.QualifyCronBackend(ctx)
		if e != nil {
			return e
		}
	}
	if err = scheduler.VerifyCronReceiptOrigin(ctx, host); err != nil {
		return err
	}
	r.Received = true
	j.Entries[id] = r
	return save(j)
}

func ready(ctx context.Context, r registration) error {
	if err := verifyRegistration(ctx, r); err != nil {
		return err
	}
	return readyBackend(ctx, r)
}

func readyBackend(ctx context.Context, r registration) error {
	boot, err := bootID()
	if err != nil {
		return err
	}
	if boot == r.Boot {
		if !r.Received {
			return errors.New("Waiting for cron to acknowledge the guarded system-task trigger")
		}
		return scheduler.VerifyCronBackendEmpty(ctx, r.Backend)
	}
	// Persistent wrappers predate this boot, so the old loader cannot have any
	// queued legacy command. Still require a qualified current execution domain.
	host, err := scheduler.QualifyCronBackend(ctx)
	if err != nil {
		return err
	}
	return scheduler.VerifyCronBackendEmpty(ctx, host)
}

func Inspect(ctx context.Context, c Contract, database string) (Receipt, error) {
	var result Receipt
	unlock, err := gate(ctx, false)
	if err != nil {
		return result, err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return result, err
	}
	r, ok := j.Entries[c.ID]
	if !ok || r.Contract != c || r.Database != database {
		return result, errors.New("Complete guarded system-task registration before Node.js maintenance")
	}
	if err = ready(ctx, r); err != nil {
		return result, err
	}
	if err = settled(ctx, &j); err != nil {
		return result, err
	}
	// Future launches must qualify their current code/configuration only after
	// acquiring the shared lifecycle lease. With no outstanding invocation,
	// unsupported future task configuration cannot run during maintenance.
	return Receipt{Contract: c, Identity: r.Identity, Database: r.Database, Helper: filepath.Base(r.Helper), Profile: "fixed-dispatch-v1"}, nil
}
