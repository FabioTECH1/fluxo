package bridge

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	gitservice "fluxo/internal/services/git"
)

type linkVerifierFixture struct {
	identity      gitservice.Identity
	identityError error
	check         func(context.Context, string, string) error
}

func (v *linkVerifierFixture) GetIdentity(context.Context) (gitservice.Identity, error) {
	return v.identity, v.identityError
}
func (v *linkVerifierFixture) CheckRepositoryBranch(c context.Context, r, b string) error {
	return v.check(c, r, b)
}

type linkRecoveryFixture struct {
	db               *sql.DB
	key              []byte
	deps             githubLinkDependencies
	providers        map[string]*linkVerifierFixture
	backups          [][]githubLinkPlan
	keyReads, checks int
}

func newLinkRecoveryFixture(t *testing.T) *linkRecoveryFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.db")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := openSQLite(context.Background(), path, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(database.BaseSchema); err != nil {
		t.Fatal(err)
	}
	f := &linkRecoveryFixture{db: db, key: bytes.Repeat([]byte{17}, 32), providers: map[string]*linkVerifierFixture{}}
	f.deps.key = func() ([]byte, error) { f.keyReads++; return f.key, nil }
	f.deps.provider = func(token string) githubLinkVerifier {
		v, ok := f.providers[token]
		if !ok {
			t.Fatalf("unexpected credential supplied to verifier")
		}
		return v
	}
	f.deps.backup = func(plans []githubLinkPlan) (string, error) {
		f.backups = append(f.backups, append([]githubLinkPlan(nil), plans...))
		return "private-before.json", nil
	}
	return f
}
func (f *linkRecoveryFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, e := f.db.Exec(query, args...); e != nil {
		t.Fatal(e)
	}
}
func (f *linkRecoveryFixture) account(t *testing.T, id int, token string) *linkVerifierFixture {
	t.Helper()
	block, _ := aes.NewCipher(f.key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	if _, e := rand.Read(nonce); e != nil {
		t.Fatal(e)
	}
	encrypted := "enc:" + base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(token), nil))
	f.exec(t, "INSERT INTO github_accounts(id,name,username,github_user_id,token) VALUES(?,?,'user',?,?)", id, fmt.Sprint(id), int64(id+100), encrypted)
	v := &linkVerifierFixture{identity: gitservice.Identity{ID: int64(id + 100), Username: "user"}, check: func(context.Context, string, string) error { f.checks++; return nil }}
	f.providers[token] = v
	return v
}
func (f *linkRecoveryFixture) site(t *testing.T, id, account int, repo string) {
	f.exec(t, `INSERT INTO sites(id,domain,path,repository,branch,github_account_id,github_deploy_key_id,github_webhook_id,push_to_deploy,deploy_script) VALUES(?,?,?,?,'main',?,99,77,1,'keep original script')`, id, fmt.Sprintf("site%d.test", id), fmt.Sprintf("/home/fluxo/site%d.test", id), repo, account)
}
func (f *linkRecoveryFixture) links(t *testing.T) map[int]int {
	t.Helper()
	rows, e := f.db.Query("SELECT id,github_account_id FROM sites ORDER BY id")
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	m := map[int]int{}
	for rows.Next() {
		var s, a int
		if e = rows.Scan(&s, &a); e != nil {
			t.Fatal(e)
		}
		m[s] = a
	}
	return m
}

func TestInstallerGitHubLinksDoesNotInitializeUnrelatedState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	t.Setenv("FLUXO_ENV", "prod")
	if _, err := RestoreInstallerGitHubLinks(context.Background(), dir); err == nil {
		t.Fatal("uninstalled recovery scope accepted")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("recovery initialized an unrelated directory", err)
	}
}

func TestInstallerGitHubLinksHealthyAndIntentionalDisconnectAreSilent(t *testing.T) {
	f := newLinkRecoveryFixture(t)
	f.site(t, 1, 4, "company/one")
	f.site(t, 2, 0, "company/two")
	f.site(t, 3, 0, "company/three")
	f.exec(t, "UPDATE sites SET github_disconnected_user_id=104 WHERE id=2")
	f.exec(t, "UPDATE sites SET github_disconnected_username='historical-owner' WHERE id=3")
	r, e := restoreGitHubLinks(context.Background(), f.db, f.deps)
	var out bytes.Buffer
	PrintInstallerGitHubLinks(&out, r, e)
	if e != nil || r.Affected != 0 || out.Len() != 0 || f.keyReads != 0 || f.checks != 0 || len(f.backups) != 0 {
		t.Fatal(r, e, out.String(), f.keyReads)
	}
}

