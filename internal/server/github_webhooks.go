package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	gitservice "fluxo/internal/services/git"
)

const githubWebhookPath = "/api/v1/github/webhook"

type githubWebhookProvider interface {
	ListWebhooks(repoFullName string) ([]gitservice.Webhook, error)
	HasWebhookDelivery(repoFullName string, hookID int64, deliveryGUID string) (bool, error)
	RegisterWebhook(repoFullName, webhookURL, secret string) (int64, error)
	UpdateWebhook(repoFullName string, hookID int64, webhookURL, secret string) error
	RemoveWebhook(repoFullName string, hookID int64) error
}

var githubDeliveryGUIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func newGitHubWebhookProvider(token string) githubWebhookProvider {
	return gitservice.NewGitHubProvider(token)
}

type siteWebhookRecord struct {
	SiteID     int
	Repository string
	AccountID  int
	Enabled    bool
	HookID     int64
	HookURL    string
}

type webhookReconcilePlan struct {
	Keep      *gitservice.Webhook
	CreateURL string
	DeleteIDs []int64
}

func siteWebhookTransition(currentEnabled, desiredEnabled, repositoryChanged bool) (removePrevious, ensureCurrent bool) {
	removePrevious = currentEnabled && (!desiredEnabled || repositoryChanged)
	ensureCurrent = desiredEnabled && (!currentEnabled || repositoryChanged)
	return removePrevious, ensureCurrent
}

func parseFluxoWebhookURL(raw string) (*url.URL, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, false
	}
	if parsed.Hostname() == "" || parsed.Path != githubWebhookPath {
		return nil, false
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || !safeinput.ValidatePortNumber(value) {
			return nil, false
		}
	}
	return parsed, true
}

func canonicalWebhookURL(raw string) string {
	parsed, ok := parseFluxoWebhookURL(raw)
	if !ok || parsed.Scheme != "https" || !webhookHostIsPublic(parsed.Hostname()) {
		return ""
	}
	return parsed.String()
}

func webhookURLForHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, "\r\n") {
		return ""
	}
	return canonicalWebhookURL((&url.URL{Scheme: "https", Host: host, Path: githubWebhookPath}).String())
}

func webhookHostIsPrivate(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

func webhookHostIsPublic(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsGlobalUnicast() && !webhookHostIsPrivate(host)
	}
	return safeinput.ValidateDomain(host)
}

func webhookIsObsolete(parsed *url.URL) bool {
	return parsed.Scheme != "https" || !webhookHostIsPublic(parsed.Hostname())
}

func webhookIsPublicIP(parsed *url.URL) bool {
	return parsed.Scheme == "https" && net.ParseIP(parsed.Hostname()) != nil && !webhookHostIsPrivate(parsed.Hostname())
}

func webhookEndpointKey(parsed *url.URL) string {
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return strings.ToLower(net.JoinHostPort(strings.TrimSuffix(parsed.Hostname(), "."), port))
}

func localWebhookEndpoints() map[string]bool {
	endpoints := make(map[string]bool)
	port := strings.TrimSpace(config.LoadConfig().Port)
	if _, err := strconv.Atoi(port); err != nil {
		return endpoints
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return endpoints
	}
	for _, address := range addresses {
		var ip net.IP
		switch value := address.(type) {
		case *net.IPNet:
			ip = value.IP
		case *net.IPAddr:
			ip = value.IP
		}
		if ip != nil {
			endpoints[strings.ToLower(net.JoinHostPort(ip.String(), port))] = true
		}
	}
	return endpoints
}

func sameWebhookURL(left, right string) bool {
	leftURL, leftOK := parseFluxoWebhookURL(left)
	rightURL, rightOK := parseFluxoWebhookURL(right)
	if !leftOK || !rightOK {
		return false
	}
	return strings.EqualFold(leftURL.Scheme, rightURL.Scheme) &&
		strings.EqualFold(leftURL.Host, rightURL.Host) && leftURL.Path == rightURL.Path
}

