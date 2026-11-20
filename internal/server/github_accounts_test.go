package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/services/git"
)

type fakeAccountProvider struct {
	id            int64
	username      string
	expires       *time.Time
	identityErr   error
	registerErr   error
	registrations int
}

func (p *fakeAccountProvider) RemoveDeployKey(string, int64) error { return nil }
func (p *fakeAccountProvider) GetIdentity(context.Context) (git.Identity, error) {
	return git.Identity{ID: p.id, Username: p.username, ExpiresAt: p.expires}, p.identityErr
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
	p := &fakeAccountProvider{id: 101, username: "alice"}
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
	if _, err = database.DB.Exec(`INSERT INTO github_accounts(id,name,username,github_user_id,token) VALUES(1,'Work','alice',101,?); INSERT INTO sites(id,domain,path,repository,github_account_id) VALUES(1,'app.test','/home/fluxo/app.test','alice/app',1)`, stored); err != nil {
		t.Fatal(err)
	}
}
func TestUpdateGitHubTokenPreservesLinksEncryptsAndRepairs(t *testing.T) {
	s, p := accountTestServer(t)
	insertAccountSite(t)
	expiry := time.Now().Add(24 * time.Hour)
	p.username = "alice-renamed"
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
	if result.GitHubUserID != 101 || result.Username != "alice-renamed" || result.ID != 1 || result.LinkedSites != 1 || result.TokenExpiresAt == nil || result.DeploymentAccess.Sites[0].Status != "verified" || p.registrations != 1 {
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
				p.id = 102 // the original login was reassigned
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
	database.DB.Exec(`INSERT INTO sites(id,domain,path,repository,github_account_id,github_disconnected_username) VALUES(2,'other.test','/home/fluxo/other.test','alice/other',0,'alice')`)
	w := accountRequest(s.handleDeleteGitHubAccount(), "DELETE", "/api/v1/github/accounts/1", "")
	if w.Code != 204 {
		t.Fatal(w.Body.String())
	}
	p.id = 102
	wrong := accountRequest(s.handleConnectGitHubAccount(), "POST", "/api/v1/github/accounts", `{"token":"recycled-login"}`)
	if wrong.Code != 201 {
		t.Fatal(wrong.Body.String())
	}
	var impostor githubAccountResponse
	if err := json.Unmarshal(wrong.Body.Bytes(), &impostor); err != nil || impostor.LinkedSites != 0 {
		t.Fatal("recycled login restored links", err)
	}
	p.id, p.username = 101, "renamed"
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
	var keyID int64
	if err := database.DB.QueryRow("SELECT github_deploy_key_id FROM sites WHERE id=1").Scan(&keyID); err != nil || p.registrations != 1 || keyID != 99 {
		t.Fatal("reconnect did not reconcile the existing key registration", keyID, err)
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
	if len(a.DeploymentAccess.Sites) != 2 || a.DeploymentAccess.Sites[0].Status != "verified" || a.DeploymentAccess.Sites[1].Status != "error" || a.DeploymentAccess.Sites[1].Reason != "check_timeout" {
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
	id, unlock, err := s.insertProvisioningSite(context.Background(), req, "")
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 || unlock == nil {
		t.Fatal("existing account should permit insertion")
	}
	unlock()
	w := accountRequest(s.handleDeleteGitHubAccount(), "DELETE", "/api/v1/github/accounts/1", "")
	if w.Code != 204 {
		t.Fatal(w.Body.String())
	}
	req.Domain = "after.test"
	id, unlock, err = s.insertProvisioningSite(context.Background(), req, "")
	if err != nil {
		t.Fatal(err)
	}
	if id != 0 || unlock != nil {
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

func TestLegacyGitHubOwnershipRequiresSavedCredential(t *testing.T) {
	s, p := accountTestServer(t)
	insertAccountSite(t)
	if _, err := database.DB.Exec("UPDATE github_accounts SET username='', github_user_id=0 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	saved := &fakeAccountProvider{id: 101, username: "renamed", identityErr: &git.APIError{Status: 401}}
	s.githubAccountProvider = func(token string) githubAccountProvider {
		if token == "old-token" {
			return saved
		}
		return p
	}
	for _, request := range []struct {
		method  string
		handler http.HandlerFunc
		body    string
	}{
		{"PUT", s.handleUpdateGitHubAccount(), `{"token":"replacement"}`},
		{"DELETE", s.handleDeleteGitHubAccount(), ""},
	} {
		w := accountRequest(request.handler, request.method, "/api/v1/github/accounts/1", request.body)
		if w.Code != 409 || strings.Contains(w.Body.String(), "old-token") {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		a, err := readGitHubAccount(context.Background(), 1)
		if err != nil || a.LinkedSites != 1 || a.GitHubUserID != 0 || config.Decrypt(a.Token) != "old-token" {
			t.Fatal("lost unverified legacy connection", err)
		}
	}
	saved.identityErr = nil
	p.id = 202
	w := accountRequest(s.handleUpdateGitHubAccount(), "PUT", "/api/v1/github/accounts/1", `{"token":"replacement"}`)
	if w.Code != 409 {
		t.Fatalf("wrong replacement: %d", w.Code)
	}
	p.id, p.username = 101, "new-name"
	w = accountRequest(s.handleUpdateGitHubAccount(), "PUT", "/api/v1/github/accounts/1", `{"token":"replacement"}`)
	a, err := readGitHubAccount(context.Background(), 1)
	if w.Code != 200 || err != nil || a.GitHubUserID != 101 || a.Username != "new-name" || a.LinkedSites != 1 {
		t.Fatal("saved credential proof failed", w.Body.String(), err)
	}
}

func TestDisconnectedGitHubSitesDoNotBorrowWebhookCredentials(t *testing.T) {
	s, _ := accountTestServer(t)
	_, err := database.DB.Exec(`INSERT INTO users(username,token_hash,webhook_secret) VALUES('admin','hash','fixture');
	INSERT INTO github_accounts(id,name,token) VALUES(1,'First','first-token'),(2,'Second','second-token');
	INSERT INTO sites(id,domain,path,repository,push_to_deploy,github_account_id) VALUES
	(1,'lost.test','/lost','acme/app',1,0),(2,'bound.test','/bound','acme/app',1,2),(3,'orphan.test','/other','lost/repo',1,0)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SetPanelDomainConfig(database.PanelDomainConfig{Domain: "panel.example.com"}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeGitHubWebhookProvider{deliveryOK: true}
	s.githubWebhookProvider = func(token string) githubWebhookProvider {
		if token != "second-token" {
			t.Fatalf("unexpected credential selected")
		}
		return fake
	}
	s.reconcileGitHubWebhooks(context.Background())
	if fake.listCalls != 1 {
		t.Fatalf("expected only bound repository to be reconciled; calls=%d", fake.listCalls)
	}
	if s.verifyAndObserveGitHubWebhook("lost/repo", "44", "11111111-2222-3333-4444-555555555555") {
		t.Fatal("verified delivery for disconnected site")
	}
	if !s.verifyAndObserveGitHubWebhook("acme/app", "45", "11111111-2222-3333-4444-555555555555") {
		t.Fatal("bound site lost delivery verification")
	}
	var hook int
	if err := database.DB.QueryRow("SELECT github_webhook_id FROM sites WHERE id=1").Scan(&hook); err != nil || hook != 0 {
		t.Fatal("assigned another account's webhook to disconnected site", err)
	}
	if pat := githubTokenForAccount(0); pat != "" {
		t.Fatal("key operation borrowed first account")
	}
}

// The replacement request signals when it is waiting for the repair's site lock.
type waitingSiteContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *waitingSiteContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

type interleavedKeyProvider struct {
	fakeAccountProvider
	repairStarted chan struct{}
	resumeRepair  chan struct{}
	removed       chan int64
	keys          map[string]string
}

func (p *interleavedKeyProvider) EnsureDeployKey(_ context.Context, repo, key string) (int64, error) {
	p.keys[repo] = key
	if repo == "alice/app" {
		close(p.repairStarted)
		<-p.resumeRepair
		return 99, nil
	}
	return 123, nil
}
func (p *interleavedKeyProvider) RemoveDeployKey(_ string, id int64) error {
	p.removed <- id
	return nil
}

func TestGitHubRepairAndRepositoryReplacementPublishMatchingKey(t *testing.T) {
	s, _ := accountTestServer(t)
	insertAccountSite(t)
	t.Setenv("FLUXO_ENV", "")
	t.Setenv("HOME", t.TempDir())
	if _, err := database.DB.Exec("UPDATE sites SET deployment_strategy='zero-downtime',branch='main' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := git.GenerateSSHKey(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	provider := &interleavedKeyProvider{fakeAccountProvider: fakeAccountProvider{id: 101, username: "alice"}, repairStarted: make(chan struct{}), resumeRepair: make(chan struct{}), removed: make(chan int64, 1), keys: map[string]string{}}
	s.githubAccountProvider = func(string) githubAccountProvider { return provider }
	s.githubPublicKey = git.DeployPublicKey
	s.githubAccessCheck = func(context.Context, int, string) error {
		if _, ok := provider.keys["alice/app"]; ok {
			return nil
		}
		return errors.New("registration missing")
	}
	rotation := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rotation <- accountRequest(s.handleUpdateGitHubAccount(), "PUT", "/api/v1/github/accounts/1", `{"token":"new-token"}`)
	}()
	select {
	case <-provider.repairStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("repair did not reach registration")
	}
	replacement := make(chan *httptest.ResponseRecorder, 1)
	ctx := &waitingSiteContext{Context: context.Background(), waiting: make(chan struct{})}
	go func() {
		r := httptest.NewRequest("PUT", "/api/v1/sites/1", strings.NewReader(`{"repository":"alice/next"}`)).WithContext(ctx)
		r.SetPathValue("id", "1")
		w := httptest.NewRecorder()
		s.handleUpdateSite()(w, r)
		replacement <- w
	}()
	select {
	case <-ctx.waiting:
	case w := <-replacement:
		close(provider.resumeRepair)
		<-rotation
		t.Fatalf("replacement passed an active repair: %d %s", w.Code, w.Body.String())
	case <-time.After(5 * time.Second):
		close(provider.resumeRepair)
		t.Fatal("replacement failed to reach site gate")
	}
	close(provider.resumeRepair)
	for _, w := range []*httptest.ResponseRecorder{<-rotation, <-replacement} {
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	select {
	case id := <-provider.removed:
		if id != 99 {
			t.Fatalf("removed registration %d", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("repaired old registration not revoked")
	}
	var repo string
	var registration int64
	if err := database.DB.QueryRow("SELECT repository,github_deploy_key_id FROM sites WHERE id=1").Scan(&repo, &registration); err != nil {
		t.Fatal(err)
	}
	public, err := git.DeployPublicKey(1)
	if err != nil || repo != "alice/next" || registration != 123 || !strings.HasPrefix(provider.keys[repo], strings.TrimSpace(public)) || provider.keys["alice/app"] == public {
		t.Fatal("repository, private key and registration disagree", repo, registration, err)
	}
}

func TestGitHubRepairRetriesRegistrationTrackingAfterRemoteSuccess(t *testing.T) {
	s, provider := accountTestServer(t)
	insertAccountSite(t)
	s.githubAccessCheck = func(context.Context, int, string) error {
		if provider.registrations == 0 {
			return errors.New("remote registration missing")
		}
		return nil
	}
	if _, err := database.DB.Exec(`CREATE TRIGGER refuse_key_tracking BEFORE UPDATE OF github_deploy_key_id ON sites BEGIN SELECT RAISE(ABORT,'fixture write failure'); END`); err != nil {
		t.Fatal(err)
	}
	site := githubSiteAccess{SiteID: 1, Repository: "alice/app"}
	if err := s.verifyGitHubSite(context.Background(), 1, site, provider, true); err == nil {
		t.Fatal("remote success hid failed local tracking")
	}
	if _, err := database.DB.Exec("DROP TRIGGER refuse_key_tracking"); err != nil {
		t.Fatal(err)
	}
	if err := s.verifyGitHubSite(context.Background(), 1, site, provider, true); err != nil {
		t.Fatal(err)
	}
	var registration int64
	if err := database.DB.QueryRow("SELECT github_deploy_key_id FROM sites WHERE id=1").Scan(&registration); err != nil || registration != 99 || provider.registrations != 2 {
		t.Fatal("working SSH skipped recovery of the remote registration ID", registration, provider.registrations, err)
	}
}

func TestGitHubAccessReasonsRequireObservedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, reason             string
		check, key, registration error
		repair                   bool
	}{
		{name: "generic failure is not a diagnosis", check: errors.New("registration missing or network fault"), reason: "check_failed"},
		{name: "observed SSH failure", check: git.ErrDeploySSHAccess, reason: "ssh_access_failed"},
		{name: "observed key unavailable", check: git.ErrDeployKeyUnavailable, reason: "private_key_unavailable"},
		{name: "repair cannot restore private key", key: git.ErrDeployKeyUnavailable, repair: true, reason: "private_key_unavailable"},
		{name: "registration actually attempted", registration: errors.New("GitHub denied request"), repair: true, reason: "registration_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, provider := accountTestServer(t)
			insertAccountSite(t)
			s.githubAccessCheck = func(context.Context, int, string) error { return tc.check }
			s.githubPublicKey = func(int) (string, error) { return "same-existing-public-key", tc.key }
			provider.registerErr = tc.registration
			if err := s.checkGitHubSites(context.Background(), 1, "token", tc.repair); err != nil {
				t.Fatal(err)
			}
			a, err := readGitHubAccount(context.Background(), 1)
			if err != nil {
				t.Fatal(err)
			}
			result := a.DeploymentAccess.Sites[0]
			if result.Reason != tc.reason || result.Status != "error" {
				t.Fatalf("unexpected result: %+v", result)
			}
			if (!tc.repair || tc.key != nil) && provider.registrations != 0 {
				t.Fatal("observational/key-unavailable path registered a key")
			}
		})
	}
}

func TestRepositorySwitchRevokesBeforeWaitingDisconnect(t *testing.T) {
	s, _ := accountTestServer(t)
	insertAccountSite(t)
	t.Setenv("FLUXO_ENV", "")
	t.Setenv("HOME", t.TempDir())
	if _, _, err := git.GenerateSSHKey(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	provider := &interleavedKeyProvider{removed: make(chan int64, 1), keys: map[string]string{}}
	s.githubAccountProvider = func(string) githubAccountProvider { return provider }
	// This is the membership reservation held by the real repository handler.
	s.githubMembershipMu.RLock()
	reserved := true
	defer func() {
		if reserved {
			s.githubMembershipMu.RUnlock()
		}
	}()
	disconnected := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		disconnected <- accountRequest(s.handleDeleteGitHubAccount(), "DELETE", "/api/v1/github/accounts/1", "")
	}()
	deadline := time.Now().Add(5 * time.Second)
	for s.githubMembershipMu.TryRLock() {
		s.githubMembershipMu.RUnlock()
		if time.Now().After(deadline) {
			t.Fatal("disconnect never queued for its write reservation")
		}
		time.Sleep(time.Millisecond)
	}
	if err := s.ensureSiteDeployKeyAccess(context.Background(), 1, "alice/app", "alice/next", 1, 77); err != nil {
		t.Fatal(err)
	}
	select {
	case key := <-provider.removed:
		if key != 77 {
			t.Fatal("revoked the wrong old registration", key)
		}
	default:
		t.Fatal("old registration cleanup escaped the credential reservation")
	}
	s.githubMembershipMu.RUnlock()
	reserved = false
	if response := <-disconnected; response.Code != 204 {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestGitHubCanceledCheckIsNotReportedAsTimeout(t *testing.T) {
	s, _ := accountTestServer(t)
	insertAccountSite(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.checkGitHubSites(ctx, 1, "token", false); err != nil {
		t.Fatal(err)
	}
	account, err := readGitHubAccount(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	site := account.DeploymentAccess.Sites[0]
	if site.Status != "error" || site.Reason != "check_canceled" || !strings.Contains(site.Message, "canceled") {
		t.Fatalf("cancellation misreported: %+v", site)
	}
}
