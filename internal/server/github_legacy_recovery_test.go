package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/services/git"

	"github.com/golang-jwt/jwt/v5"
)

func legacyRecoveryFixture(t *testing.T) (*Server, *fakeAccountProvider, *fakeAccountProvider, ConnectGitHubAccountRequest) {
	t.Helper()
	s, replacement := accountTestServer(t)
	insertAccountSite(t)
	hash, err := hashPassword("panel-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.DB.Exec(`UPDATE github_accounts SET github_user_id=0 WHERE id=1; INSERT INTO users(username,token_hash,token_version) VALUES('recovery-admin',?,7)`, hash); err != nil {
		t.Fatal(err)
	}
	saved := &fakeAccountProvider{identityErr: &git.APIError{Status: 401}}
	s.githubAccountProvider = func(token string) githubAccountProvider {
		if token == "old-token" {
			return saved
		}
		return replacement
	}
	ip := "203.0.113.93"
	loginMutex.Lock()
	delete(loginAttempts, ip)
	loginMutex.Unlock()
	t.Cleanup(func() { loginMutex.Lock(); delete(loginAttempts, ip); loginMutex.Unlock() })
	return s, replacement, saved, ConnectGitHubAccountRequest{
		Token: "replacement", LegacyRecovery: &githubLegacyRecovery{
			Password: "panel-password", Username: "alice", Confirm: true,
			Sites: []githubLegacyRecoverySite{{SiteID: 1, Repository: "alice/app"}},
		},
	}
}

func legacyRecoveryRequest(t *testing.T, s *Server, body ConnectGitHubAccountRequest, authenticated bool, version int) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("PUT", "/api/v1/github/accounts/1", strings.NewReader(string(raw)))
	r.RemoteAddr = "203.0.113.93:1234"
	r.SetPathValue("id", "1")
	if authenticated {
		expires := time.Now().Add(time.Hour)
		if version < 0 {
			version, expires = 7, time.Now().Add(-time.Hour)
		}
		ctx := context.WithValue(r.Context(), userContextKey, "recovery-admin")
		ctx = context.WithValue(ctx, authClaimsContextKey, jwt.MapClaims{"sub": "recovery-admin", "ver": float64(version), "exp": float64(expires.Unix())})
		r = r.WithContext(ctx)
	}
	w := httptest.NewRecorder()
	s.handleUpdateGitHubAccount()(w, r)
	return w
}

func TestLegacyGitHubRecoveryExplicitlyBindsIdentityAndPreservesLinks(t *testing.T) {
	s, replacement, _, body := legacyRecoveryFixture(t)
	// Recovery selects the administrator-confirmed new account. It must not
	// infer old ownership from a login, including one that may have been reused.
	replacement.id, replacement.username = 202, "chosen-account"
	body.LegacyRecovery.Username = " CHOSEN-ACCOUNT "
	w := legacyRecoveryRequest(t, s, body, true, 7)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	a, err := readGitHubAccount(context.Background(), 1)
	if err != nil || a.ID != 1 || a.GitHubUserID != 202 || a.Username != "chosen-account" || a.LinkedSites != 1 || config.Decrypt(a.Token) != "replacement" || a.Token == "replacement" {
		t.Fatalf("unexpected recovered connection: id=%d owner=%d links=%d err=%v", a.ID, a.GitHubUserID, a.LinkedSites, err)
	}
	var accountID int
	var repository string
	if err = database.DB.QueryRow("SELECT github_account_id,repository FROM sites WHERE id=1").Scan(&accountID, &repository); err != nil || accountID != 1 || repository != "alice/app" {
		t.Fatal("site association changed", err)
	}
	if strings.Contains(w.Body.String(), "panel-password") || strings.Contains(w.Body.String(), "replacement") {
		t.Fatal("response exposed credentials")
	}
	var audit int
	if err = database.DB.QueryRow("SELECT count(*) FROM activity WHERE type='github_legacy_recovered'").Scan(&audit); err != nil || audit != 1 {
		t.Fatal("recovery audit missing", err)
	}
}