func planWebhookReconciliation(hooks []gitservice.Webhook, storedID int64, storedURL, panelDomain, preferredURL string, observedIDs map[int64]bool, localEndpoints map[string]bool) webhookReconcilePlan {
	type candidate struct {
		hook     gitservice.Webhook
		parsed   *url.URL
		owned    bool
		observed bool
		publicIP bool
		obsolete bool
	}

	panelDomain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(panelDomain)), ".")
	panelURL := webhookURLForHost(panelDomain)
	storedURL = canonicalWebhookURL(storedURL)
	preferredURL = canonicalWebhookURL(preferredURL)
	candidates := make([]candidate, 0, len(hooks))
	storedCandidateIndex := -1
	for _, hook := range hooks {
		parsed, ok := parseFluxoWebhookURL(hook.Config.URL)
		if !ok {
			continue
		}
		isPanel := panelURL != "" && sameWebhookURL(hook.Config.URL, panelURL)
		isStored := hook.ID == storedID || (storedURL != "" && sameWebhookURL(hook.Config.URL, storedURL))
		isPreferred := preferredURL != "" && sameWebhookURL(hook.Config.URL, preferredURL)
		isObserved := observedIDs[hook.ID]
		isLocalEndpoint := localEndpoints[webhookEndpointKey(parsed)]
		item := candidate{
			hook:     hook,
			parsed:   parsed,
			owned:    isStored || isPanel || isPreferred || isObserved || isLocalEndpoint,
			observed: isObserved,
			publicIP: webhookIsPublicIP(parsed),
			obsolete: webhookIsObsolete(parsed),
		}
		if isStored && storedCandidateIndex == -1 {
			storedCandidateIndex = len(candidates)
		}
		candidates = append(candidates, item)
	}

	keepIndex := -1
	for index := range candidates {
		code := candidates[index].hook.LastResponse.Code
		if candidates[index].owned && candidates[index].publicIP && !candidates[index].obsolete &&
			(candidates[index].observed || (code >= 200 && code < 300)) {
			keepIndex = index
			break
		}
	}
	if keepIndex < 0 && storedCandidateIndex >= 0 && !candidates[storedCandidateIndex].obsolete {
		keepIndex = storedCandidateIndex
	}
	if keepIndex < 0 {
		for index := range candidates {
			if candidates[index].owned && !candidates[index].obsolete && candidates[index].hook.LastResponse.Code == 200 {
				keepIndex = index
				break
			}
		}
		if keepIndex < 0 {
			for index := range candidates {
				if candidates[index].owned && !candidates[index].obsolete {
					keepIndex = index
					break
				}
			}
		}
	}

	plan := webhookReconcilePlan{}
	if keepIndex >= 0 {
		kept := candidates[keepIndex].hook
		plan.Keep = &kept
	} else if preferredURL != "" {
		plan.CreateURL = preferredURL
	}

	deleteSet := make(map[int64]struct{})
	for index, item := range candidates {
		if keepIndex == index {
			continue
		}
		duplicateOfKept := keepIndex >= 0 && sameWebhookURL(item.hook.Config.URL, candidates[keepIndex].hook.Config.URL)
		if item.owned || duplicateOfKept {
			deleteSet[item.hook.ID] = struct{}{}
		}
	}
	for id := range deleteSet {
		plan.DeleteIDs = append(plan.DeleteIDs, id)
	}
	sort.Slice(plan.DeleteIDs, func(i, j int) bool { return plan.DeleteIDs[i] < plan.DeleteIDs[j] })
	return plan
}

func (s *Server) loadSiteWebhookRecord(siteID int) (siteWebhookRecord, error) {
	var record siteWebhookRecord
	err := database.DB.QueryRow(`
		SELECT id, COALESCE(repository, ''), COALESCE(github_account_id, 0),
		       COALESCE(push_to_deploy, 0), COALESCE(github_webhook_id, 0),
		       COALESCE(github_webhook_url, '')
		FROM sites WHERE id = ?`, siteID).Scan(
		&record.SiteID, &record.Repository, &record.AccountID, &record.Enabled, &record.HookID, &record.HookURL,
	)
	record.Repository = strings.TrimSpace(record.Repository)
	return record, err
}

func loadGitHubToken(accountID int) (string, error) {
	var token string
	if accountID > 0 {
		_ = database.DB.QueryRow("SELECT token FROM github_accounts WHERE id = ?", accountID).Scan(&token)
	} else {
		_ = database.DB.QueryRow("SELECT token FROM github_accounts ORDER BY id ASC LIMIT 1").Scan(&token)
	}
	token = config.Decrypt(token)
	if token == "" {
		return "", fmt.Errorf("connected GitHub account is unavailable")
	}
	return token, nil
}