func TestInstallerGitHubLinksRestoresSixSitesAndPreservesCredentialsKeysAndSettings(t *testing.T) {
	f := newLinkRecoveryFixture(t)
	f.account(t, 4, "fixture-secret")
	for _, id := range []int{14, 16, 19, 21, 22, 23} {
		f.site(t, id, 0, fmt.Sprintf("company/repo%d", id))
	}
	f.site(t, 90, 8, "another/site")
	var oldToken string
	f.db.QueryRow("SELECT token FROM github_accounts WHERE id=4").Scan(&oldToken)
	r, e := restoreGitHubLinks(context.Background(), f.db, f.deps)
	if e != nil || len(r.Restored) != 6 || len(r.Skipped) != 0 || len(f.backups) != 1 || len(f.backups[0]) != 6 {
		t.Fatal(r, e, f.backups)
	}
	for id, account := range f.links(t) {
		if id == 90 {
			if account != 8 {
				t.Fatal("existing link replaced")
			}
		} else if account != 4 {
			t.Fatal(id, account)
		}
	}
	var untouched int
	f.db.QueryRow(`SELECT COUNT(*) FROM sites WHERE github_deploy_key_id=99 AND github_webhook_id=77 AND push_to_deploy=1 AND deploy_script='keep original script' AND branch='main' AND repository LIKE '%/%'`).Scan(&untouched)
	var token string
	f.db.QueryRow("SELECT token FROM github_accounts WHERE id=4").Scan(&token)
	if untouched != 7 || token != oldToken {
		t.Fatal("unrelated site data or credential changed")
	}
	var audits int
	f.db.QueryRow("SELECT COUNT(*) FROM activity WHERE type='github_links_restored'").Scan(&audits)
	if audits != 6 {
		t.Fatal(audits)
	}
	var output bytes.Buffer
	PrintInstallerGitHubLinks(&output, r, e)
	if !strings.Contains(output.String(), "6 site(s)") || strings.Contains(output.String(), "fixture-secret") || strings.Contains(output.String(), "company/") {
		t.Fatal(output.String())
	}
	checks := f.checks
	r, e = restoreGitHubLinks(context.Background(), f.db, f.deps)
	if e != nil || r.Affected != 0 || f.checks != checks || len(f.backups) != 1 {
		t.Fatal("retry replayed completed recovery", r, e)
	}
}

func TestInstallerGitHubLinksMultipleAccountsMatchByRepositoryAccess(t *testing.T) {
	f := newLinkRecoveryFixture(t)
	a := f.account(t, 4, "a")
	b := f.account(t, 8, "b")
	a.check = func(_ context.Context, r, _ string) error {
		if r == "company/one" {
			return nil
		}
		return &gitservice.APIError{Status: 404}
	}
	b.check = func(_ context.Context, r, _ string) error {
		if r == "company/two" {
			return nil
		}
		return &gitservice.APIError{Status: 404}
	}
	f.site(t, 1, 0, "company/one")
	f.site(t, 2, 0, "company/two")
	r, e := restoreGitHubLinks(context.Background(), f.db, f.deps)
	if e != nil || len(r.Restored) != 2 || !reflect.DeepEqual(f.links(t), map[int]int{1: 4, 2: 8}) {
		t.Fatal(r, e, f.links(t))
	}
}

func TestInstallerGitHubLinksAmbiguityAndUnavailableChecksDoNotGuess(t *testing.T) {
	for _, kind := range []string{"both-access", "forbidden", "timeout", "invalid-token", "wrong-owner", "missing-branch", "no-accounts", "malformed-settings", "many-accounts"} {
		t.Run(kind, func(t *testing.T) {
			f := newLinkRecoveryFixture(t)
			f.site(t, 1, 0, "company/one")
			if kind != "no-accounts" {
				a := f.account(t, 4, "a")
				switch kind {
				case "both-access":
					f.account(t, 8, "b")
				case "forbidden":
					a.check = func(context.Context, string, string) error { return &gitservice.APIError{Status: 403} }
				case "timeout":
					f.account(t, 8, "b").check = func(context.Context, string, string) error { return context.DeadlineExceeded }
				case "invalid-token":
					a.identityError = &gitservice.APIError{Status: 401}
				case "wrong-owner":
					a.identity.ID = 999
				case "missing-branch":
					a.check = func(context.Context, string, string) error { return &gitservice.APIError{Status: 404} }
				case "malformed-settings":
					f.exec(t, "UPDATE sites SET repository='https://other.invalid/repo' WHERE id=1")
				case "many-accounts":
					for i := 10; i < 18; i++ {
						f.account(t, i, fmt.Sprint(i))
					}
				}
			}
			r, e := restoreGitHubLinks(context.Background(), f.db, f.deps)
			if e != nil || len(r.Restored) != 0 || len(r.Skipped) != 1 || f.links(t)[1] != 0 || len(f.backups) != 0 {
				t.Fatal(r, e)
			}
		})
	}
}

