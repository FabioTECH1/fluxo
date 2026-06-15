// Package server implements the Fluxo HTTP server: route registration,
// JWT authentication middleware, REST API handlers, WebSocket hub, and
// SPA fallback for the embedded Vue 3 frontend.
package server

import (
	"encoding/json"
	"log"
	"net/http"

	"fluxo/ui"
)

// Server wraps Go 1.22+ enhanced ServeMux with method + path pattern routing.
// All API routes are registered in routes() and wrapped by AuthMiddleware in
// ServeHTTP before dispatch.
type Server struct {
	mux *http.ServeMux
}

// Version is set from main at startup via ldflags or defaults to "dev".
var Version = "dev"

// NewServer creates a fully configured HTTP server with all routes registered.
func NewServer() *Server {
	s := &Server{
		mux: http.NewServeMux(),
	}
	s.routes()
	return s
}

// routes registers all HTTP endpoints using Go 1.22 method + path pattern syntax
// (e.g. "GET /api/v1/sites/{id}"). API routes are prefixed with /api/v1/.
// Non-API paths are served by the embedded Vue SPA with History API fallback.
func (s *Server) routes() {
	// Authentication
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin())
	s.mux.HandleFunc("GET /api/v1/auth/bootstrap", s.handleBootstrapStatus())

	// Unauthenticated GitHub webhook
	s.mux.HandleFunc("POST /api/v1/github/webhook", s.handleGitHubWebhook())

	// Health check (unauthenticated by middleware bypass)
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth())
	s.mux.HandleFunc("GET /api/v1/version", s.handleVersion())

	// Sites CRUD
	s.mux.HandleFunc("GET /api/v1/sites", s.handleListSites())
	s.mux.HandleFunc("POST /api/v1/sites", s.handleCreateSite())
	s.mux.HandleFunc("GET /api/v1/sites/{id}", s.handleGetSite())
	s.mux.HandleFunc("PUT /api/v1/sites/{id}", s.handleUpdateSite())
	s.mux.HandleFunc("DELETE /api/v1/sites/{id}", s.handleDeleteSite())

	// Server runtime
	s.mux.HandleFunc("GET /api/v1/server/php", s.handleGetPHPVersions())
	s.mux.HandleFunc("GET /api/v1/settings", s.handleGetSettings())
	s.mux.HandleFunc("POST /api/v1/settings", s.handleUpdateSettings())
	s.mux.HandleFunc("POST /api/v1/settings/password", s.handleUpdatePassword())
	s.mux.HandleFunc("GET /api/v1/settings/bootstrap-credentials", s.handleGetBootstrapCredentials())
	s.mux.HandleFunc("POST /api/v1/settings/bootstrap-credentials/copied", s.handleMarkCredentialsCopied())

	// GitHub integration
	s.mux.HandleFunc("GET /api/v1/github/repos", s.handleGetGitHubRepos())
	s.mux.HandleFunc("GET /api/v1/github/branches", s.handleGetGitHubBranches())

	// Per-site environment
	s.mux.HandleFunc("GET /api/v1/sites/{id}/env", s.handleGetEnv())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/env", s.handleUpdateEnv())

	// Daemons (systemd services)
	s.mux.HandleFunc("GET /api/v1/sites/{id}/daemons", s.handleListDaemons())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/daemons", s.handleCreateDaemon())
	s.mux.HandleFunc("DELETE /api/v1/sites/{id}/daemons/{daemon_id}", s.handleDeleteDaemon())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/daemons/{daemon_id}/restart", s.handleRestartDaemon())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/daemons/{daemon_id}/start", s.handleStartDaemon())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/daemons/{daemon_id}/stop", s.handleStopDaemon())
	s.mux.HandleFunc("GET /api/v1/sites/{id}/daemons/{daemon_id}/logs", s.handleGetDaemonLogs())

	// Cron jobs
	s.mux.HandleFunc("GET /api/v1/sites/{id}/crons", s.handleListCrons())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/crons", s.handleCreateCron())
	s.mux.HandleFunc("DELETE /api/v1/sites/{id}/crons/{cron_id}", s.handleDeleteCron())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/crons/{cron_id}/run", s.handleRunCron())
	s.mux.HandleFunc("GET /api/v1/sites/{id}/crons/{cron_id}/logs", s.handleGetCronLogs())

	// SSL
	s.mux.HandleFunc("POST /api/v1/sites/{id}/ssl/letsencrypt", s.handleLetsEncrypt())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/ssl/custom", s.handleCustomSSL())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/ssl/activate", s.handleActivateSSL())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/ssl/deactivate", s.handleDeactivateSSL())

	// Site databases
	s.mux.HandleFunc("GET /api/v1/sites/{id}/databases", s.handleListDatabases())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/databases", s.handleCreateDatabase())
	s.mux.HandleFunc("DELETE /api/v1/sites/{id}/databases/{db_id}", s.handleDeleteDatabase())

	// Deployments
	s.mux.HandleFunc("GET /api/v1/sites/{id}/deployments", s.handleListDeployments())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/deploy", s.handleTriggerDeployment())

	// Web terminal commands
	s.mux.HandleFunc("GET /api/v1/sites/{id}/commands", s.handleListCommands())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/commands", s.handleExecuteCommand())

	// Features (Laravel-specific toggles)
	s.mux.HandleFunc("GET /api/v1/sites/{id}/features", s.handleGetFeatures())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/features/scheduler/enable", s.handleEnableScheduler())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/features/scheduler/disable", s.handleDisableScheduler())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/features/nightwatch/enable", s.handleEnableNightwatch())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/features/nightwatch/disable", s.handleDisableNightwatch())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/features/maintenance/enable", s.handleEnableMaintenance())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/features/maintenance/disable", s.handleDisableMaintenance())

	// Site logs & domains
	s.mux.HandleFunc("GET /api/v1/sites/{id}/logs/list", s.handleSiteLogSources())
	s.mux.HandleFunc("GET /api/v1/sites/{id}/domains", s.handleListDomains())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/domains", s.handleAddDomain())
	s.mux.HandleFunc("DELETE /api/v1/sites/{id}/domains/{domain_id}", s.handleDeleteDomain())

	// WebSocket for real-time deploy log streaming (bypasses auth)
	s.mux.HandleFunc("GET /api/v1/ws", s.handleWebSocket())

	// Database engines
	s.mux.HandleFunc("GET /api/v1/server/engines", s.handleGetEngines())
	s.mux.HandleFunc("POST /api/v1/server/engines/postgres/install", s.handleInstallPostgres())
	s.mux.HandleFunc("POST /api/v1/server/engines/mysql/install", s.handleInstallMySQL())
	s.mux.HandleFunc("POST /api/v1/server/engines/redis/install", s.handleInstallRedis())

	// System metrics & logs
	s.mux.HandleFunc("GET /api/v1/system/metrics", s.handleGetMetrics())
	s.mux.HandleFunc("GET /api/v1/system/logs", s.handleGetLogs())
	s.mux.HandleFunc("GET /api/v1/system/logs/list", s.handleGetLogList())
	s.mux.HandleFunc("POST /api/v1/system/logs/clear", s.handleClearLog())
	s.mux.HandleFunc("GET /api/v1/system/logs/download", s.handleDownloadLog())
	s.mux.HandleFunc("GET /api/v1/system/activity", s.handleGetActivity())

	// Global database management
	s.mux.HandleFunc("GET /api/v1/databases", s.handleListAllDatabases())
	s.mux.HandleFunc("POST /api/v1/databases", s.handleCreateGlobalDatabase())
	s.mux.HandleFunc("GET /api/v1/databases/sizes", s.handleGetDatabaseSizes())
	s.mux.HandleFunc("GET /api/v1/databases/users", s.handleGetDatabaseUsers())
	s.mux.HandleFunc("POST /api/v1/databases/users", s.handleCreateDatabaseUser())
	s.mux.HandleFunc("GET /api/v1/databases/users/grants", s.handleGetUserGrants())
	s.mux.HandleFunc("POST /api/v1/databases/users/grants", s.handleUpdateUserGrants())
	s.mux.HandleFunc("DELETE /api/v1/databases/users", s.handleDeleteDatabaseUser())
	s.mux.HandleFunc("DELETE /api/v1/databases/{db_id}", s.handleDeleteDatabase())

	// PHP runtime management
	s.mux.HandleFunc("GET /api/v1/server/php/settings", s.handleGetPHPSettings())
	s.mux.HandleFunc("POST /api/v1/server/php/settings", s.handleUpdatePHPSettings())
	s.mux.HandleFunc("GET /api/v1/server/php/versions/available", s.handleGetPHPAvailableVersions())
	s.mux.HandleFunc("POST /api/v1/server/php/versions/install", s.handleInstallPHPVersion())
	s.mux.HandleFunc("POST /api/v1/server/php/versions/remove", s.handleRemovePHPVersion())
	s.mux.HandleFunc("POST /api/v1/server/php/versions/default", s.handleSetDefaultPHP())
	s.mux.HandleFunc("GET /api/v1/server/php/cli-default", s.handleGetCLIDefaultPHP())

	// Nginx, Node.js, MySQL runtime info
	s.mux.HandleFunc("GET /api/v1/server/nginx/info", s.handleGetNginxInfo())
	s.mux.HandleFunc("POST /api/v1/server/nginx/restart", s.handleRestartNginx())
	s.mux.HandleFunc("POST /api/v1/server/php/restart/{version}", s.handleRestartPHP())
	s.mux.HandleFunc("POST /api/v1/server/php/start/{version}", s.handleStartPHP())
	s.mux.HandleFunc("POST /api/v1/server/php/stop/{version}", s.handleStopPHP())
	s.mux.HandleFunc("GET /api/v1/server/node/info", s.handleGetNodeInfo())
	s.mux.HandleFunc("POST /api/v1/server/node/restart", s.handleRestartNode())
	s.mux.HandleFunc("POST /api/v1/server/node/install", s.handleInstallNode())
	s.mux.HandleFunc("POST /api/v1/server/node/remove", s.handleRemoveNode())
	s.mux.HandleFunc("GET /api/v1/server/mysql/info", s.handleGetMySQLInfo())
	s.mux.HandleFunc("GET /api/v1/server/redis/info", s.handleGetRedisInfo())
	s.mux.HandleFunc("GET /api/v1/server/postgres/info", s.handleGetPostgresInfo())
	s.mux.HandleFunc("POST /api/v1/server/mysql/restart", s.systemdAction("mariadb", "restart", "MySQL"))
	s.mux.HandleFunc("POST /api/v1/server/mysql/start", s.systemdAction("mariadb", "start", "MySQL"))
	s.mux.HandleFunc("POST /api/v1/server/mysql/stop", s.systemdAction("mariadb", "stop", "MySQL"))
	s.mux.HandleFunc("POST /api/v1/server/redis/restart", s.systemdAction("redis-server", "restart", "Redis"))
	s.mux.HandleFunc("POST /api/v1/server/redis/start", s.systemdAction("redis-server", "start", "Redis"))
	s.mux.HandleFunc("POST /api/v1/server/redis/stop", s.systemdAction("redis-server", "stop", "Redis"))
	s.mux.HandleFunc("POST /api/v1/server/postgres/restart", s.systemdAction("postgresql", "restart", "PostgreSQL"))
	s.mux.HandleFunc("POST /api/v1/server/postgres/start", s.systemdAction("postgresql", "start", "PostgreSQL"))
	s.mux.HandleFunc("POST /api/v1/server/postgres/stop", s.systemdAction("postgresql", "stop", "PostgreSQL"))

	// Global daemons & crons
	s.mux.HandleFunc("GET /api/v1/daemons", s.handleListAllDaemons())
	s.mux.HandleFunc("POST /api/v1/daemons", s.handleCreateGlobalDaemon())
	s.mux.HandleFunc("DELETE /api/v1/daemons/{daemon_id}", s.handleDeleteDaemon())
	s.mux.HandleFunc("POST /api/v1/daemons/{daemon_id}/restart", s.handleRestartDaemon())
	s.mux.HandleFunc("POST /api/v1/daemons/{daemon_id}/start", s.handleStartDaemon())
	s.mux.HandleFunc("POST /api/v1/daemons/{daemon_id}/stop", s.handleStopDaemon())
	s.mux.HandleFunc("GET /api/v1/daemons/{daemon_id}/logs", s.handleGetDaemonLogs())
	s.mux.HandleFunc("GET /api/v1/crons", s.handleListAllCrons())
	s.mux.HandleFunc("POST /api/v1/crons", s.handleCreateGlobalCron())
	s.mux.HandleFunc("DELETE /api/v1/crons/{cron_id}", s.handleDeleteCron())
	s.mux.HandleFunc("POST /api/v1/crons/{cron_id}/run", s.handleRunCron())
	s.mux.HandleFunc("GET /api/v1/crons/{cron_id}/logs", s.handleGetCronLogs())

	// SSH keys
	s.mux.HandleFunc("GET /api/v1/ssh-keys", s.handleListSSHKeys())
	s.mux.HandleFunc("POST /api/v1/ssh-keys", s.handleCreateSSHKey())
	s.mux.HandleFunc("DELETE /api/v1/ssh-keys/{id}", s.handleDeleteSSHKey())

	// Firewall rules
	s.mux.HandleFunc("GET /api/v1/firewall", s.handleListFirewallRules())
	s.mux.HandleFunc("POST /api/v1/firewall", s.handleCreateFirewallRule())
	s.mux.HandleFunc("DELETE /api/v1/firewall/{id}", s.handleDeleteFirewallRule())

	// SPA catch-all: serves the embedded Vue 3 frontend for all non-API routes.
	// Uses History API fallback: if the path doesn't match a static file,
	// index.html is returned so Vue Router can handle the route client-side.
	s.mux.HandleFunc("/", s.handleSPA())
}

// handleHealth is a minimal health-check endpoint (unauthenticated).
func (s *Server) handleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// handleVersion returns the current binary version (unauthenticated).
func (s *Server) handleVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"version": Version})
	}
}

// handleSPA serves the embedded Vue 3 SPA with History API fallback.
// If a requested file exists in the embedded dist/ filesystem it is served
// directly; otherwise index.html is returned so Vue Router handles routing.
func (s *Server) handleSPA() http.HandlerFunc {
	spaHandler := ui.DistHandler()
	return func(w http.ResponseWriter, r *http.Request) {
		spaHandler.ServeHTTP(w, r)
	}
}

// ServeHTTP is the main HTTP entrypoint. Every request is logged, then
// passed through AuthMiddleware before reaching the route handler.
// The middleware skips auth for login, WebSocket, and non-API paths.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s %s", r.Method, r.URL.Path)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	if r.URL.Path != "/api/v1/ws" {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:; img-src 'self' data:; font-src 'self'")
	}
	AuthMiddleware(s.mux).ServeHTTP(w, r)
}