func loadWebhookCredentials(accountID int) (string, string, error) {
	token, err := loadGitHubToken(accountID)
	if err != nil {
		return "", "", err
	}
	var secret string
	if err := database.DB.QueryRow("SELECT webhook_secret FROM users ORDER BY id ASC LIMIT 1").Scan(&secret); err != nil {
		return "", "", fmt.Errorf("load webhook secret: %w", err)
	}
	secret = config.Decrypt(secret)
	if secret == "" {
		generated, err := safeinput.GenerateSecretHex(32)
		if err != nil {
			return "", "", fmt.Errorf("generate webhook secret: %w", err)
		}
		encrypted, err := config.EncryptSecret(generated)
		if err != nil {
			return "", "", fmt.Errorf("encrypt webhook secret: %w", err)
		}
		if _, err := database.DB.Exec("UPDATE users SET webhook_secret = ?", encrypted); err != nil {
			return "", "", fmt.Errorf("save webhook secret: %w", err)
		}
		secret = generated
	}
	return token, secret, nil
}

func loadObservedWebhookIDs(repository string) (map[int64]bool, error) {
	rows, err := database.DB.Query("SELECT hook_id FROM github_webhook_observations WHERE repository = ?", repository)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	observed := make(map[int64]bool)
	for rows.Next() {
		var hookID int64
		if err := rows.Scan(&hookID); err != nil {
			return nil, err
		}
		observed[hookID] = true
	}
	return observed, rows.Err()
}

func (s *Server) ensureSiteGitHubWebhook(ctx context.Context, siteID int, preferredURL string) error {
	s.githubWebhookMu.Lock()
	defer s.githubWebhookMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	record, err := s.loadSiteWebhookRecord(siteID)
	if err != nil {
		return err
	}
	if !record.Enabled || record.Repository == "" || !safeinput.ValidateRepoFullName(record.Repository) {
		return nil
	}
	token, secret, err := loadWebhookCredentials(record.AccountID)
	if err != nil {
		return err
	}
	provider := s.githubWebhookProvider(token)
	hooks, err := provider.ListWebhooks(record.Repository)
	if err != nil {
		return err
	}
	panelConfig, panelErr := database.GetPanelDomainConfig()
	if panelErr != nil {
		return panelErr
	}
	if storedCallback := canonicalWebhookURL(record.HookURL); storedCallback != "" {
		preferredURL = storedCallback
	} else if requestedCallback := canonicalWebhookURL(preferredURL); requestedCallback != "" {
		preferredURL = requestedCallback
	} else if panelConfig.Domain != "" {
		preferredURL = webhookURLForHost(panelConfig.Domain)
	} else {
		preferredURL = ""
	}
	observedIDs, err := loadObservedWebhookIDs(record.Repository)
	if err != nil {
		return fmt.Errorf("load observed webhooks: %w", err)
	}
	plan := planWebhookReconciliation(hooks, record.HookID, record.HookURL, panelConfig.Domain, preferredURL, observedIDs, localWebhookEndpoints())

	var retainedID int64
	retainedURL := ""
	if plan.Keep != nil {
		retainedID = plan.Keep.ID
		retainedURL = plan.Keep.Config.URL
		pushOnly := len(plan.Keep.Events) == 1 && plan.Keep.Events[0] == "push"
		needsSecretRefresh := record.HookID != retainedID && !observedIDs[retainedID]
		if needsSecretRefresh || !plan.Keep.Active || !pushOnly || plan.Keep.Config.ContentType != "json" {
			if err := provider.UpdateWebhook(record.Repository, retainedID, retainedURL, secret); err != nil {
				return fmt.Errorf("refresh retained webhook: %w", err)
			}
		}
	} else if plan.CreateURL != "" {
		retainedURL = plan.CreateURL
		retainedID, err = provider.RegisterWebhook(record.Repository, retainedURL, secret)
		if err != nil {
			return fmt.Errorf("register webhook: %w", err)
		}
		if retainedID == 0 {
			return fmt.Errorf("register webhook: GitHub returned an empty webhook ID")
		}
		if err := provider.UpdateWebhook(record.Repository, retainedID, retainedURL, secret); err != nil {
			return fmt.Errorf("activate registered webhook: %w", err)
		}
	}

	// Revalidate after GitHub calls. Normal site mutations take the same mutex,
	// while this check also protects against deletion or out-of-process database
	// changes that happen during a slow provider request.
	var activeSites int
	if err := database.DB.QueryRow(`
		SELECT COUNT(*) FROM sites
		WHERE repository = ? AND push_to_deploy = 1 AND COALESCE(deletion_status, '') = ''`, record.Repository).Scan(&activeSites); err != nil {
		return fmt.Errorf("revalidate webhook users: %w", err)
	}
	if activeSites == 0 && retainedID > 0 {
		if err := provider.RemoveWebhook(record.Repository, retainedID); err != nil {
			return fmt.Errorf("remove webhook after site state changed: %w", err)
		}
		_, _ = database.DB.Exec("DELETE FROM github_webhook_observations WHERE repository = ? AND hook_id = ?", record.Repository, retainedID)
		_, _ = database.DB.Exec("UPDATE sites SET github_webhook_id = 0, github_webhook_url = '' WHERE repository = ?", record.Repository)
		return nil
	}

	if retainedID == 0 {
		for _, hookID := range plan.DeleteIDs {
			if err := provider.RemoveWebhook(record.Repository, hookID); err != nil {
				return fmt.Errorf("remove obsolete webhook %d: %w", hookID, err)
			}
			_, _ = database.DB.Exec("DELETE FROM github_webhook_observations WHERE repository = ? AND hook_id = ?", record.Repository, hookID)
		}
		_, _ = database.DB.Exec("UPDATE sites SET github_webhook_id = 0, github_webhook_url = '' WHERE id = ?", siteID)
		return fmt.Errorf("no public HTTPS callback is available; connect a panel domain or enable Push to Deploy from the public server address")
	}

	removed := 0
	for _, hookID := range plan.DeleteIDs {
		if hookID == retainedID {
			continue
		}
		if err := provider.RemoveWebhook(record.Repository, hookID); err != nil {
			log.Printf("Warning: retained GitHub webhook %d for site %d but could not remove duplicate %d: %v", retainedID, siteID, hookID, err)
			continue
		}
		_, _ = database.DB.Exec("DELETE FROM github_webhook_observations WHERE repository = ? AND hook_id = ?", record.Repository, hookID)
		removed++
	}
	if _, err := database.DB.Exec(`
		UPDATE sites SET github_webhook_id = ?, github_webhook_url = ?
		WHERE repository = ? AND push_to_deploy = 1`, retainedID, retainedURL, record.Repository); err != nil {
		return fmt.Errorf("store retained webhook: %w", err)
	}
	if removed > 0 {
		LogActivity(siteID, "source_control", fmt.Sprintf("Removed %d duplicate or obsolete GitHub webhook(s)", removed))
	}
	log.Printf("GitHub webhook reconciled for site %d (%s): retained=%d removed=%d", siteID, record.Repository, retainedID, removed)
	return nil
}