func TestInstallerGitHubLinksConcurrentChangesArePreserved(t *testing.T) {
	for _, kind := range []string{"credential", "key", "new-account", "site-repository", "site-disconnected", "active-deployment", "deleting-site"} {
		t.Run(kind, func(t *testing.T) {
			f := newLinkRecoveryFixture(t)
			v := f.account(t, 4, "a")
			f.site(t, 1, 0, "company/one")
			v.check = func(context.Context, string, string) error {
				switch kind {
				case "key":
					f.key = bytes.Repeat([]byte{25}, 32)
				case "credential":
					f.exec(t, "UPDATE github_accounts SET token='changed' WHERE id=4")
				case "new-account":
					f.account(t, 8, "b")
				case "site-repository":
					f.exec(t, "UPDATE sites SET repository='other/repo' WHERE id=1")
				case "site-disconnected":
					f.exec(t, "UPDATE sites SET github_disconnected_user_id=104 WHERE id=1")
				case "active-deployment":
					f.exec(t, "INSERT INTO deployments(site_id,status) VALUES(1,'running')")
				case "deleting-site":
					f.exec(t, "UPDATE sites SET deletion_status='pending' WHERE id=1")
				}
				return nil
			}
			r, e := restoreGitHubLinks(context.Background(), f.db, f.deps)
			if f.links(t)[1] != 0 || len(r.Restored) != 0 || len(f.backups) != 0 {
				t.Fatal(r, e)
			}
			if (kind == "credential" || kind == "key" || kind == "new-account") && e == nil {
				t.Fatal("changed matching assumptions were accepted")
			}
		})
	}
}

func TestInstallerGitHubLinksBackupAndDatabaseFailuresRollBack(t *testing.T) {
	for _, kind := range []string{"backup", "second-update", "audit"} {
		t.Run(kind, func(t *testing.T) {
			f := newLinkRecoveryFixture(t)
			f.account(t, 4, "a")
			f.site(t, 1, 0, "company/one")
			f.site(t, 2, 0, "company/two")
			switch kind {
			case "backup":
				f.deps.backup = func([]githubLinkPlan) (string, error) { return "", errors.New("disk failure") }
			case "second-update":
				f.exec(t, `CREATE TRIGGER fixture_fail BEFORE UPDATE ON sites WHEN OLD.id=2 BEGIN SELECT RAISE(ABORT,'fixture'); END`)
			case "audit":
				f.exec(t, `CREATE TRIGGER fixture_fail BEFORE INSERT ON activity BEGIN SELECT RAISE(ABORT,'fixture'); END`)
			}
			r, e := restoreGitHubLinks(context.Background(), f.db, f.deps)
			if e == nil || len(r.Restored) != 0 || f.links(t)[1] != 0 || f.links(t)[2] != 0 {
				t.Fatal(r, e)
			}
		})
	}
}

func TestInstallerGitHubLinksRequestAndTimeBudgets(t *testing.T) {
	f := newLinkRecoveryFixture(t)
	f.account(t, 4, "a")
	for i := 1; i <= 140; i++ {
		f.site(t, i, 0, fmt.Sprintf("company/site%d", i))
	}
	r, e := restoreGitHubLinks(context.Background(), f.db, f.deps)
	if e != nil || f.checks != 63 || len(r.Restored) != 63 || len(r.Warnings) != 1 {
		t.Fatal(r, e, f.checks)
	}
	f = newLinkRecoveryFixture(t)
	v := f.account(t, 4, "a")
	f.site(t, 1, 0, "company/one")
	v.check = func(c context.Context, _, _ string) error { <-c.Done(); return c.Err() }
	c, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	r, e = restoreGitHubLinks(c, f.db, f.deps)
	if e != nil || len(r.Restored) != 0 || len(r.Skipped) != 1 || f.links(t)[1] != 0 {
		t.Fatal(r, e)
	}
}