func TestLegacyGitHubRecoveryRefusesUnapprovedOrChangedInputs(t *testing.T) {
	for _, scenario := range []string{"ordinary", "unauthenticated", "wrong-password", "unconfirmed", "wrong-username", "missing-sites", "stale-sites", "changed-repository", "duplicate-sites", "known-owner", "duplicate-owner", "saved-valid", "saved-network", "saved-forbidden", "new-invalid", "stale-session", "expired-session", "password-change", "session-change", "storage-failure", "audit-failure", "rate-limit"} {
		t.Run(scenario, func(t *testing.T) {
			s, replacement, saved, body := legacyRecoveryFixture(t)
			authenticated, version, expected := true, 7, 409
			switch scenario {
			case "ordinary":
				body.LegacyRecovery = nil
			case "unauthenticated":
				authenticated, expected = false, 401
			case "wrong-password":
				body.LegacyRecovery.Password, expected = "wrong", 401
			case "unconfirmed":
				body.LegacyRecovery.Confirm, expected = false, 400
			case "wrong-username":
				body.LegacyRecovery.Username = "someone-else"
			case "stale-sites":
				body.LegacyRecovery.Sites = []githubLegacyRecoverySite{}
			case "missing-sites":
				body.LegacyRecovery.Sites, expected = nil, 400
			case "changed-repository":
				body.LegacyRecovery.Sites[0].Repository = "alice/old"
			case "duplicate-sites":
				body.LegacyRecovery.Sites = append(body.LegacyRecovery.Sites, body.LegacyRecovery.Sites[0])
			case "known-owner":
				database.DB.Exec("UPDATE github_accounts SET github_user_id=101 WHERE id=1")
			case "duplicate-owner":
				database.DB.Exec("INSERT INTO github_accounts(name,username,github_user_id,token) VALUES('Other','alice',101,'unrelated')")
			case "saved-valid":
				saved.id, saved.username, saved.identityErr = 101, "alice", nil
			case "saved-network":
				saved.identityErr, expected = errors.New("network unavailable"), 503
			case "saved-forbidden":
				saved.identityErr, expected = &git.APIError{Status: 403}, 503
			case "new-invalid":
				replacement.identityErr, expected = &git.APIError{Status: 401}, 400
			case "stale-session":
				version, expected = 6, 401
			case "expired-session":
				version, expected = -1, 401
			case "password-change", "session-change":
				change := "UPDATE users SET token_hash='changed' WHERE username='recovery-admin'"
				if scenario == "session-change" {
					change = "UPDATE users SET token_version=8 WHERE username='recovery-admin'"
				}
				s.githubAccountProvider = func(token string) githubAccountProvider {
					if token == "old-token" {
						return saved
					}
					return &recoveryHookProvider{fakeAccountProvider: replacement, hook: func() {
						if _, err := database.DB.Exec(change); err != nil {
							t.Fatal(err)
						}
					}}
				}
			case "storage-failure":
				database.DB.Exec("CREATE TRIGGER reject_recovery BEFORE UPDATE OF github_user_id ON github_accounts BEGIN SELECT RAISE(ABORT,'fixture'); END")
				expected = 500
			case "audit-failure":
				database.DB.Exec("CREATE TRIGGER reject_recovery_audit BEFORE INSERT ON activity WHEN NEW.type='github_legacy_recovered' BEGIN SELECT RAISE(ABORT,'fixture'); END")
				expected = 500
			case "rate-limit":
				attempt := loginAttemptForIP("203.0.113.93", time.Now())
				loginMutex.Lock()
				attempt.count = 5
				loginMutex.Unlock()
				expected = 429
			}
			w := legacyRecoveryRequest(t, s, body, authenticated, version)
			if w.Code != expected {
				t.Fatalf("%d want %d: %s", w.Code, expected, w.Body.String())
			}
			a, err := readGitHubAccount(context.Background(), 1)
			if err != nil || config.Decrypt(a.Token) != "old-token" || a.LinkedSites != 1 {
				t.Fatal("refusal changed credential or site links", err)
			}
			expectedOwner := int64(0)
			if scenario == "known-owner" {
				expectedOwner = 101
			}
			if a.GitHubUserID != expectedOwner {
				t.Fatal("refusal changed the saved numeric identity")
			}
			if strings.Contains(w.Body.String(), "panel-password") || strings.Contains(w.Body.String(), "old-token") {
				t.Fatal("refusal exposed credentials")
			}
		})
	}
}

func TestLegacyGitHubRecoveryPayloadOnlyAppliesToReplacement(t *testing.T) {
	s, _, _, body := legacyRecoveryFixture(t)
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	w := accountRequest(s.handleConnectGitHubAccount(), "POST", "/api/v1/github/accounts", string(raw))
	if w.Code != 400 {
		t.Fatalf("POST accepted recovery: %d %s", w.Code, w.Body.String())
	}
	w = accountRequest(s.handleUpdateGitHubAccount(), "PUT", "/api/v1/github/accounts/1", `{"token":"replacement","legacy_recovery":{"force":true}}`)
	if w.Code != 400 {
		t.Fatalf("unknown recovery fields: %d %s", w.Code, w.Body.String())
	}
}

func TestLegacyGitHubRecoveryWithNoRepositories(t *testing.T) {
	s, _, _, body := legacyRecoveryFixture(t)
	if _, err := database.DB.Exec("UPDATE sites SET repository='' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	body.LegacyRecovery.Sites = []githubLegacyRecoverySite{}
	w := legacyRecoveryRequest(t, s, body, true, 7)
	if w.Code != 200 {
		t.Fatalf("empty reviewed list: %d %s", w.Code, w.Body.String())
	}
	var connection int
	if err := database.DB.QueryRow("SELECT github_account_id FROM sites WHERE id=1").Scan(&connection); err != nil || connection != 1 {
		t.Fatal("empty-repository site link lost", err)
	}
}

type recoveryHookProvider struct {
	*fakeAccountProvider
	hook func()
}

func (p *recoveryHookProvider) GetIdentity(ctx context.Context) (git.Identity, error) {
	p.hook()
	return p.fakeAccountProvider.GetIdentity(ctx)
}