func (s *Server) removeSiteGitHubWebhook(ctx context.Context, record siteWebhookRecord, siteDeletion bool) error {
	s.githubWebhookMu.Lock()
	defer s.githubWebhookMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if current, err := s.loadSiteWebhookRecord(record.SiteID); err == nil && current.Repository == record.Repository {
		record.AccountID = current.AccountID
		if current.HookID > 0 {
			record.HookID = current.HookID
		}
		if current.HookURL != "" {
			record.HookURL = current.HookURL
		}
	}
	if record.Repository == "" || !safeinput.ValidateRepoFullName(record.Repository) {
		_, _ = database.DB.Exec("UPDATE sites SET github_webhook_id = 0, github_webhook_url = '' WHERE id = ?", record.SiteID)
		return nil
	}
	if !siteDeletion {
		var currentRepository string
		var currentEnabled bool
		err := database.DB.QueryRow("SELECT COALESCE(repository, ''), COALESCE(push_to_deploy, 0) FROM sites WHERE id = ?", record.SiteID).Scan(&currentRepository, &currentEnabled)
		if err == nil && currentEnabled && strings.TrimSpace(currentRepository) == record.Repository {
			return nil
		}
	}
	var otherEnabled int
	if err := database.DB.QueryRow(`
		SELECT COUNT(*) FROM sites
		WHERE id != ? AND repository = ? AND push_to_deploy = 1`, record.SiteID, record.Repository).Scan(&otherEnabled); err != nil {
		return err
	}
	if otherEnabled > 0 {
		_, err := database.DB.Exec("UPDATE sites SET github_webhook_id = 0, github_webhook_url = '' WHERE id = ?", record.SiteID)
		return err
	}
	token, err := loadGitHubToken(record.AccountID)
	if err != nil {
		return err
	}
	provider := s.githubWebhookProvider(token)
	hooks, err := provider.ListWebhooks(record.Repository)
	if err != nil {
		return err
	}
	panelConfig, _ := database.GetPanelDomainConfig()
	observedIDs, err := loadObservedWebhookIDs(record.Repository)
	if err != nil {
		return err
	}
	localEndpoints := localWebhookEndpoints()
	panelURL := webhookURLForHost(panelConfig.Domain)
	deleteSet := make(map[int64]struct{})
	for _, hook := range hooks {
		parsed, ok := parseFluxoWebhookURL(hook.Config.URL)
		if !ok {
			continue
		}
		matchesPanel := panelURL != "" && sameWebhookURL(hook.Config.URL, panelURL)
		owned := hook.ID == record.HookID || sameWebhookURL(hook.Config.URL, record.HookURL) || matchesPanel ||
			observedIDs[hook.ID] || localEndpoints[webhookEndpointKey(parsed)]
		if owned {
			deleteSet[hook.ID] = struct{}{}
		}
	}
	if record.HookID > 0 {
		deleteSet[record.HookID] = struct{}{}
	}
	for hookID := range deleteSet {
		if err := provider.RemoveWebhook(record.Repository, hookID); err != nil {
			return fmt.Errorf("remove webhook %d: %w", hookID, err)
		}
		_, _ = database.DB.Exec("DELETE FROM github_webhook_observations WHERE repository = ? AND hook_id = ?", record.Repository, hookID)
	}
	_, err = database.DB.Exec("UPDATE sites SET github_webhook_id = 0, github_webhook_url = '' WHERE id = ?", record.SiteID)
	return err
}

