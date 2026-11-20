package bridge

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/safeinput"
	gitservice "fluxo/internal/services/git"
)

const githubLinkCandidateWhere = `COALESCE(github_account_id,0)=0 AND COALESCE(repository,'')<>'' AND COALESCE(github_disconnected_user_id,0)=0 AND COALESCE(github_disconnected_username,'')='' AND COALESCE(deletion_status,'')=''`

type RestoredGitHubLink struct {
	SiteID    int `json:"site_id"`
	AccountID int `json:"account_id"`
}
type SkippedGitHubLink struct {
	SiteID int    `json:"site_id"`
	Reason string `json:"reason"`
}
type InstallerGitHubLinksReport struct {
	Affected int                  `json:"affected"`
	Restored []RestoredGitHubLink `json:"restored,omitempty"`
	Skipped  []SkippedGitHubLink  `json:"skipped,omitempty"`
	Backup   string               `json:"backup,omitempty"`
	Warnings []string             `json:"warnings,omitempty"`
}
type githubLinkAccount struct {
	ID    int
	Owner int64
	Token string
}
type githubLinkPlan struct {
	SiteID, AccountID  int
	Repository, Branch string
	PreviousAccountID  int
}
type githubLinkVerifier interface {
	GetIdentity(context.Context) (gitservice.Identity, error)
	CheckRepositoryBranch(context.Context, string, string) error
}
type githubLinkDependencies struct {
	key      func() ([]byte, error)
	provider func(string) githubLinkVerifier
	backup   func([]githubLinkPlan) (string, error)
}

