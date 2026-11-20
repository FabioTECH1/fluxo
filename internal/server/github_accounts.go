package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/services/git"
)

type ConnectGitHubAccountRequest struct {
	Name           string                `json:"name"`
	Token          string                `json:"token"`
	LegacyRecovery *githubLegacyRecovery `json:"legacy_recovery,omitempty"`
}
type githubAccountProvider interface {
	GetIdentity(context.Context) (git.Identity, error)
	EnsureDeployKey(context.Context, string, string) (int64, error)
	RemoveDeployKey(string, int64) error
}
type githubSiteAccess struct {
	SiteID     int    `json:"site_id"`
	Domain     string `json:"domain"`
	Repository string `json:"repository"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	Reason     string `json:"reason,omitempty"`
}
type githubAccessReport struct {
	CheckedAt *time.Time         `json:"checked_at"`
	Sites     []githubSiteAccess `json:"sites"`
}
type githubAccountResponse struct {
	database.GitHubAccount
	TokenExpiresAt   *time.Time         `json:"token_expires_at"`
	TokenStatus      string             `json:"token_status"`
	TokenCheckedAt   *time.Time         `json:"token_checked_at"`
	LinkedSites      int                `json:"linked_sites"`
	DeploymentAccess githubAccessReport `json:"deployment_access"`
}

func (s *Server) accountProvider(token string) githubAccountProvider {
	if s.githubAccountProvider != nil {
		return s.githubAccountProvider(token)
	}
	return git.NewGitHubProvider(token)
}
func parseAccountTime(value string) *time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	return &t
}
func accountTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
func linkedGitHubSites(ctx context.Context, id int) ([]githubSiteAccess, error) {
	rows, err := database.DB.QueryContext(ctx, "SELECT id,domain,repository FROM sites WHERE github_account_id=? AND COALESCE(repository,'')<>'' ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sites := []githubSiteAccess{}
	for rows.Next() {
		var site githubSiteAccess
		if err := rows.Scan(&site.SiteID, &site.Domain, &site.Repository); err != nil {
			return nil, err
		}
		site.Status = "unchecked"
		sites = append(sites, site)
	}
	return sites, rows.Err()
}
func readGitHubAccount(ctx context.Context, id int) (githubAccountResponse, error) {
	var a githubAccountResponse
	var expiry, checked, report string
	err := database.DB.QueryRowContext(ctx, `SELECT id,name,username,github_user_id,token,created_at,updated_at,token_expires_at,token_status,token_checked_at,deployment_access FROM github_accounts WHERE id=?`, id).Scan(&a.ID, &a.Name, &a.Username, &a.GitHubUserID, &a.Token, &a.CreatedAt, &a.UpdatedAt, &expiry, &a.TokenStatus, &checked, &report)
	if err != nil {
		return a, err
	}
	a.TokenExpiresAt = parseAccountTime(expiry)
	a.TokenCheckedAt = parseAccountTime(checked)
	sites, err := linkedGitHubSites(ctx, id)
	if err != nil {
		return a, err
	}
	a.LinkedSites = len(sites)
	var previous githubAccessReport
	_ = json.Unmarshal([]byte(report), &previous)
	a.DeploymentAccess = githubAccessReport{Sites: sites}
	// Repository changes, new sites and deletions invalidate the previous snapshot.
	same := len(sites) == len(previous.Sites) && previous.CheckedAt != nil
	for i := range sites {
		if !same {
			break
		}
		if sites[i].SiteID != previous.Sites[i].SiteID || sites[i].Repository != previous.Sites[i].Repository {
			same = false
		}
	}
	if same {
		a.DeploymentAccess = previous
		for i := range sites {
			a.DeploymentAccess.Sites[i].Domain = sites[i].Domain
		}
	}
	return a, nil
}
func writeGitHubAccount(w http.ResponseWriter, status int, a githubAccountResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(a)
}
func githubAccountID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.Error(w, "Invalid account ID", 400)
		return 0, false
	}
	return id, true
}
func (s *Server) githubOperationContext(parent context.Context) (context.Context, context.CancelFunc) {
	timeout := s.githubOperationTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return context.WithTimeout(parent, timeout)
}
func finalGitHubAccount(r *http.Request, id int) (githubAccountResponse, error) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	return readGitHubAccount(ctx, id)
}
func (s *Server) lockGitHubAccounts(w http.ResponseWriter) bool {
	if !s.githubAccountMu.TryLock() {
		http.Error(w, "Another GitHub connection operation is in progress; retry shortly", 409)
		return false
	}
	return true
}

func (s *Server) handleListGitHubAccounts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.DB.QueryContext(r.Context(), "SELECT id FROM github_accounts ORDER BY name ASC")
		if err != nil {
			http.Error(w, "Failed to list accounts", 500)
			return
		}
		ids := []int{}
		for rows.Next() {
			var id int
			if err = rows.Scan(&id); err != nil {
				break
			}
			ids = append(ids, id)
		}
		rowErr := rows.Err()
		rows.Close()
		if err != nil || rowErr != nil {
			http.Error(w, "Failed to list accounts", 500)
			return
		}
		accounts := []githubAccountResponse{}
		for _, id := range ids {
			a, err := readGitHubAccount(r.Context(), id)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				http.Error(w, "Failed to list accounts", 500)
				return
			}
			accounts = append(accounts, a)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(accounts)
	}
}

func decodeGitHubToken(w http.ResponseWriter, r *http.Request) (ConnectGitHubAccountRequest, bool) {
	var req ConnectGitHubAccountRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "Invalid account payload", 400)
		return req, false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		http.Error(w, "Invalid account payload", 400)
		return req, false
	}
	req.Token = strings.TrimSpace(req.Token)
	req.Name = strings.TrimSpace(req.Name)
	if req.LegacyRecovery != nil && r.Method != http.MethodPut {
		http.Error(w, "Legacy recovery is only available for an existing connection", 400)
		return req, false
	}
	if req.Token == "" || len(req.Token) > 4096 || len(req.Name) > 100 {
		http.Error(w, "Token is required (maximum 4096 characters); account label must be at most 100 characters", 400)
		return req, false
	}
	return req, true
}

func (s *Server) handleConnectGitHubAccount() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		identity, err := s.accountProvider(req.Token).GetIdentity(ctx)
		if err != nil {
			http.Error(w, "Could not validate GitHub token: "+err.Error(), 400)
			return
		}
		var exists bool
		if err := database.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM github_accounts WHERE github_user_id=? OR (github_user_id=0 AND lower(username)=lower(?)))", identity.ID, identity.Username).Scan(&exists); err != nil {
			http.Error(w, "Failed to check existing accounts", 500)
			return
		}
		if exists {
			http.Error(w, "This GitHub account is already connected; use Update token", 409)
			return
		}
		encrypted, err := config.EncryptSecret(req.Token)
		if err != nil {
			http.Error(w, "Failed to encrypt GitHub token", 500)
			return
		}
		if req.Name == "" {
			req.Name = identity.Username
		}
		id, err := s.saveGitHubConnection(ctx, req.Name, identity, encrypted)
		if err != nil {
			http.Error(w, "Failed to save account and restore its site links", 500)
			return
		}
		if err = s.checkGitHubSites(ctx, int(id), req.Token, true); err != nil {
			http.Error(w, "Account saved, but deployment checks could not be saved", 500)
			return
		}
		a, err := finalGitHubAccount(r, int(id))
		if err != nil {
			http.Error(w, "Account saved, but its status could not be loaded", 500)
			return
		}
		writeGitHubAccount(w, 201, a)
	}
}

func (s *Server) saveGitHubConnection(ctx context.Context, name string, identity git.Identity, encrypted string) (int64, error) {
	s.githubMembershipMu.Lock()
	defer s.githubMembershipMu.Unlock()
	s.githubWebhookMu.Lock()
	defer s.githubWebhookMu.Unlock()
	tx, err := database.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `INSERT INTO github_accounts(name,username,github_user_id,token,token_expires_at,token_status,token_checked_at) VALUES(?,?,?,?,?,'valid',?)`, name, identity.Username, identity.ID, encrypted, accountTime(identity.ExpiresAt), accountTime(&now))
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	// Username-only historical markers do not establish ownership.
	if _, err := tx.ExecContext(ctx, `UPDATE sites SET github_account_id=?,github_disconnected_username='',github_disconnected_user_id=0 WHERE github_account_id=0 AND github_disconnected_user_id=?`, id, identity.ID); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func availableLegacyOwner(w http.ResponseWriter, ctx context.Context, localID int, userID int64) bool {
	var exists bool
	if err := database.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM github_accounts WHERE github_user_id=? AND id!=?)", userID, localID).Scan(&exists); err != nil {
		http.Error(w, "Failed to check existing accounts", 500)
		return false
	}
	if exists {
		http.Error(w, "This GitHub account is already connected; use Update token", 409)
		return false
	}
	return true
}

func (s *Server) handleUpdateGitHubAccount() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "Account not found", 404)
			} else {
				http.Error(w, "Failed to load account", 500)
			}
			return
		}
		var recoveryActor, recoveryCredential string
		var recoveryVersion int
		if req.LegacyRecovery != nil {
			if a.GitHubUserID != 0 {
				http.Error(w, "This connection already has a verified owner; use ordinary token replacement", 409)
				return
			}
			var authorized bool
			recoveryActor, recoveryCredential, recoveryVersion, authorized = authorizeGitHubLegacyRecovery(w, r, ctx, req.LegacyRecovery)
			if !authorized {
				return
			}
		}
		identity, err := s.accountProvider(req.Token).GetIdentity(ctx)
		if err != nil {
			http.Error(w, "Could not validate GitHub token: "+err.Error(), 400)
			return
		}
		ownerID := a.GitHubUserID
		if req.LegacyRecovery != nil && req.LegacyRecovery.GitHubUserID != nil && *req.LegacyRecovery.GitHubUserID != identity.ID {
			http.Error(w, "The new token's GitHub account changed; review the connection again", 409)
			return
		}
		recovered := false
		membershipLocked := false
		defer func() {
			if membershipLocked {
				s.githubMembershipMu.Unlock()
			}
		}()
		if ownerID == 0 {
			saved, err := s.accountProvider(config.Decrypt(a.Token)).GetIdentity(ctx)
			if err != nil {
				var apiError *git.APIError
				if !errors.As(err, &apiError) || apiError.Status != 401 {
					http.Error(w, "Saved-token verification is temporarily unavailable; retry before recovering this connection", 503)
					return
				}
				if req.LegacyRecovery == nil {
					http.Error(w, unresolvedGitHubOwner, http.StatusConflict)
					return
				}
				if !strings.EqualFold(strings.TrimSpace(req.LegacyRecovery.Username), identity.Username) {
					http.Error(w, "Confirmation username must match the GitHub account authenticated by the new token", 409)
					return
				}
				s.githubMembershipMu.Lock()
				membershipLocked = true
				a, err = readGitHubAccount(ctx, id)
				if err != nil {
					http.Error(w, "Failed to reload legacy connection", 500)
					return
				}
				if err = validateGitHubRecoverySites(a, req.LegacyRecovery); err != nil {
					http.Error(w, err.Error(), 409)
					return
				}
				ownerID, recovered = identity.ID, true
			} else {
				if req.LegacyRecovery != nil {
					http.Error(w, "The saved token verified the owner; use ordinary token replacement", 409)
					return
				}
				ownerID = saved.ID
			}
			if !availableLegacyOwner(w, ctx, id, ownerID) {
				return
			}
		}
		if ownerID != identity.ID {
			http.Error(w, "Replacement token must belong to the same GitHub account", 409)
			return
		}
		encrypted, err := config.EncryptSecret(req.Token)
		if err != nil {
			http.Error(w, "Failed to encrypt GitHub token", 500)
			return
		}
		now := time.Now().UTC()
		tx, err := database.DB.BeginTx(ctx, nil)
		if err != nil {
			http.Error(w, "Failed to save token", 500)
			return
		}
		defer tx.Rollback()
		query := `UPDATE github_accounts SET username=?,github_user_id=?,token=?,token_expires_at=?,token_status='valid',token_checked_at=?,deployment_access='{}',updated_at=CURRENT_TIMESTAMP WHERE id=?`
		args := []any{identity.Username, identity.ID, encrypted, accountTime(identity.ExpiresAt), accountTime(&now), id}
		if recovered {
			query += ` AND github_user_id=0 AND token=? AND EXISTS(SELECT 1 FROM users WHERE username=? AND token_hash=? AND token_version=?)`
			args = append(args, a.Token, recoveryActor, recoveryCredential, recoveryVersion)
		}
		result, err := tx.ExecContext(ctx, query, args...)
		if err == nil && recovered {
			if changed, countErr := result.RowsAffected(); countErr != nil || changed != 1 {
				http.Error(w, "Connection or panel authentication changed; sign in and review recovery again", 409)
				return
			}
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, "DELETE FROM github_cache WHERE key=? OR key LIKE ?", fmt.Sprintf("repos:account:%d", id), fmt.Sprintf("branches:%%:account:%d", id))
		}
		if err == nil && recovered {
			_, err = tx.ExecContext(ctx, "INSERT INTO activity(site_id,type,summary,username,ip_address) VALUES(0,?,?,?,?)", "github_legacy_recovered", fmt.Sprintf("Administrator recovered GitHub connection %d as @%s (GitHub ID %d); existing site links retained", id, identity.Username, identity.ID), recoveryActor, getClientIP(r))
		}
		if err != nil {
			http.Error(w, "Failed to save token", 500)
			return
		}
		if err = tx.Commit(); err != nil {
			http.Error(w, "Failed to save token", 500)
			return
		}
		if recovered {
			s.githubMembershipMu.Unlock()
			membershipLocked = false
		}
		if err = s.checkGitHubSites(ctx, id, req.Token, true); err != nil {
			http.Error(w, "Token saved, but deployment checks could not be saved", 500)
			return
		}
		a, err = finalGitHubAccount(r, id)
		if err != nil {
			http.Error(w, "Token saved, but its status could not be loaded", 500)
			return
		}
		writeGitHubAccount(w, 200, a)
	}
}

func writeGitHubSiteLockError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "Site not found", 404)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		http.Error(w, "Site operation canceled", 408)
	default:
		http.Error(w, "Failed to load site", 500)
	}
}

func (s *Server) lockExistingGitHubSite(ctx context.Context, id int) (func(), error) {
	// An active reservation already establishes existence, including during
	// provisioning. Otherwise a request for a future ID must not reserve it.
	if _, active := s.githubSiteOperations.Load(id); !active {
		var existing int
		if err := database.DB.QueryRowContext(ctx, "SELECT id FROM sites WHERE id=?", id).Scan(&existing); err != nil {
			return nil, err
		}
	}
	return s.lockGitHubSite(ctx, id)
}

func (s *Server) lockGitHubSite(ctx context.Context, id int) (func(), error) {
	done := make(chan struct{})
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		existing, busy := s.githubSiteOperations.LoadOrStore(id, done)
		if !busy {
			return func() { s.githubSiteOperations.Delete(id); close(done) }, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-existing.(chan struct{}):
		}
	}
}

func (s *Server) checkGitHubSites(ctx context.Context, id int, token string, repair bool) error {
	dbCtx, cancelDB := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	sites, err := linkedGitHubSites(dbCtx, id)
	cancelDB()
	if err != nil {
		return err
	}
	provider := s.accountProvider(token)
	for i := range sites {
		site := &sites[i]
		err := s.verifyGitHubSite(ctx, id, *site, provider, repair)
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			site.Status, site.Message = "error", "Check timed out; retry Check access"
			site.Reason = "check_timeout"
		case ctx.Err() != nil:
			site.Status, site.Message = "error", "Check canceled; retry Check access"
			site.Reason = "check_canceled"
		case err != nil:
			site.Status, site.Message = "error", err.Error()
			site.Reason = githubAccessReason(err)
		default:
			site.Status, site.Message = "verified", "SSH repository access verified"
		}
	}
	now := time.Now().UTC()
	report := githubAccessReport{CheckedAt: &now, Sites: sites}
	encoded, err := json.Marshal(report)
	if err != nil {
		return err
	}
	// Persist partial/timeout results too; they must never appear verified.
	dbCtx, cancelDB = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelDB()
	_, err = database.DB.ExecContext(dbCtx, "UPDATE github_accounts SET deployment_access=? WHERE id=?", string(encoded), id)
	return err
}

var errGitHubSiteChanged = errors.New("Site settings changed; retry Check access")
var errGitHubRegistration = errors.New("Deploy key registration failed")

// Presentation metadata describes only observed failure stages. In particular,
// SSH failure cannot establish a missing remote registration or network fault.
func githubAccessReason(err error) string {
	switch {
	case errors.Is(err, git.ErrDeployKeyUnavailable):
		return "private_key_unavailable"
	case errors.Is(err, git.ErrDeploySSHAccess):
		return "ssh_access_failed"
	case errors.Is(err, errGitHubSiteChanged):
		return "site_changed"
	case errors.Is(err, errGitHubRegistration):
		return "registration_failed"
	default:
		return "check_failed"
	}
}

func (s *Server) verifyGitHubSite(ctx context.Context, accountID int, site githubSiteAccess, provider githubAccountProvider, repair bool) error {
	unlock, err := s.lockGitHubSite(ctx, site.SiteID)
	if err != nil {
		return err
	}
	defer unlock()
	var current bool
	err = database.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sites WHERE id=? AND github_account_id=? AND repository=? AND COALESCE(deletion_status,'')='')`, site.SiteID, accountID, site.Repository).Scan(&current)
	if err != nil {
		return errors.New("Site access check unavailable; retry Check access")
	}
	if !current {
		return errGitHubSiteChanged
	}
	check := s.githubAccessCheck
	if check == nil {
		check = git.CheckDeployAccess
	}
	err = check(ctx, site.SiteID, site.Repository)
	if !repair || ctx.Err() != nil {
		return err
	}
	public := s.githubPublicKey
	if public == nil {
		public = git.DeployPublicKey
	}
	key, err := public(site.SiteID)
	if err != nil {
		return err
	}
	keyID, err := provider.EnsureDeployKey(ctx, site.Repository, key)
	if err != nil {
		return fmt.Errorf("%w: %v", errGitHubRegistration, err)
	}
	// A previous remote registration may have succeeded before its local write
	// failed. Always resolve its ID during repair, even when SSH already works.
	return recordSiteDeployKey(ctx, site.SiteID, accountID, site.Repository, keyID, func() error {
		return check(ctx, site.SiteID, site.Repository)
	})
}