func (s *Server) reconcileRepositoryGitHubWebhook(ctx context.Context, repository string) {
	repository = strings.TrimSpace(repository)
	if !safeinput.ValidateRepoFullName(repository) {
		return
	}
	rows, err := database.DB.Query(`
		SELECT id FROM sites
		WHERE repository = ? AND push_to_deploy = 1 AND COALESCE(deletion_status, '') = ''
		ORDER BY id`, repository)
	if err != nil {
		log.Printf("Warning: failed to find a site for observed GitHub webhook %s: %v", repository, err)
		return
	}
	var siteIDs []int
	for rows.Next() {
		var siteID int
		if rows.Scan(&siteID) == nil {
			siteIDs = append(siteIDs, siteID)
		}
	}
	rows.Close()
	for _, siteID := range siteIDs {
		if ctx.Err() != nil {
			return
		}
		if err := s.ensureSiteGitHubWebhook(ctx, siteID, ""); err == nil {
			return
		} else {
			log.Printf("Warning: failed to reconcile observed GitHub webhook for site %d: %v", siteID, err)
		}
	}
}

func (s *Server) reconcileGitHubWebhooks(ctx context.Context) {
	rows, err := database.DB.Query(`
		SELECT id, repository FROM sites
		WHERE push_to_deploy = 1 AND TRIM(COALESCE(repository, '')) != ''
		ORDER BY id`)
	if err != nil {
		log.Printf("Warning: failed to enumerate GitHub webhooks for reconciliation: %v", err)
		return
	}
	type activeSite struct {
		id         int
		repository string
	}
	var activeSites []activeSite
	for rows.Next() {
		var site activeSite
		if rows.Scan(&site.id, &site.repository) == nil {
			activeSites = append(activeSites, site)
		}
	}
	rows.Close()
	reconciledRepositories := make(map[string]bool)
	for _, site := range activeSites {
		if ctx.Err() != nil {
			return
		}
		if reconciledRepositories[site.repository] {
			continue
		}
		if err := s.ensureSiteGitHubWebhook(ctx, site.id, ""); err != nil {
			log.Printf("Warning: failed to reconcile GitHub webhook for site %d: %v", site.id, err)
		} else {
			reconciledRepositories[site.repository] = true
		}
	}

	disabledRows, err := database.DB.Query(`
		SELECT id FROM sites
		WHERE push_to_deploy = 0
		  AND (COALESCE(github_webhook_id, 0) > 0 OR TRIM(COALESCE(github_webhook_url, '')) != '')`)
	if err != nil {
		return
	}
	var disabledSiteIDs []int
	for disabledRows.Next() {
		var siteID int
		if disabledRows.Scan(&siteID) == nil {
			disabledSiteIDs = append(disabledSiteIDs, siteID)
		}
	}
	disabledRows.Close()
	for _, siteID := range disabledSiteIDs {
		record, err := s.loadSiteWebhookRecord(siteID)
		if err == nil {
			if err := s.removeSiteGitHubWebhook(ctx, record, false); err != nil {
				log.Printf("Warning: failed to remove disabled GitHub webhook for site %d: %v", siteID, err)
			}
		}
	}
}
