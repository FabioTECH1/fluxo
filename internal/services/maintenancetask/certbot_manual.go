package maintenancetask

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const certbotManualProfileKey = "certbot-manual-renewal-contract"
const certbotManualNoHooksContract = "certbot-manual-nohooks-v1"
const manualCertbotNotice = "Some certificates use manual validation and cannot renew automatically. This update keeps their SSL configuration unchanged."

// Even without auth hooks, the vendor manual plugin constructs a Reverter
// BEFORE prepare() rejects noninteractive renewal. Its recovery routine can
// execute undo commands and restore/delete files from unfinished checkpoints.
// Never authorize that work as an incidental effect of this optional profile.
func qualifyManualCertbotReverter(p *profile, work string) error {
	return inspectManualCertbotReverter(p, work, trustedDatabaseDirectory, os.Lstat, p.directory)
}

func inspectManualCertbotReverter(p *profile, work string, trust func(string) error, lstat func(string) (os.FileInfo, error), directory func(string, bool) ([]os.DirEntry, error)) error {
	if err := trust(work); err != nil {
		return err
	}
	for _, name := range []string{"temp_checkpoint", "IN_PROGRESS"} {
		path := filepath.Join(work, name)
		if _, err := lstat(path); !os.IsNotExist(err) {
			if err != nil {
				return err
			}
			return fmt.Errorf("Certbot has unfinished rollback state at %s; preserve it for review before runtime maintenance", path)
		}
		p.Files[path] = "absent"
	}
	// Construction may create an empty backups directory, but never reads or
	// executes finalized backups. Existing membership is still retained.
	_, err := directory(filepath.Join(work, "backups"), true)
	return err
}

// This bounded, read-only notice does not establish execution authority. The
// ordinary profile/helper/settlement checks remain authoritative. A notice is
// useful for an already registered job too: coordination must not imply that
// a manually issued certificate has acquired working automatic renewal.
func certbotRenewalNotice(ctx context.Context, root string) string {
	const review = "Certificate renewal configuration needs separate review; this update does not repair SSL renewal."
	for _, path := range []string{"/etc/letsencrypt/cli.ini", "/root/.config/letsencrypt/cli.ini"} {
		raw, err := readRegular(path, 1<<20)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return review
		}
		if _, err = validateCurrentCertbotConfig(raw, false); err != nil {
			return review
		}
	}
	p := newProfile(CertificateRenewal)
	entries, err := p.directory(root, true)
	if err != nil || len(entries) > 256 {
		return review
	}
	remaining := 8 << 20
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ""
		}
		if !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		raw, err := readRegular(filepath.Join(root, entry.Name()), 1<<20)
		if err != nil {
			return review
		}
		remaining -= len(raw)
		if remaining < 0 {
			return review
		}
		manual, err := validateCurrentCertbotConfig(raw, true)
		if err != nil {
			return review
		}
		if manual {
			return manualCertbotNotice
		}
	}
	return ""
}

var errTaskRegistrationRequired = errors.New("Scheduled-job coordination is incomplete; check Fluxo's scheduled-job diagnostics before runtime maintenance")
