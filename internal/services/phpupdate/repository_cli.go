package phpupdate

import (
	"context"
	"encoding/json"
	"fluxo/internal/database"
	"fmt"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"time"
)

// SetupRepositoryFromInstaller is the automatic installer action, using the same
// review, supervisor, journal and audit records as the panel for existing hosts.
func SetupRepositoryFromInstaller(ctx context.Context, dataDir string) error {
	if os.Geteuid() != 0 || !filepath.IsAbs(dataDir) {
		return ErrSupervisor
	}
	dbPath := filepath.Join(dataDir, "fluxo.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return BootstrapRepository(ctx, filepath.Join(dataDir, "php-repository"))
	} else if err != nil {
		return err
	}
	if err := database.InitDB(dbPath); err != nil {
		return err
	}
	defer database.DB.Close()
	m := NewManager(dataDir)
	wait := func(o Operation) (Operation, error) {
		phase := ""
		for {
			// Reconcile only after the existing terminal-unit/empty-cgroup
			// proof. Installer retry must also work with the daemon stopped.
			m.reconcile(ctx)
			current, err := GetOperation(o.ID)
			if err != nil {
				return current, err
			}
			if current.Phase != phase {
				fmt.Println(current.Message)
				phase = current.Phase
			}
			if !current.Active() {
				if current.Status != "succeeded" {
					return current, fmt.Errorf("%s", current.Message)
				}
				return current, nil
			}
			select {
			case <-ctx.Done():
				return current, ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
	m.reconcile(ctx)
	if active, err := ActiveOperation(); err != nil {
		return err
	} else if active != nil {
		if active.Kind != "repository_review" && active.Kind != "repository_apply" {
			return ErrBusy
		}
		// Join existing repository work instead of publishing another plan.
		// Proven interruption may leave an intent that the status below resumes.
		finished, err := wait(*active)
		if err != nil && finished.Status != "interrupted" {
			return err
		}
	}
	status := m.RepositoryStatus(ctx)
	if status.State == "current" {
		fmt.Println(status.Message)
		return nil
	}
	planID := status.ResumePlanID
	if planID == "" {
		o, err := m.EnqueueRepository(ctx, "repository_review", "", uuid.NewString(), "installer", "")
		if err != nil {
			return err
		}
		done, err := wait(o)
		if err != nil {
			return err
		}
		var review RepositoryReview
		if err = json.Unmarshal(done.Result, &review); err != nil {
			return err
		}
		if len(review.RetainedExtensions) > 0 {
			fmt.Printf("Keeping verified installed extension revisions: %v\n", review.RetainedExtensions)
		}
		if len(review.RollbackUnavailable) > 0 {
			fmt.Printf("Previous packages unavailable for complete rollback: %v. Any later PHP update will require acknowledgement if coverage remains incomplete.\n", review.RollbackUnavailable)
		}
		planID = review.ID
	}
	o, err := m.EnqueueRepository(ctx, "repository_apply", planID, uuid.NewString(), "installer", "")
	if err != nil {
		return err
	}
	_, err = wait(o)
	return err
}
