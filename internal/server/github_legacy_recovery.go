package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/services/git"

	"github.com/golang-jwt/jwt/v5"
)

type githubLegacyRecoverySite struct {
	SiteID     int    `json:"site_id"`
	Repository string `json:"repository"`
}

// This is an explicit administrator reassociation, not proof of an old login's
// ownership. Ordinary token replacement always keeps its numeric identity check.
type githubLegacyRecovery struct {
	Password     string                     `json:"password"`
	Username     string                     `json:"username"`
	Confirm      bool                       `json:"confirm"`
	Sites        []githubLegacyRecoverySite `json:"sites"`
	GitHubUserID *int64                     `json:"github_user_id,omitempty"`
}

type githubRecoveryIdentity struct {
	Name         string `json:"name,omitempty"`
	Username     string `json:"username"`
	GitHubUserID int64  `json:"github_user_id"`
}

type githubRecoveryReview struct {
	Stored        githubRecoveryIdentity `json:"stored"`
	Proposed      githubRecoveryIdentity `json:"proposed"`
	Sites         []githubSiteAccess     `json:"sites"`
	NeedsRecovery bool                   `json:"needs_recovery"`
}

// Review validates identities and reads site links without saving credentials,
// repairing keys, backfilling an owner, or granting recovery authorization.
func (s *Server) handleReviewGitHubRecovery() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		id, ok := githubAccountID(w, r)
		if !ok {
			return
		}
		req, ok := decodeGitHubToken(w, r)
		if !ok {
			return
		}
		if !s.lockGitHubAccounts(w) {
			return
		}
		defer s.githubAccountMu.Unlock()
		ctx, cancel := s.githubOperationContext(r.Context())
		defer cancel()
		a, err := readGitHubAccount(ctx, id)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, sql.ErrNoRows) {
				status = http.StatusNotFound
			}
			http.Error(w, "Connection could not be loaded; reopen the dialog", status)
			return
		}
		if a.GitHubUserID != 0 {
			http.Error(w, "This connection already has a verified owner; use ordinary token replacement", 409)
			return
		}
		proposed, err := s.accountProvider(req.Token).GetIdentity(ctx)
		if err != nil {
			http.Error(w, "Could not validate GitHub token: "+err.Error(), 400)
			return
		}
		saved, savedErr := s.accountProvider(config.Decrypt(a.Token)).GetIdentity(ctx)
		needsRecovery := savedErr != nil
		if needsRecovery {
			var apiError *git.APIError
			if !errors.As(savedErr, &apiError) || apiError.Status != 401 {
				http.Error(w, "Saved-token verification is temporarily unavailable; retry before recovering this connection", 503)
				return
			}
		} else if saved.ID != proposed.ID {
			http.Error(w, "Replacement token must belong to the same GitHub account", 409)
			return
		}
		if !availableLegacyOwner(w, ctx, id, proposed.ID) {
			return
		}
		// Sites may have changed while GitHub was responding. Approval rechecks
		// this snapshot under membership admission before it can write anything.
		a, err = readGitHubAccount(ctx, id)
		if err != nil {
			http.Error(w, "Failed to review current site links", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(githubRecoveryReview{
			Stored:   githubRecoveryIdentity{Name: a.Name, Username: a.Username, GitHubUserID: a.GitHubUserID},
			Proposed: githubRecoveryIdentity{Username: proposed.Username, GitHubUserID: proposed.ID},
			Sites:    a.DeploymentAccess.Sites, NeedsRecovery: needsRecovery,
		})
	}
}

func authorizeGitHubLegacyRecovery(w http.ResponseWriter, r *http.Request, ctx context.Context, recovery *githubLegacyRecovery) (string, string, int, bool) {
	username := usernameFromContext(r.Context())
	if username == "" || username == "__bootstrap__" {
		http.Error(w, "Sign in again before recovering a legacy connection", 401)
		return "", "", 0, false
	}
	if !recovery.Confirm || recovery.Password == "" || len(recovery.Password) > 1024 || strings.TrimSpace(recovery.Username) == "" || len(recovery.Username) > 100 || recovery.Sites == nil || len(recovery.Sites) > 1024 || (recovery.GitHubUserID != nil && *recovery.GitHubUserID <= 0) {
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
