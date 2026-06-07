package server

import (
	"encoding/json"
	"log"
	"net/http"

	"fluxo/ui"
)

type Server struct {
	mux *http.ServeMux
}

func NewServer() *Server {
	s := &Server{
		mux: http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	// API v1 Routes
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin())

	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth())
	s.mux.HandleFunc("GET /api/v1/sites", s.handleListSites())
	s.mux.HandleFunc("POST /api/v1/sites", s.handleCreateSite())
	s.mux.HandleFunc("DELETE /api/v1/sites/{id}", s.handleDeleteSite())
	s.mux.HandleFunc("GET /api/v1/server/php", s.handleGetPHPVersions())
	s.mux.HandleFunc("GET /api/v1/settings", s.handleGetSettings())
	s.mux.HandleFunc("POST /api/v1/settings", s.handleUpdateSettings())
	s.mux.HandleFunc("POST /api/v1/settings/password", s.handleUpdatePassword())
	s.mux.HandleFunc("GET /api/v1/github/repos", s.handleGetGitHubRepos())

	s.mux.HandleFunc("GET /api/v1/sites/{id}/env", s.handleGetEnv())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/env", s.handleUpdateEnv())

	s.mux.HandleFunc("GET /api/v1/sites/{id}/daemons", s.handleListDaemons())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/daemons", s.handleCreateDaemon())
	s.mux.HandleFunc("DELETE /api/v1/sites/{id}/daemons/{daemon_id}", s.handleDeleteDaemon())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/daemons/{daemon_id}/restart", s.handleRestartDaemon())

	s.mux.HandleFunc("GET /api/v1/sites/{id}/crons", s.handleListCrons())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/crons", s.handleCreateCron())
	s.mux.HandleFunc("DELETE /api/v1/sites/{id}/crons/{cron_id}", s.handleDeleteCron())

	s.mux.HandleFunc("POST /api/v1/sites/{id}/ssl/letsencrypt", s.handleLetsEncrypt())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/ssl/custom", s.handleCustomSSL())

	s.mux.HandleFunc("GET /api/v1/sites/{id}/databases", s.handleListDatabases())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/databases", s.handleCreateDatabase())
	s.mux.HandleFunc("DELETE /api/v1/sites/{id}/databases/{db_id}", s.handleDeleteDatabase())

	s.mux.HandleFunc("GET /api/v1/sites/{id}/deployments", s.handleListDeployments())
	s.mux.HandleFunc("POST /api/v1/sites/{id}/deploy", s.handleTriggerDeployment())
	s.mux.HandleFunc("GET /api/v1/ws", s.handleWebSocket())

	s.mux.HandleFunc("GET /api/v1/server/engines", s.handleGetEngines())
	s.mux.HandleFunc("POST /api/v1/server/engines/postgres/install", s.handleInstallPostgres())
	s.mux.HandleFunc("GET /api/v1/system/metrics", s.handleGetMetrics())
	s.mux.HandleFunc("GET /api/v1/system/logs", s.handleGetLogs())
	s.mux.HandleFunc("GET /api/v1/system/logs/list", s.handleGetLogList())
	s.mux.HandleFunc("POST /api/v1/system/logs/clear", s.handleClearLog())
	s.mux.HandleFunc("GET /api/v1/system/logs/download", s.handleDownloadLog())
	s.mux.HandleFunc("GET /api/v1/system/activity", s.handleGetActivity())
	s.mux.HandleFunc("GET /api/v1/databases", s.handleListAllDatabases())
	s.mux.HandleFunc("POST /api/v1/databases", s.handleCreateGlobalDatabase())
	s.mux.HandleFunc("GET /api/v1/databases/sizes", s.handleGetDatabaseSizes())
	s.mux.HandleFunc("GET /api/v1/databases/users", s.handleGetDatabaseUsers())
	s.mux.HandleFunc("POST /api/v1/databases/users", s.handleCreateDatabaseUser())
	s.mux.HandleFunc("GET /api/v1/databases/users/grants", s.handleGetUserGrants())
	s.mux.HandleFunc("POST /api/v1/databases/users/grants", s.handleUpdateUserGrants())
	s.mux.HandleFunc("DELETE /api/v1/databases/users", s.handleDeleteDatabaseUser())
	s.mux.HandleFunc("DELETE /api/v1/databases/{db_id}", s.handleDeleteDatabase())

	// Runtime Routes
	s.mux.HandleFunc("GET /api/v1/server/php/settings", s.handleGetPHPSettings())
	s.mux.HandleFunc("POST /api/v1/server/php/settings", s.handleUpdatePHPSettings())
	s.mux.HandleFunc("GET /api/v1/server/php/versions/available", s.handleGetPHPAvailableVersions())
	s.mux.HandleFunc("POST /api/v1/server/php/versions/install", s.handleInstallPHPVersion())
	s.mux.HandleFunc("POST /api/v1/server/php/versions/remove", s.handleRemovePHPVersion())
	s.mux.HandleFunc("POST /api/v1/server/php/versions/default", s.handleSetDefaultPHP())
	s.mux.HandleFunc("GET /api/v1/server/nginx/info", s.handleGetNginxInfo())
	s.mux.HandleFunc("POST /api/v1/server/nginx/restart", s.handleRestartNginx())
	s.mux.HandleFunc("POST /api/v1/server/php/restart/{version}", s.handleRestartPHP())
	s.mux.HandleFunc("GET /api/v1/server/node/info", s.handleGetNodeInfo())
	s.mux.HandleFunc("POST /api/v1/server/node/restart", s.handleRestartNode())
	s.mux.HandleFunc("GET /api/v1/daemons", s.handleListAllDaemons())
	s.mux.HandleFunc("POST /api/v1/daemons", s.handleCreateGlobalDaemon())
	s.mux.HandleFunc("DELETE /api/v1/daemons/{daemon_id}", s.handleDeleteDaemon())
	s.mux.HandleFunc("POST /api/v1/daemons/{daemon_id}/restart", s.handleRestartDaemon())
	s.mux.HandleFunc("GET /api/v1/crons", s.handleListAllCrons())
	s.mux.HandleFunc("POST /api/v1/crons", s.handleCreateGlobalCron())
	s.mux.HandleFunc("DELETE /api/v1/crons/{cron_id}", s.handleDeleteCron())

	// SSH Keys Routes
	s.mux.HandleFunc("GET /api/v1/ssh-keys", s.handleListSSHKeys())
	s.mux.HandleFunc("POST /api/v1/ssh-keys", s.handleCreateSSHKey())
	s.mux.HandleFunc("DELETE /api/v1/ssh-keys/{id}", s.handleDeleteSSHKey())

	// Firewall Routes
	s.mux.HandleFunc("GET /api/v1/firewall", s.handleListFirewallRules())
	s.mux.HandleFunc("POST /api/v1/firewall", s.handleCreateFirewallRule())
	s.mux.HandleFunc("DELETE /api/v1/firewall/{id}", s.handleDeleteFirewallRule())

	// Fallback to SPA for all other routes
	s.mux.HandleFunc("/", s.handleSPA())
}

func (s *Server) handleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

func (s *Server) handleSPA() http.HandlerFunc {
	spaHandler := ui.DistHandler()
	return func(w http.ResponseWriter, r *http.Request) {
		spaHandler.ServeHTTP(w, r)
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Simple logging middleware
	log.Printf("%s %s", r.Method, r.URL.Path)

	AuthMiddleware(s.mux).ServeHTTP(w, r)
}
