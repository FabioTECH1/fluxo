package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/services/git"
)

type fakeAccountProvider struct {
	username      string
	expires       *time.Time
	identityErr   error
	registerErr   error
	registrations int
}

func (p *fakeAccountProvider) GetIdentity(context.Context) (git.Identity, error) {
	return git.Identity{Username: p.username, ExpiresAt: p.expires}, p.identityErr
}
func (p *fakeAccountProvider) EnsureDeployKey(context.Context, string, string) (int64, error) {
	p.registrations++
	return 99, p.registerErr
}
func accountTestServer(t *testing.T) (*Server, *fakeAccountProvider) {
	t.Helper()
	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "test.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.DB.Close() })
	if err := config.InitEncryption(dir); err != nil {
		t.Fatal(err)
	}
	p := &fakeAccountProvider{username: "alice"}
	s := &Server{githubAccountProvider: func(string) githubAccountProvider { return p }, githubAccessCheck: func(context.Context, int, string) error { return nil }, githubPublicKey: func(int) (string, error) { return "public", nil }}
	return s, p
}
func accountRequest(handler http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 5 {
		r.SetPathValue("id", parts[4])
	}
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}
func insertAccountSite(t *testing.T) {
	t.Helper()
	stored, err := config.EncryptSecret("old-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.DB.Exec(`INSERT INTO github_accounts(id,name,username,token) VALUES(1,'Work','alice',?); INSERT INTO sites(id,domain,path,repository,github_account_id) VALUES(1,'app.test','/home/fluxo/app.test','alice/app',1)`, stored); err != nil {
		t.Fatal(err)
	}
}
func TestUpdateGitHubTokenPreservesLinksEncryptsAndRepairs(t *testing.T) {
	s, p := accountTestServer(t)
	insertAccountSite(t)
	expiry := time.Now().Add(24 * time.Hour)
	p.expires = &expiry
	if _, err := database.DB.Exec(`INSERT INTO github_cache(key,data) VALUES('repos:account:1','old'),('repos:account:10','keep'),('branches:alice/app:account:1','old')`); err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.githubAccessCheck = func(context.Context, int, string) error {
		calls++
		if calls == 1 {
			return errors.New("missing registration")
		}
		return nil
	}
	w := accountRequest(s.handleUpdateGitHubAccount(), "PUT", "/api/v1/github/accounts/1", `{"token":"new-token"}`)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var result githubAccountResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ID != 1 || result.LinkedSites != 1 || result.TokenExpiresAt == nil || result.DeploymentAccess.Sites[0].Status != "verified" || p.registrations != 1 {
		t.Fatalf("result=%+v provider=%+v", result, p)
	}
	var stored string
	var accountID int
	var keyID int64
	database.DB.QueryRow("SELECT token FROM github_accounts WHERE id=1").Scan(&stored)
	database.DB.QueryRow("SELECT github_account_id,github_deploy_key_id FROM sites WHERE id=1").Scan(&accountID, &keyID)
	if !strings.HasPrefix(stored, "enc:") || config.Decrypt(stored) != "new-token" || accountID != 1 || keyID != 99 {
		t.Fatal("credential or site state not saved")
	}
	if strings.Contains(w.Body.String(), "new-token") || strings.Contains(w.Body.String(), "enc:") {
		t.Fatal("token leaked")
	}
	var cacheCount int
	database.DB.QueryRow("SELECT count(*) FROM github_cache").Scan(&cacheCount)
	if cacheCount != 1 {
		t.Fatal("cache invalidation was too broad or incomplete")
	}
}
func TestUpdateGitHubTokenRejectsDifferentAccountAndInvalidToken(t *testing.T) {
	for _, which := range []string{"other", "invalid"} {
		t.Run(which, func(t *testing.T) {
			s, p := accountTestServer(t)
			insertAccountSite(t)
			code := 409
			if which == "other" {
				p.username = "bob"
			} else {
				p.identityErr = &git.APIError{Status: 401}
				code = 400
			}
			w := accountRequest(s.handleUpdateGitHubAccount(), "PUT", "/api/v1/github/accounts/1", `{"token":"new-token"}`)
			if w.Code != code {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			var stored string
			database.DB.QueryRow("SELECT token FROM github_accounts WHERE id=1").Scan(&stored)
			if config.Decrypt(stored) != "old-token" {
				t.Fatal("failed validation replaced token")
			}
		})
	}
}
func TestCheckGitHubAccessIsReadOnlyAndInvalidatesStaleResults(t *testing.T) {
	s, p := accountTestServer(t)
	insertAccountSite(t)
	w := accountRequest(s.handleCheckGitHubAccount(), "POST", "/api/v1/github/accounts/1/check", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	a, err := readGitHubAccount(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if a.DeploymentAccess.Sites[0].Status != "verified" || p.registrations != 0 {
		t.Fatal("check mutated registrations or failed")
	}
	database.DB.Exec("UPDATE sites SET repository='alice/other' WHERE id=1")
	a, err = readGitHubAccount(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if a.DeploymentAccess.CheckedAt != nil || a.DeploymentAccess.Sites[0].Status != "unchecked" {
		t.Fatal("stale success survived repository change")
	}
	p.identityErr = &git.APIError{Status: 403}
	w = accountRequest(s.handleCheckGitHubAccount(), "POST", "/api/v1/github/accounts/1/check", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	a, _ = readGitHubAccount(context.Background(), 1)
	if a.TokenStatus != "error" {
		t.Fatal("403 was misreported as expired")
	}
	p.identityErr = &git.APIError{Status: 401}
	w = accountRequest(s.handleCheckGitHubAccount(), "POST", "/api/v1/github/accounts/1/check", "")
	a, _ = readGitHubAccount(context.Background(), 1)
	if a.TokenStatus != "invalid" || a.DeploymentAccess.CheckedAt == nil || a.DeploymentAccess.Sites[0].Status != "verified" {
		t.Fatal("token rejection hid independently verified SSH access")
	}
}
func TestReconnectRestoresOnlyExplicitlyDisconnectedIdentity(t *testing.T) {
	s, p := accountTestServer(t)
	insertAccountSite(t)
	database.DB.Exec(`INSERT INTO sites(id,domain,path,repository,github_account_id) VALUES(2,'other.test','/home/fluxo/other.test','alice/other',0)`)
	w := accountRequest(s.handleDeleteGitHubAccount(), "DELETE", "/api/v1/github/accounts/1", "")
	if w.Code != 204 {
		t.Fatal(w.Body.String())
	}
	w = accountRequest(s.handleConnectGitHubAccount(), "POST", "/api/v1/github/accounts", `{"token":"replacement"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var a githubAccountResponse
	json.Unmarshal(w.Body.Bytes(), &a)
	var linked, unrelated int
	database.DB.QueryRow("SELECT github_account_id FROM sites WHERE id=1").Scan(&linked)
	database.DB.QueryRow("SELECT github_account_id FROM sites WHERE id=2").Scan(&unrelated)
	if linked != a.ID || unrelated != 0 {
		t.Fatal("reconnect lost sites or claimed unrelated sites")
	}
	if p.registrations != 0 {
		t.Fatal("working key was unnecessarily registered")
	}
	w = accountRequest(s.handleConnectGitHubAccount(), "POST", "/api/v1/github/accounts", `{"token":"another"}`)
	if w.Code != 409 {
		t.Fatal("duplicate GitHub identity accepted")
	}
}
func TestTokenSavedWhenDeployKeyRepairFails(t *testing.T) {
	s, p := accountTestServer(t)
	insertAccountSite(t)
	p.registerErr = &git.APIError{Status: 403}
	s.githubAccessCheck = func(context.Context, int, string) error { return errors.New("SSH rejected") }
	w := accountRequest(s.handleUpdateGitHubAccount(), "PUT", "/api/v1/github/accounts/1", `{"token":"replacement"}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	a, _ := readGitHubAccount(context.Background(), 1)
	if a.TokenStatus != "valid" || a.DeploymentAccess.Sites[0].Status != "error" || !strings.Contains(a.DeploymentAccess.Sites[0].Message, "permissions") {
		t.Fatal("partial failure hidden")
	}
}
func TestDisconnectGitHubAccountRollsBackOnFailure(t *testing.T) {
	s, _ := accountTestServer(t)
	insertAccountSite(t)
	database.DB.Exec(`CREATE TRIGGER fail_disconnect BEFORE DELETE ON github_accounts BEGIN SELECT RAISE(ABORT,'fixture'); END`)
	w := accountRequest(s.handleDeleteGitHubAccount(), "DELETE", "/api/v1/github/accounts/1", "")
	if w.Code != 500 {
		t.Fatal(w.Code)
	}
	var id int
	database.DB.QueryRow("SELECT github_account_id FROM sites WHERE id=1").Scan(&id)
	if id != 1 {
		t.Fatal("failed disconnect orphaned sites")
	}
}

func TestGitHubBatchTimeoutStillReturnsPartialReport(t *testing.T) {
	s, _ := accountTestServer(t)
	insertAccountSite(t)
	if _, err := database.DB.Exec(`INSERT INTO sites(id,domain,path,repository,github_account_id) VALUES(2,'two.test','/home/fluxo/two.test','alice/two',1)`); err != nil {
		t.Fatal(err)
	}
	s.githubOperationTimeout = 20 * time.Millisecond
	s.githubAccessCheck = func(ctx context.Context, id int, _ string) error {
		if id == 1 {
			return nil
		}
		<-ctx.Done()
		return ctx.Err()
	}
	w := accountRequest(s.handleUpdateGitHubAccount(), "PUT", "/api/v1/github/accounts/1", `{"token":"replacement"}`)
	if w.Code != 200 {
		t.Fatalf("committed token reported as failure: %d %s", w.Code, w.Body.String())
	}
	var a githubAccountResponse
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if len(a.DeploymentAccess.Sites) != 2 || a.DeploymentAccess.Sites[0].Status != "verified" || a.DeploymentAccess.Sites[1].Status != "error" {
		t.Fatalf("partial report hidden: %+v", a.DeploymentAccess)
	}
}

func TestGitHubCacheCannotBeRepopulatedByOldCredential(t *testing.T) {
	s, _ := accountTestServer(t)
	insertAccountSite(t)
	var oldToken string
	if err := database.DB.QueryRow("SELECT token FROM github_accounts WHERE id=1").Scan(&oldToken); err != nil {
		t.Fatal(err)
	}
	resume := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		<-resume
		done <- database.SetCachedGitHubDataForAccount("repos:account:1", `[{"full_name":"alice/private-old"}]`, 1, oldToken)
	}()
	w := accountRequest(s.handleUpdateGitHubAccount(), "PUT", "/api/v1/github/accounts/1", `{"token":"narrower-token"}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	close(resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, exists := database.GetCachedGitHubData("repos:account:1"); exists {
		t.Fatal("old-token refresh repopulated cache after rotation")
	}
	var current string
	database.DB.QueryRow("SELECT token FROM github_accounts WHERE id=1").Scan(&current)
	if err := database.SetCachedGitHubDataForAccount("repos:account:1", `[]`, 1, current); err != nil {
		t.Fatal(err)
	}
	if data, exists := database.GetCachedGitHubData("repos:account:1"); !exists || data != "[]" {
		t.Fatal("current-token cache write failed")
	}
	w = accountRequest(s.handleDeleteGitHubAccount(), "DELETE", "/api/v1/github/accounts/1", "")
	if w.Code != 204 {
		t.Fatal(w.Body.String())
	}
	if err := database.SetCachedGitHubDataForAccount("repos:account:1", `["stale"]`, 1, current); err != nil {
		t.Fatal(err)
	}
	if _, exists := database.GetCachedGitHubData("repos:account:1"); exists {
		t.Fatal("disconnected account cache was restored")
	}
}

func TestSiteInsertCannotOutliveDisconnectedGitHubAccount(t *testing.T) {
	s, _ := accountTestServer(t)
	insertAccountSite(t)
	req := CreateSiteRequest{Domain: "before.test", Repository: "alice/before", GitHubAccountID: 1, AppType: "php", PHPVersion: "8.4", DeploymentStrategy: "standard", WWWRedirect: "none"}
	result, err := insertSiteRecord(context.Background(), req, "")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		t.Fatal("existing account should permit insertion")
	}
	w := accountRequest(s.handleDeleteGitHubAccount(), "DELETE", "/api/v1/github/accounts/1", "")
	if w.Code != 204 {
		t.Fatal(w.Body.String())
	}
	req.Domain = "after.test"
	result, err = insertSiteRecord(context.Background(), req, "")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 0 {
		t.Fatal("site inserted with dangling account ID after disconnect")
	}
	w = accountRequest(s.handleConnectGitHubAccount(), "POST", "/api/v1/github/accounts", `{"token":"replacement"}`)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	var a githubAccountResponse
	json.Unmarshal(w.Body.Bytes(), &a)
	if a.LinkedSites != 2 {
		t.Fatal("site inserted before disconnect was not recovered")
	}
	var nonexistent int
	database.DB.QueryRow("SELECT count(*) FROM sites WHERE domain='after.test'").Scan(&nonexistent)
	if nonexistent != 0 {
		t.Fatal("dangling site was persisted")
	}
}