// RestoreInstallerGitHubLinks runs only from the installed bridge's normal
// installer. It never initializes a database/key, changes a credential, touches
// SSH keys, or grants execution authority. The shell treats errors as warnings.
func RestoreInstallerGitHubLinks(ctx context.Context, dataDir string) (InstallerGitHubLinksReport, error) {
	var report InstallerGitHubLinksReport
	if os.Geteuid() != 0 || os.Getenv("FLUXO_ENV") != "prod" || dataDir != "/var/lib/fluxo" {
		return report, errors.New("GitHub link recovery requires the installed root production scope")
	}
	exe, err := os.Executable()
	if err != nil || exe != "/usr/local/bin/fluxo" {
		return report, errors.New("use the installed Fluxo bridge for link recovery")
	}
	installed, err := protectedInfo(exe)
	running, runningErr := os.Stat("/proc/self/exe")
	if err != nil || runningErr != nil || !os.SameFile(installed, running) {
		return report, errors.New("installed Fluxo image changed; retry with the installed bridge")
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	db, err := open(ctx, dataDir, true)
	if err != nil {
		return report, err
	}
	defer db.Close()
	report, err = restoreGitHubLinks(ctx, db, githubLinkDependencies{
		key:      func() ([]byte, error) { return readKey(dataDir) },
		provider: func(token string) githubLinkVerifier { return gitservice.NewGitHubProvider(token) },
		backup: func(plans []githubLinkPlan) (string, error) {
			folder, e := os.MkdirTemp(dataDir, "github-link-recovery-")
			if e != nil {
				return "", e
			}
			path := filepath.Join(folder, "before.json")
			// Every entry was unlinked (account 0). This private, fsynced record
			// retains the exact saved settings and proposed links, without tokens.
			e = writeActivationJSON(path, struct {
				Format  int              `json:"format"`
				Created time.Time        `json:"created"`
				Links   []githubLinkPlan `json:"links"`
			}{1, time.Now().UTC(), plans})
			if e == nil {
				e = syncActivationDirectory(dataDir)
			}
			return path, e
		},
	})
	if report.Affected > 0 {
		if err != nil {
			report.Warnings = append(report.Warnings, "Automatic recovery could not finish; review the affected links before retrying")
		}
		// Troubleshooting metadata only; never used to authorize a later repair.
		if e := writeActivationJSON(filepath.Join(dataDir, "github-link-recovery-last.json"), report); e != nil && err == nil {
			err = e
		}
	}
	return report, err
}

func readGitHubLinkAccounts(ctx context.Context, q queryer) ([]githubLinkAccount, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,github_user_id,token FROM github_accounts ORDER BY id LIMIT 9`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []githubLinkAccount{}
	for rows.Next() {
		var a githubLinkAccount
		if err = rows.Scan(&a.ID, &a.Owner, &a.Token); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

func restoreGitHubLinks(ctx context.Context, db *sql.DB, deps githubLinkDependencies) (InstallerGitHubLinksReport, error) {
	report := InstallerGitHubLinksReport{}
	rows, err := db.QueryContext(ctx, `SELECT id,repository,COALESCE(branch,'') FROM sites WHERE `+githubLinkCandidateWhere+` ORDER BY id LIMIT 129`)
	if err != nil {
		return report, err
	}
	sites := []githubLinkPlan{}
	for rows.Next() {
		var s githubLinkPlan
		if err = rows.Scan(&s.SiteID, &s.Repository, &s.Branch); err != nil {
			rows.Close()
			return report, err
		}
		sites = append(sites, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return report, err
	}
	report.Affected = len(sites)
	if len(sites) == 0 {
		return report, nil
	} // Healthy and intentionally disconnected sites incur no network/key work.
	if len(sites) > 128 {
		sites = sites[:128]
		report.Warnings = append(report.Warnings, "Additional unlinked sites exceed this run's recovery limit")
	}
	accounts, err := readGitHubLinkAccounts(ctx, db)
	if err != nil {
		return report, err
	}
	skipAll := func(reason string) {
		for _, s := range sites {
			report.Skipped = append(report.Skipped, SkippedGitHubLink{s.SiteID, reason})
		}
	}
	if len(accounts) == 0 || len(accounts) > 8 {
		skipAll("No unique account can be verified within the recovery limits")
		return report, nil
	}
	key, err := deps.key()
	if err != nil {
		return report, errors.New("Existing encryption key could not be read; links were preserved")
	}
	providers := make([]githubLinkVerifier, len(accounts))
	for i, a := range accounts {
		token, e := config.DecryptSecretWithKey(a.Token, key)
		if e != nil || token == "" {
			skipAll("A saved credential could not be verified; links were preserved")
			return report, nil
		}
		providers[i] = deps.provider(token)
		checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		identity, e := providers[i].GetIdentity(checkCtx)
		cancel()
		if e != nil || identity.ID <= 0 || a.Owner != 0 && a.Owner != identity.ID {
			skipAll("A connected account could not be verified; links were preserved")
			return report, nil
		}
	}
	plans := []githubLinkPlan{}
	requests := len(accounts)
	for _, site := range sites {
		if len(site.Repository) > 256 || !safeinput.ValidateRepoFullName(site.Repository) || len(site.Branch) > 1024 || !safeinput.ValidateGitRef(site.Branch) {
			report.Skipped = append(report.Skipped, SkippedGitHubLink{site.SiteID, "Saved repository or branch needs review"})
			continue
		}
		matches := []int{}
		unavailable := false
		for i, p := range providers {
			if requests >= 64 || ctx.Err() != nil {
				unavailable = true
				break
			}
			requests++
			checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			e := p.CheckRepositoryBranch(checkCtx, site.Repository, site.Branch)
			cancel()
			if e == nil {
				matches = append(matches, accounts[i].ID)
				continue
			}
			var apiErr *gitservice.APIError
			if !errors.As(e, &apiErr) || apiErr.Status != 404 {
				unavailable = true
			}
		}
		switch {
		case unavailable:
			report.Skipped = append(report.Skipped, SkippedGitHubLink{site.SiteID, "Repository matching unavailable; retry after connectivity or account access is restored"})
		case len(matches) > 1:
			report.Skipped = append(report.Skipped, SkippedGitHubLink{site.SiteID, "More than one connected account can access this repository"})
		case len(matches) == 0:
			report.Skipped = append(report.Skipped, SkippedGitHubLink{site.SiteID, "No connected account verified the saved repository and branch"})
		default:
			site.AccountID = matches[0]
			plans = append(plans, site)
		}
	}
	if len(plans) == 0 {
		return report, nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	current, err := readGitHubLinkAccounts(ctx, tx)
	if err != nil {
		return report, err
	}
	if !reflect.DeepEqual(accounts, current) {
		return report, errors.New("Connected accounts changed during recovery; retry the installer")
	}
	currentKey, keyErr := deps.key()
	if keyErr != nil || !bytes.Equal(key, currentKey) {
		return report, errors.New("Encryption key changed during recovery; links were preserved")
	}
	ready := []githubLinkPlan{}
	for _, p := range plans {
		var eligible bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sites WHERE id=? AND repository=? AND COALESCE(branch,'')=? AND `+githubLinkCandidateWhere+` AND NOT EXISTS(SELECT 1 FROM deployments WHERE site_id=sites.id AND status IN ('pending','queued','running')))`, p.SiteID, p.Repository, p.Branch).Scan(&eligible)
		if err != nil {
			return report, err
		}
		if eligible {
			ready = append(ready, p)
		} else {
			report.Skipped = append(report.Skipped, SkippedGitHubLink{p.SiteID, "Site changed or has a pending deployment; link preserved"})
		}
	}
	if len(ready) == 0 {
		return report, nil
	}
	report.Backup, err = deps.backup(ready)
	if err != nil {
		return report, errors.New("Could not preserve original GitHub associations; links were not changed")
	}
	for _, p := range ready {
		result, e := tx.ExecContext(ctx, `UPDATE sites SET github_account_id=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND repository=? AND COALESCE(branch,'')=? AND `+githubLinkCandidateWhere, p.AccountID, p.SiteID, p.Repository, p.Branch)
		if e != nil {
			return report, e
		}
		n, e := result.RowsAffected()
		if e != nil || n != 1 {
			return report, errors.New("Site changed during recovery; links were not changed")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO activity(type,summary,username) VALUES('github_links_restored',?,'installer')`, fmt.Sprintf("Installer restored GitHub account %d link for site %d", p.AccountID, p.SiteID))
		if err != nil {
			return report, err
		}
	}
	if err = tx.Commit(); err != nil {
		return report, err
	}
	for _, p := range ready {
		report.Restored = append(report.Restored, RestoredGitHubLink{p.SiteID, p.AccountID})
	}
	return report, nil
}

func PrintInstallerGitHubLinks(out io.Writer, report InstallerGitHubLinksReport, err error) {
	if len(report.Restored) > 0 {
		fmt.Fprintf(out, "Restored GitHub account links for %d site(s).\n", len(report.Restored))
	}
	if err != nil {
		fmt.Fprintln(out, "WARNING: GitHub account-link recovery needs attention. Fluxo update can continue; review Source Control or retry this bridge installer.")
	}
}
