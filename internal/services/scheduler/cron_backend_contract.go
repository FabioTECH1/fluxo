package scheduler

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

// CronBackendIdentity contains only provider evidence. System maintenance jobs
// use this contract without inventing a PHP branch or modifying Scheduler state.
type CronBackendIdentity = legacyHost

const legacyNobleCronContract = "ubuntu-noble-cron-v1"

// BackendReaderContracts names immutable meanings, not host qualification.
// The empty historical backend field always denotes the original Noble proof.
func BackendReaderContracts() []string {
	return []string{legacyNobleCronContract, "cronie-noble-1", jammyCronCandidateID, resoluteCronCandidateID}
}

func newDistributionBackend(id string) bool {
	return id == jammyCronCandidateID || id == resoluteCronCandidateID
}

// BackendIdentityRequiresCurrent prevents an older core from assigning new
// provider authority to a backend contract it never persisted or understood.
func BackendIdentityRequiresCurrent(identity CronBackendIdentity) bool {
	return newDistributionBackend(identity.Backend)
}

func backendReaderContract(identity CronBackendIdentity) string {
	if identity.Backend == "" {
		return legacyNobleCronContract
	}
	return identity.Backend
}

// BackendReaderContract names the reader role required by an embedded identity.
// Callers validate the identity before granting authority under this contract.
func BackendReaderContract(identity CronBackendIdentity) string {
	return backendReaderContract(identity)
}

// ValidateBackendIdentity validates persisted meaning without observing today's
// selected host or rewriting historical receipts to a newer backend identity.
func ValidateBackendIdentity(identity CronBackendIdentity) error {
	if _, err := uuid.Parse(identity.Boot); err != nil || identity.PID <= 1 || identity.Start == "" || identity.GroupInode == 0 {
		return errors.New("invalid cron backend execution identity")
	}
	switch identity.Backend {
	case "":
		if identity.Group != "" || identity.Configuration != "" {
			return errors.New("historical Noble identity has unrecognized backend fields")
		}
	case "cronie-noble-1":
		if identity.Group != cronieAdapter.group {
			return errors.New("invalid Cronie execution domain")
		}
	case jammyCronCandidateID, resoluteCronCandidateID:
		if identity.Group != "/system.slice/cron.service" {
			return errors.New("invalid distribution cron execution domain")
		}
	default:
		return errors.New("unknown cron backend contract")
	}
	if identity.Backend != "" {
		raw, err := hex.DecodeString(identity.Configuration)
		if err != nil || len(raw) != 32 || strings.ToLower(identity.Configuration) != identity.Configuration {
			return errors.New("invalid cron backend configuration digest")
		}
	}
	return nil
}

// RequireCronBackendWrite must precede every new distribution-ID publication.
// The caller first validates the helper against its durable path/digest record;
// this function additionally reads that actual protected image's capabilities.
// Legacy histories remain readable without acquiring new execution authority.
func RequireCronBackendWrite(ctx context.Context, identity CronBackendIdentity, helperPath string) error {
	if err := ValidateBackendIdentity(identity); err != nil {
		return err
	}
	if !newDistributionBackend(identity.Backend) {
		return nil
	}
	if !filepath.IsAbs(helperPath) || filepath.Clean(helperPath) != helperPath {
		return errors.New("cron backend writer needs its exact helper path")
	}
	if err := trustedFile(helperPath); err != nil {
		return err
	}
	before, err := os.Stat(helperPath)
	if err != nil {
		return err
	}
	beforeSHA, err := fileDigest(helperPath)
	if err != nil {
		return err
	}
	out, err := syscmd.Run(ctx, 5*time.Second, helperPath, "--execution-capabilities")
	if err != nil {
		return errors.New("cron backend helper does not report compatible readers")
	}
	var capabilities executiondomain.ReaderCapabilities
	if err = json.Unmarshal([]byte(out), &capabilities); err != nil {
		return errors.New("cron backend helper capabilities are malformed")
	}
	after, err := os.Stat(helperPath)
	if err != nil || !os.SameFile(before, after) {
		return errors.New("cron backend helper changed during inspection")
	}
	afterSHA, err := fileDigest(helperPath)
	if err != nil || beforeSHA != afterSHA {
		return errors.New("cron backend helper payload changed during inspection")
	}
	if err = trustedFile(helperPath); err != nil {
		return err
	}
	if err = executiondomain.GuardBackendWrite("/var/lib/fluxo", backendReaderContract(identity), capabilities); err != nil {
		return fmt.Errorf("cron backend writer is not admitted: %w", err)
	}
	return nil
}

func QualifyCronBackend(ctx context.Context) (CronBackendIdentity, error) {
	return qualifyBackendHost(ctx)
}

// RequalifyCronBackend observes the current provider under the saved immutable
// backend contract; it does not select or publish a different backend contract.
func RequalifyCronBackend(ctx context.Context, identity CronBackendIdentity) (CronBackendIdentity, error) {
	now, err := requalifyBackendIdentity(ctx, identity)
	if err != nil {
		return CronBackendIdentity{}, err
	}
	if now.Backend != identity.Backend {
		return CronBackendIdentity{}, errors.New("Scheduled-job backend contract changed; explicit migration is required")
	}
	return now, nil
}

// VerifyCronBackendCurrent checks generation and configuration independently
// of whether the caller originated in cron or the provider hierarchy is empty.
func VerifyCronBackendCurrent(ctx context.Context, identity CronBackendIdentity) error {
	now, err := requalifyBackendIdentity(ctx, identity)
	if err != nil {
		return err
	}
	if now != identity {
		return errors.New("Scheduled-job engine identity changed")
	}
	return nil
}

func VerifyCronReceiptOrigin(ctx context.Context, identity CronBackendIdentity) error {
	if err := VerifyCronBackendCurrent(ctx, identity); err != nil {
		return err
	}
	return cronReceiptOrigin(identity)
}

func VerifyCronBackendEmpty(ctx context.Context, identity CronBackendIdentity) error {
	return legacyDomainDrained(ctx, identity)
}

// PublishSystemTaskCron accepts only exact durable predecessors in the fixed
// cron directory. Publication is not evidence that cron has loaded the file.
func PublishSystemTaskCron(name string, predecessors [][]byte, replacement []byte) error {
	if name == "" || filepath.Base(name) != name || !strings.HasPrefix(name, "fluxo-") || strings.ContainsAny(name, "\x00\r\n") {
		return errors.New("Invalid system-task cron path")
	}
	path := filepath.Join(cronDir, name)
	if err := owned(cronDir, true); err != nil {
		return err
	}
	err := trustedFile(path)
	var raw []byte
	if err == nil {
		raw, err = os.ReadFile(path)
	} else if !os.IsNotExist(err) {
		return err
	}
	matched := false
	for _, p := range predecessors {
		if (p == nil && os.IsNotExist(err)) || (p != nil && err == nil && string(p) == string(raw)) {
			matched = true
		}
	}
	if !matched {
		return errors.New("Scheduled-job trigger differs from its saved predecessor")
	}
	return publishCron(path, replacement)
}