func recordSiteDeployKey(ctx context.Context, siteID, accountID int, repo string, keyID int64, verify func() error) error {
	if keyID <= 0 {
		return errors.New("GitHub returned no deploy key registration")
	}
	// Retain the result of a successful remote operation even if its request
	// was canceled. The independent database write is bounded and still binds
	// the exact site, account and repository reserved by the caller.
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	result, err := database.DB.ExecContext(dbCtx, `UPDATE sites SET github_deploy_key_id=? WHERE id=? AND github_account_id=? AND repository=?`, keyID, siteID, accountID, repo)
	if err != nil {
		return errors.New("Could not save the site's deploy key registration")
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errors.New("Site settings changed; retry Check access")
	}
	if verify != nil {
		return verify()
	}
	return nil
}

func (s *Server) handleCheckGitHubAccount() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := githubAccountID(w, r)
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
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "Account not found", 404)
			} else {
				http.Error(w, "Failed to load account", 500)
			}
			return
		}
		token := config.Decrypt(a.Token)
		identity, checkErr := s.accountProvider(token).GetIdentity(ctx)
		now := time.Now().UTC()
		status := "valid"
		expiry := a.TokenExpiresAt
		if checkErr != nil {
			status = "error"
			var apiError *git.APIError
			if errors.As(checkErr, &apiError) && apiError.Status == 401 {
				status = "invalid"
			}
		} else if a.GitHubUserID != 0 && a.GitHubUserID != identity.ID {
			status = "invalid"
			checkErr = errors.New("Stored token does not match the connected GitHub account")
		} else {
			if a.GitHubUserID == 0 && !availableLegacyOwner(w, ctx, id, identity.ID) {
				return
			}
			expiry = identity.ExpiresAt
		}
		if _, err = database.DB.ExecContext(ctx, `UPDATE github_accounts SET token_status=?,token_expires_at=?,token_checked_at=?,deployment_access='{}',username=CASE WHEN ?='valid' THEN ? ELSE username END,github_user_id=CASE WHEN ?='valid' THEN ? ELSE github_user_id END WHERE id=?`, status, accountTime(expiry), accountTime(&now), status, identity.Username, status, identity.ID, id); err != nil {
			http.Error(w, "Failed to save connection status", 500)
			return
		}
		err = s.checkGitHubSites(ctx, id, token, false)
		if err != nil {
			http.Error(w, "Failed to save deployment check", 500)
			return
		}
		a, err = finalGitHubAccount(r, id)
		if err != nil {
			http.Error(w, "Failed to load connection status", 500)
			return
		}
		writeGitHubAccount(w, 200, a)
	}
}

