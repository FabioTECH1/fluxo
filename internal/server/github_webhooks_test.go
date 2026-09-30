package server

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"fluxo/internal/config"
	"fluxo/internal/database"
	gitservice "fluxo/internal/services/git"
)

func testWebhook(id int64, callback string, responseCode int) gitservice.Webhook {
	var hook gitservice.Webhook
	hook.ID = id
	hook.Active = true
	hook.Events = []string{"push"}
	hook.Config.URL = callback
	hook.LastResponse.Code = responseCode
	return hook
}

func TestPlanWebhookReconciliationKeepsPublicIPAndRemovesKnownDuplicates(t *testing.T) {
	hooks := []gitservice.Webhook{
		testWebhook(11, "http://10.136.4.167:9595/api/v1/github/webhook", 0),
		testWebhook(22, "https://163.245.222.6:9595/api/v1/github/webhook", 200),
		testWebhook(33, "https://panel.example.com/api/v1/github/webhook", 200),
		testWebhook(44, "https://other-server.example.com/api/v1/github/webhook", 200),
		testWebhook(55, "https://hooks.example.com/unrelated", 200),
	}
	plan := planWebhookReconciliation(hooks, 33, "", "panel.example.com", "", map[int64]bool{22: true}, map[string]bool{"10.136.4.167:9595": true})
	if plan.Keep == nil || plan.Keep.ID != 22 {
		t.Fatalf("kept hook = %+v, want public-IP hook 22", plan.Keep)
	}
	if !reflect.DeepEqual(plan.DeleteIDs, []int64{11, 33}) {
		t.Fatalf("deleted hooks = %v, want [11 33]", plan.DeleteIDs)
	}
}

