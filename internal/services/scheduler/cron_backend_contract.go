package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// CronBackendIdentity contains only provider evidence. System maintenance jobs
// use this contract without inventing a PHP branch or modifying Scheduler state.
type CronBackendIdentity = legacyHost

func QualifyCronBackend(ctx context.Context) (CronBackendIdentity, error) {
	return qualifyBackendHost(ctx)
}

func VerifyCronReceiptOrigin(ctx context.Context, identity CronBackendIdentity) error {
	now, err := qualifyBackendHost(ctx)
	if err != nil {
		return err
	}
	if now != identity {
		return errors.New("Scheduled-job engine identity changed")
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