const unresolvedGitHubOwner = "The saved credential could not verify this legacy GitHub account. Its site links have been preserved."

func (s *Server) handleDeleteGitHubAccount() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := githubAccountID(w, r)
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
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Account not found", 404)
			return
		}
		if err != nil {
			http.Error(w, "Failed to load account", 500)
			return
		}
		ownerID, username := a.GitHubUserID, a.Username
		if ownerID == 0 {
			if saved, err := s.accountProvider(config.Decrypt(a.Token)).GetIdentity(ctx); err == nil {
				ownerID, username = saved.ID, saved.Username
			}
		}
		s.githubMembershipMu.Lock()
		defer s.githubMembershipMu.Unlock()
		s.githubWebhookMu.Lock()
		defer s.githubWebhookMu.Unlock()
		tx, err := database.DB.BeginTx(r.Context(), nil)
		if err != nil {
			http.Error(w, "Failed to disconnect account", 500)
			return
		}
		defer tx.Rollback()
		if ownerID == 0 {
			var linked bool
			if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM sites WHERE github_account_id=?)", id).Scan(&linked); err != nil {
				http.Error(w, "Failed to load site links", 500)
				return
			}
			if linked {
				http.Error(w, unresolvedGitHubOwner, 409)
				return
			}
		}
		statements := []struct {
			query string
			args  []any
		}{
			{"UPDATE sites SET github_account_id=0,github_disconnected_username=?,github_disconnected_user_id=? WHERE github_account_id=?", []any{username, ownerID, id}},
			{"DELETE FROM github_cache WHERE key=? OR key LIKE ?", []any{fmt.Sprintf("repos:account:%d", id), fmt.Sprintf("branches:%%:account:%d", id)}},
			{"DELETE FROM github_accounts WHERE id=?", []any{id}},
		}
		for _, statement := range statements {
			if _, err = tx.ExecContext(r.Context(), statement.query, statement.args...); err != nil {
				http.Error(w, "Failed to disconnect account", 500)
				return
			}
		}
		if err = tx.Commit(); err != nil {
			http.Error(w, "Failed to disconnect account", 500)
			return
		}
		w.WriteHeader(204)
	}
}
