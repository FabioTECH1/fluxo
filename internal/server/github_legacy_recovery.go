package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fluxo/internal/database"

	"github.com/golang-jwt/jwt/v5"
)

type githubLegacyRecoverySite struct {
	SiteID     int    `json:"site_id"`
	Repository string `json:"repository"`
}

// This is an explicit administrator reassociation, not proof of an old login's
// ownership. Ordinary token replacement always keeps its numeric identity check.
type githubLegacyRecovery struct {
	Password string                     `json:"password"`
	Username string                     `json:"username"`
	Confirm  bool                       `json:"confirm"`
	Sites    []githubLegacyRecoverySite `json:"sites"`
}

func authorizeGitHubLegacyRecovery(w http.ResponseWriter, r *http.Request, ctx context.Context, recovery *githubLegacyRecovery) (string, string, int, bool) {
	username := usernameFromContext(r.Context())
	if username == "" || username == "__bootstrap__" {
		http.Error(w, "Sign in again before recovering a legacy connection", 401)
		return "", "", 0, false
	}
	if !recovery.Confirm || recovery.Password == "" || len(recovery.Password) > 1024 || strings.TrimSpace(recovery.Username) == "" || len(recovery.Username) > 100 || recovery.Sites == nil || len(recovery.Sites) > 1024 {
		http.Error(w, "Legacy recovery requires your panel password, the token owner's GitHub username, explicit confirmation and the reviewed site list", 400)
		return "", "", 0, false
	}
	ip := getClientIP(r)
	attempt := loginAttemptForIP(ip, time.Now())
	loginMutex.Lock()
	blocked := attempt.count >= 5 && time.Since(attempt.lastError) <= loginAttemptWindow
	loginMutex.Unlock()
	if blocked {
		http.Error(w, "Too many login attempts. Please try again later.", 429)
		return "", "", 0, false
	}
	var credential string
	var version int
	if err := database.DB.QueryRowContext(ctx, "SELECT token_hash,token_version FROM users WHERE username=?", username).Scan(&credential, &version); err != nil || !verifyPassword(recovery.Password, credential) {
		recordLoginFailure(attempt, ip, username)
		http.Error(w, "Incorrect panel password", 401)
		return "", "", 0, false
	}
	claims, ok := r.Context().Value(authClaimsContextKey).(jwt.MapClaims)
	if !ok {
		http.Error(w, "Sign in again before recovering a legacy connection", 401)
		return "", "", 0, false
	}
	expiry, err := claims.GetExpirationTime()
	claimedVersion, hasVersion := claims["ver"].(float64)
	if err != nil || expiry == nil || !time.Now().Before(expiry.Time) || !hasVersion || claimedVersion != float64(version) {
		http.Error(w, "Sign in again before recovering a legacy connection", 401)
		return "", "", 0, false
	}
	return username, credential, version, true
}

func validateGitHubRecoverySites(account githubAccountResponse, recovery *githubLegacyRecovery) error {
	if len(recovery.Sites) != len(account.DeploymentAccess.Sites) {
		return fmt.Errorf("Site links changed; close and reopen the recovery dialog to review them")
	}
	reviewed := make(map[int]string, len(recovery.Sites))
	for _, site := range recovery.Sites {
		if site.SiteID <= 0 {
			return fmt.Errorf("Invalid recovery site list")
		}
		if _, duplicate := reviewed[site.SiteID]; duplicate {
			return fmt.Errorf("Invalid recovery site list")
		}
		reviewed[site.SiteID] = site.Repository
	}
	for _, site := range account.DeploymentAccess.Sites {
		if repository, ok := reviewed[site.SiteID]; !ok || repository != site.Repository {
			return fmt.Errorf("Site links changed; close and reopen the recovery dialog to review them")
		}
	}
	return nil
}