func TestPlanWebhookReconciliationDoesNotClaimUnrelatedPublicHook(t *testing.T) {
	hooks := []gitservice.Webhook{
		testWebhook(70, "https://other-server.example.com/api/v1/github/webhook", 200),
	}
	preferred := "https://panel.example.com/api/v1/github/webhook"
	plan := planWebhookReconciliation(hooks, 0, "", "panel.example.com", preferred, nil, nil)
	if plan.Keep != nil || plan.CreateURL != preferred || len(plan.DeleteIDs) != 0 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestPlanWebhookReconciliationReplacesFailedStoredDomainWithObservedPublicIP(t *testing.T) {
	hooks := []gitservice.Webhook{
		testWebhook(22, "https://163.245.222.6:9595/api/v1/github/webhook", 200),
		testWebhook(33, "https://old-panel.example.com/api/v1/github/webhook", 0),
	}
	plan := planWebhookReconciliation(hooks, 33, "", "", "", map[int64]bool{22: true}, nil)
	if plan.Keep == nil || plan.Keep.ID != 22 || !reflect.DeepEqual(plan.DeleteIDs, []int64{33}) {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestPlanWebhookReconciliationDoesNotInferPublicIPOwnership(t *testing.T) {
	hooks := []gitservice.Webhook{
		testWebhook(22, "https://203.0.113.10:9595/api/v1/github/webhook", 200),
		testWebhook(33, "https://panel.example.com/api/v1/github/webhook", 200),
	}
	plan := planWebhookReconciliation(hooks, 33, "", "panel.example.com", "", nil, nil)
	if plan.Keep == nil || plan.Keep.ID != 33 || len(plan.DeleteIDs) != 0 {
		t.Fatalf("unverified public hook was claimed: %+v", plan)
	}
}

func TestPlanWebhookReconciliationDoesNotClaimAnotherLocalPort(t *testing.T) {
	hooks := []gitservice.Webhook{
		testWebhook(11, "https://10.136.4.167:9696/api/v1/github/webhook", 200),
		testWebhook(33, "https://panel.example.com/api/v1/github/webhook", 200),
	}
	plan := planWebhookReconciliation(hooks, 33, "", "panel.example.com", "", nil, map[string]bool{"10.136.4.167:9595": true})
	if plan.Keep == nil || plan.Keep.ID != 33 || len(plan.DeleteIDs) != 0 {
		t.Fatalf("different local port was claimed: %+v", plan)
	}
}

func TestWebhookURLForHostRejectsPrivateAndInvalidHosts(t *testing.T) {
	if got := webhookURLForHost("10.0.0.8:9595"); got != "" {
		t.Fatalf("private callback = %q", got)
	}
	if got := webhookURLForHost("203.0.113.10:9595"); got != "https://203.0.113.10:9595/api/v1/github/webhook" {
		t.Fatalf("public callback = %q", got)
	}
	if got := webhookURLForHost("panel.example.com"); got != "https://panel.example.com/api/v1/github/webhook" {
		t.Fatalf("domain callback = %q", got)
	}
}

func TestSiteWebhookTransitionSkipsOrdinarySettingsSave(t *testing.T) {
	tests := []struct {
		name                         string
		current, desired, repoChange bool
		remove, ensure               bool
	}{
		{name: "unchanged enabled", current: true, desired: true},
		{name: "unchanged disabled"},
		{name: "enable", desired: true, ensure: true},
		{name: "disable", current: true, remove: true},
		{name: "move enabled repository", current: true, desired: true, repoChange: true, remove: true, ensure: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			remove, ensure := siteWebhookTransition(test.current, test.desired, test.repoChange)
			if remove != test.remove || ensure != test.ensure {
				t.Fatalf("transition = (%v, %v), want (%v, %v)", remove, ensure, test.remove, test.ensure)
			}
		})
	}
}

type fakeGitHubWebhookProvider struct {
	hooks      []gitservice.Webhook
	deliveryOK bool
	onRegister func()
	updatedID  int64
	removedIDs []int64
	listCalls  int
}

func (p *fakeGitHubWebhookProvider) HasWebhookDelivery(string, int64, string) (bool, error) {
	return p.deliveryOK, nil
}

func (p *fakeGitHubWebhookProvider) ListWebhooks(string) ([]gitservice.Webhook, error) {
	p.listCalls++
	return append([]gitservice.Webhook(nil), p.hooks...), nil
}

func (p *fakeGitHubWebhookProvider) RegisterWebhook(string, string, string) (int64, error) {
	if p.onRegister != nil {
		p.onRegister()
	}
	return 99, nil
}

func (p *fakeGitHubWebhookProvider) UpdateWebhook(_ string, id int64, _ string, _ string) error {
	p.updatedID = id
	return nil
}

func (p *fakeGitHubWebhookProvider) RemoveWebhook(_ string, id int64) error {
	p.removedIDs = append(p.removedIDs, id)
	return nil
}

func TestEnsureSiteGitHubWebhookCleansUpgradeDuplicates(t *testing.T) {
	dataDir := t.TempDir()
	if err := database.InitDB(filepath.Join(dataDir, "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })
	if err := config.InitEncryption(dataDir); err != nil {
		t.Fatal(err)
	}
	secret, err := config.EncryptSecret("webhook-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("INSERT INTO users (username, token_hash, webhook_secret) VALUES ('admin', 'hash', ?)", secret); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("INSERT INTO github_accounts (id, name, token) VALUES (1, 'GitHub', 'token')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`INSERT INTO sites
		(domain, path, repository, branch, push_to_deploy, github_account_id, github_webhook_id)
		VALUES ('app.example.com', '/home/fluxo/app.example.com', 'acme/app', 'main', 1, 1, 33)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`INSERT INTO github_webhook_observations (repository, hook_id) VALUES ('acme/app', 11), ('acme/app', 22)`); err != nil {
		t.Fatal(err)
	}
	if err := database.SetPanelDomainConfig(database.PanelDomainConfig{Domain: "panel.example.com"}); err != nil {
		t.Fatal(err)
	}

	fake := &fakeGitHubWebhookProvider{hooks: []gitservice.Webhook{
		testWebhook(11, "http://10.136.4.167:9595/api/v1/github/webhook", 0),
		testWebhook(22, "https://163.245.222.6:9595/api/v1/github/webhook", 200),
		testWebhook(33, "https://panel.example.com/api/v1/github/webhook", 200),
	}}
	server := NewServer(nil, dataDir, false)
	server.githubWebhookProvider = func(string) githubWebhookProvider { return fake }
	if err := server.ensureSiteGitHubWebhook(context.Background(), 1, ""); err != nil {
		t.Fatal(err)
	}
	if fake.updatedID != 22 || !reflect.DeepEqual(fake.removedIDs, []int64{11, 33}) {
		t.Fatalf("updated=%d removed=%v", fake.updatedID, fake.removedIDs)
	}
	var storedID int64
	var storedURL string
	if err := database.DB.QueryRow("SELECT github_webhook_id, github_webhook_url FROM sites WHERE id = 1").Scan(&storedID, &storedURL); err != nil {
		t.Fatal(err)
	}
	if storedID != 22 || storedURL != "https://163.245.222.6:9595/api/v1/github/webhook" {
		t.Fatalf("stored webhook = (%d, %q)", storedID, storedURL)
	}
}

func TestInsertWebhookDeploymentSuppressesImmediateDuplicateCommit(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })
	if _, err := database.DB.Exec("INSERT INTO sites (id, domain, path) VALUES (1, 'app.example.com', '/home/fluxo/app.example.com')"); err != nil {
		t.Fatal(err)
	}
	inserted, err := insertWebhookDeployment(1, "main", "abc123", "Ship checkout", "Ada")
	if err != nil || !inserted {
		t.Fatalf("first insert = %v, %v", inserted, err)
	}
	var hash, message, author, source string
	if err := database.DB.QueryRow(`SELECT commit_hash, commit_message, commit_author, trigger_source
		FROM deployments WHERE site_id = 1`).Scan(&hash, &message, &author, &source); err != nil {
		t.Fatal(err)
	}
	if hash != "abc123" || message != "Ship checkout" || author != "Ada" || source != "github_webhook" {
		t.Fatalf("pending webhook metadata = (%q, %q, %q, %q)", hash, message, author, source)
	}
	inserted, err = insertWebhookDeployment(1, "main", "abc123", "Ship checkout", "Ada")
	if err != nil || inserted {
		t.Fatalf("duplicate insert = %v, %v", inserted, err)
	}
	inserted, err = insertWebhookDeployment(1, "main", "def456", "", "")
	if err != nil || !inserted {
		t.Fatalf("new commit insert = %v, %v", inserted, err)
	}
}

func TestWebhookCommitMetadataUsesMatchingHeadCommitSubject(t *testing.T) {
	var payload githubWebhookPayload
	payload.HeadCommit.ID = "abc123"
	payload.HeadCommit.Message = " Ship checkout \n\nAdditional details"
	payload.HeadCommit.Author.Name = " Ada "
	message, author := payload.commitMetadata("abc123")
	if message != "Ship checkout" || author != "Ada" {
		t.Fatalf("commit metadata = (%q, %q)", message, author)
	}
	message, author = payload.commitMetadata("def456")
	if message != "" || author != "" {
		t.Fatalf("mismatched commit metadata = (%q, %q)", message, author)
	}
}

func TestRecordObservedGitHubWebhookRequiresValidatedIdentity(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })
	if !recordObservedGitHubWebhook("acme/app", 22) {
		t.Fatal("valid hook identity was not recorded")
	}
	if recordObservedGitHubWebhook("invalid repository", 22) || recordObservedGitHubWebhook("acme/app", 0) {
		t.Fatal("invalid hook identity was recorded")
	}
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM github_webhook_observations WHERE repository = 'acme/app' AND hook_id = 22").Scan(&count); err != nil || count != 1 {
		t.Fatalf("observed hook count = %d, %v", count, err)
	}
}

func TestVerifyAndObserveGitHubWebhookRejectsUnverifiedHeaders(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })
	if err := config.InitEncryption(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("INSERT INTO github_accounts (id, name, token) VALUES (1, 'GitHub', 'token')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`INSERT INTO sites
		(id, domain, path, repository, push_to_deploy, github_account_id)
		VALUES (1, 'app.example.com', '/home/fluxo/app.example.com', 'acme/app', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	fake := &fakeGitHubWebhookProvider{deliveryOK: false}
	server := NewServer(nil, t.TempDir(), false)
	server.githubWebhookProvider = func(string) githubWebhookProvider { return fake }
	if server.verifyAndObserveGitHubWebhook("acme/app", "22", "11111111-2222-3333-4444-555555555555") {
		t.Fatal("unverified headers were accepted as webhook ownership")
	}
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM github_webhook_observations").Scan(&count); err != nil || count != 0 {
		t.Fatalf("unverified observation count = %d, %v", count, err)
	}
	fake.deliveryOK = true
	if !server.verifyAndObserveGitHubWebhook("acme/app", "22", "11111111-2222-3333-4444-555555555555") {
		t.Fatal("GitHub-verified delivery was not recorded")
	}
}

func TestRemoveSiteGitHubWebhookIgnoresStaleDisable(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })
	if _, err := database.DB.Exec(`INSERT INTO sites
		(id, domain, path, repository, push_to_deploy, github_webhook_id, github_webhook_url)
		VALUES (1, 'app.example.com', '/home/fluxo/app.example.com', 'acme/app', 1, 22,
		'https://203.0.113.10:9595/api/v1/github/webhook')`); err != nil {
		t.Fatal(err)
	}
	fake := &fakeGitHubWebhookProvider{}
	server := NewServer(nil, t.TempDir(), false)
	server.githubWebhookProvider = func(string) githubWebhookProvider { return fake }
	record, err := server.loadSiteWebhookRecord(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.removeSiteGitHubWebhook(context.Background(), record, false); err != nil {
		t.Fatal(err)
	}
	if fake.listCalls != 0 || len(fake.removedIDs) != 0 {
		t.Fatalf("stale disable touched GitHub: list=%d removed=%v", fake.listCalls, fake.removedIDs)
	}
	var storedID int64
	if err := database.DB.QueryRow("SELECT github_webhook_id FROM sites WHERE id = 1").Scan(&storedID); err != nil || storedID != 22 {
		t.Fatalf("stored webhook ID = %d, %v", storedID, err)
	}
}

func TestSiteDeletionPreservesWebhookSharedByAnotherSite(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })
	for _, values := range []struct {
		id     int
		domain string
	}{{1, "production.example.com"}, {2, "staging.example.com"}} {
		if _, err := database.DB.Exec(`INSERT INTO sites
			(id, domain, path, repository, push_to_deploy, github_webhook_id, github_webhook_url)
			VALUES (?, ?, ?, 'acme/app', 1, 22, 'https://203.0.113.10:9595/api/v1/github/webhook')`,
			values.id, values.domain, "/home/fluxo/"+values.domain); err != nil {
			t.Fatal(err)
		}
	}
	fake := &fakeGitHubWebhookProvider{}
	server := NewServer(nil, t.TempDir(), false)
	server.githubWebhookProvider = func(string) githubWebhookProvider { return fake }
	record, err := server.loadSiteWebhookRecord(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.removeSiteGitHubWebhook(context.Background(), record, true); err != nil {
		t.Fatal(err)
	}
	if fake.listCalls != 0 || len(fake.removedIDs) != 0 {
		t.Fatalf("shared webhook was touched: list=%d removed=%v", fake.listCalls, fake.removedIDs)
	}
	var productionID, stagingID int64
	_ = database.DB.QueryRow("SELECT github_webhook_id FROM sites WHERE id = 1").Scan(&productionID)
	_ = database.DB.QueryRow("SELECT github_webhook_id FROM sites WHERE id = 2").Scan(&stagingID)
	if productionID != 0 || stagingID != 22 {
		t.Fatalf("webhook IDs after deletion cleanup = (%d, %d)", productionID, stagingID)
	}
}

func TestEnsureSiteGitHubWebhookRemovesRegistrationWhenSiteDisablesMidflight(t *testing.T) {
	dataDir := t.TempDir()
	if err := database.InitDB(filepath.Join(dataDir, "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.DB.Close() })
	if err := config.InitEncryption(dataDir); err != nil {
		t.Fatal(err)
	}
	secret, err := config.EncryptSecret("webhook-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("INSERT INTO users (username, token_hash, webhook_secret) VALUES ('admin', 'hash', ?)", secret); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("INSERT INTO github_accounts (id, name, token) VALUES (1, 'GitHub', 'token')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`INSERT INTO sites
		(id, domain, path, repository, push_to_deploy, github_account_id)
		VALUES (1, 'app.example.com', '/home/fluxo/app.example.com', 'acme/app', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	fake := &fakeGitHubWebhookProvider{}
	fake.onRegister = func() {
		_, _ = database.DB.Exec("UPDATE sites SET push_to_deploy = 0 WHERE id = 1")
	}
	server := NewServer(nil, dataDir, false)
	server.githubWebhookProvider = func(string) githubWebhookProvider { return fake }
	if err := server.ensureSiteGitHubWebhook(context.Background(), 1, "https://203.0.113.10:9595/api/v1/github/webhook"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fake.removedIDs, []int64{99}) {
		t.Fatalf("registered webhook cleanup = %v, want [99]", fake.removedIDs)
	}
	var storedID int64
	if err := database.DB.QueryRow("SELECT github_webhook_id FROM sites WHERE id = 1").Scan(&storedID); err != nil || storedID != 0 {
		t.Fatalf("stored webhook ID = %d, %v", storedID, err)
	}
}
