// Package database manages the SQLite database connection, schema, and migrations.
package database

import (
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/services/git"

	_ "modernc.org/sqlite" // CGo-free SQLite driver
)

// DB is the global database handle, initialized by InitDB at startup.
var DB *sql.DB

// InitDB opens the SQLite database, pings it, applies the schema, and runs incremental migrations.
func InitDB(filepath string) error {
	var err error
	DB, err = sql.Open("sqlite", filepath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	DB.Exec("PRAGMA busy_timeout = 5000")
	DB.Exec("PRAGMA journal_mode = WAL")

	// Base schema — all tables created with IF NOT EXISTS.
	schema := `
	CREATE TABLE IF NOT EXISTS sites (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		domain TEXT NOT NULL UNIQUE,
		path TEXT NOT NULL,
		repository TEXT,
		branch TEXT,
		php_version TEXT,
		app_type TEXT DEFAULT 'php',
		app_port INTEGER,
		node_preset TEXT DEFAULT '',
		node_mode TEXT DEFAULT '',
		package_manager TEXT DEFAULT 'npm',
		build_command TEXT DEFAULT '',
		start_command TEXT DEFAULT '',
		static_output_dir TEXT DEFAULT '',
		deployment_strategy TEXT DEFAULT 'standard',
		ssl_provider TEXT DEFAULT 'none',
		ssl_active INTEGER DEFAULT 0,
		web_root TEXT DEFAULT '/public',
		push_to_deploy INTEGER DEFAULT 0,
		deploy_script TEXT DEFAULT '',
		expose_env INTEGER DEFAULT 0,
		db_engine TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS deployments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		commit_hash TEXT,
		commit_message TEXT,
		commit_author TEXT,
		status TEXT NOT NULL,
		output TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(site_id) REFERENCES sites(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS daemons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		name TEXT DEFAULT '',
		command TEXT NOT NULL,
		directory TEXT NOT NULL,
		user TEXT DEFAULT 'fluxo',
		instances INTEGER DEFAULT 1,
		status TEXT DEFAULT 'stopped',
		start_seconds INTEGER DEFAULT 1,
		stop_seconds INTEGER DEFAULT 15,
		stop_signal TEXT DEFAULT 'SIGTERM',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(site_id) REFERENCES sites(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS crons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		name TEXT DEFAULT '',
		expression TEXT NOT NULL,
		command TEXT NOT NULL,
		user TEXT DEFAULT 'fluxo',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(site_id) REFERENCES sites(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS databases (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		engine TEXT NOT NULL,
		name TEXT NOT NULL,
		username TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(engine, name)
	);

	CREATE TABLE IF NOT EXISTS ssh_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		public_key TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS firewall_rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		rule_type TEXT DEFAULT 'allow',
		port TEXT NOT NULL,
		from_ip TEXT DEFAULT 'Any',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS commands (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		command TEXT NOT NULL,
		status TEXT DEFAULT 'success',
		output TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(site_id) REFERENCES sites(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS domain_aliases (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		domain TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(site_id) REFERENCES sites(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		token_hash TEXT NOT NULL,
		github_pat TEXT,
		admin_email TEXT,
		default_php TEXT DEFAULT '8.4',
		fluxo_db_password TEXT DEFAULT '',
		fluxo_sudo_password TEXT DEFAULT '',
		fluxo_mysql_password TEXT DEFAULT '',
		fluxo_postgres_password TEXT DEFAULT '',
		credentials_copied INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS activity (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER DEFAULT 0,
		type TEXT NOT NULL,
		summary TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS certificates (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		domain TEXT NOT NULL,
		provider TEXT NOT NULL,
		cert_path TEXT,
		key_path TEXT,
		active INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(site_id) REFERENCES sites(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS github_cache (
		key TEXT PRIMARY KEY,
		data TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS github_accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		username TEXT NOT NULL DEFAULT '',
		token TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err = DB.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Incremental migrations — each ALTER ADD COLUMN is ignored by SQLite if the column already exists.
	DB.Exec("ALTER TABLE sites ADD COLUMN repository TEXT")
	DB.Exec("ALTER TABLE sites ADD COLUMN branch TEXT")
	DB.Exec("ALTER TABLE sites ADD COLUMN deployment_strategy TEXT DEFAULT 'standard'")
	DB.Exec("ALTER TABLE sites ADD COLUMN app_type TEXT DEFAULT 'php'")
	DB.Exec("ALTER TABLE sites ADD COLUMN app_port INTEGER")
	DB.Exec("ALTER TABLE sites ADD COLUMN node_preset TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE sites ADD COLUMN node_mode TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE sites ADD COLUMN package_manager TEXT DEFAULT 'npm'")
	DB.Exec("ALTER TABLE sites ADD COLUMN build_command TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE sites ADD COLUMN start_command TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE sites ADD COLUMN static_output_dir TEXT DEFAULT ''")
	DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_app_port ON sites (app_port) WHERE app_port > 0")
	DB.Exec("ALTER TABLE sites ADD COLUMN ssl_provider TEXT DEFAULT 'none'")
	DB.Exec("ALTER TABLE sites ADD COLUMN ssl_active INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE sites ADD COLUMN web_root TEXT DEFAULT '/public'")
	DB.Exec("ALTER TABLE sites ADD COLUMN push_to_deploy INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE sites ADD COLUMN deploy_script TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE sites ADD COLUMN expose_env INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE sites ADD COLUMN db_engine TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE deployments ADD COLUMN commit_message TEXT")
	DB.Exec("ALTER TABLE deployments ADD COLUMN commit_author TEXT")
	DB.Exec("ALTER TABLE deployments ADD COLUMN branch TEXT")
	DB.Exec("ALTER TABLE deployments ADD COLUMN trigger_source TEXT DEFAULT 'manual'")
	DB.Exec("ALTER TABLE deployments ADD COLUMN target_commit_hash TEXT")
	DB.Exec("ALTER TABLE users ADD COLUMN github_pat TEXT")
	DB.Exec("ALTER TABLE users ADD COLUMN admin_email TEXT")
	DB.Exec("ALTER TABLE users ADD COLUMN default_php TEXT DEFAULT '8.4'")
	DB.Exec("ALTER TABLE users ADD COLUMN webhook_secret TEXT")
	DB.Exec("ALTER TABLE users ADD COLUMN token_version INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE users ADD COLUMN fluxo_db_password TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE users ADD COLUMN fluxo_sudo_password TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE users ADD COLUMN fluxo_mysql_password TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE users ADD COLUMN fluxo_postgres_password TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE users ADD COLUMN credentials_copied INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE firewall_rules ADD COLUMN rule_type TEXT DEFAULT 'allow'")
	DB.Exec("ALTER TABLE daemons ADD COLUMN name TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE daemons ADD COLUMN user TEXT DEFAULT 'fluxo'")
	DB.Exec("ALTER TABLE daemons ADD COLUMN start_seconds INTEGER DEFAULT 1")
	DB.Exec("ALTER TABLE daemons ADD COLUMN stop_seconds INTEGER DEFAULT 15")
	DB.Exec("ALTER TABLE daemons ADD COLUMN stop_signal TEXT DEFAULT 'SIGTERM'")
	DB.Exec("ALTER TABLE crons ADD COLUMN user TEXT DEFAULT 'fluxo'")
	DB.Exec("ALTER TABLE crons ADD COLUMN name TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE activity ADD COLUMN username TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE activity ADD COLUMN ip_address TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE certificates ADD COLUMN expires_at DATETIME")
	DB.Exec("ALTER TABLE sites ADD COLUMN github_deploy_key_id INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE sites ADD COLUMN github_webhook_id INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE sites ADD COLUMN github_account_id INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE github_accounts ADD COLUMN username TEXT NOT NULL DEFAULT ''")
	DB.Exec("ALTER TABLE users ADD COLUMN pending_new_password_engine TEXT DEFAULT ''")

	// Fix: app_port unique index only applies to ports > 0, not the default 0
	DB.Exec("DROP INDEX IF EXISTS idx_sites_app_port")
	DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_app_port ON sites (app_port) WHERE app_port > 0")

	// Migrate existing SSL data to certificates table
	migrateSSLCertsToTable()

	// Migrate existing fluxo_db_password to engine-specific columns.
	var existingMysqlPass, existingDbPass string
	DB.QueryRow("SELECT fluxo_mysql_password FROM users ORDER BY id ASC LIMIT 1").Scan(&existingMysqlPass)
	if existingMysqlPass == "" {
		DB.QueryRow("SELECT fluxo_db_password FROM users ORDER BY id ASC LIMIT 1").Scan(&existingDbPass)
		if existingDbPass != "" {
			DB.Exec("UPDATE users SET fluxo_mysql_password = ?, fluxo_postgres_password = ? WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)", existingDbPass, existingDbPass)
		}
	}
	DB.Exec(`CREATE TABLE IF NOT EXISTS databases (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		engine TEXT NOT NULL,
		name TEXT NOT NULL,
		username TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(engine, name)
	)`)

	// Migrate: replace old UNIQUE(name) with UNIQUE(engine, name) on databases table
	DB.Exec("PRAGMA foreign_keys = OFF")
	DB.Exec("CREATE TABLE IF NOT EXISTS databases_migrate (id INTEGER PRIMARY KEY AUTOINCREMENT, site_id INTEGER NOT NULL, engine TEXT NOT NULL, name TEXT NOT NULL, username TEXT NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP, UNIQUE(engine, name))")
	DB.Exec("INSERT OR IGNORE INTO databases_migrate (id, site_id, engine, name, username, created_at) SELECT id, site_id, engine, name, username, created_at FROM databases")
	DB.Exec("DROP TABLE databases")
	DB.Exec("ALTER TABLE databases_migrate RENAME TO databases")
	DB.Exec("PRAGMA foreign_keys = ON")

	// Clean up existing deploy scripts — remove lines now handled internally.
	rows, err := DB.Query("SELECT id, deploy_script FROM sites")
	if err == nil {
		defer rows.Close()
		type siteScript struct {
			id     int
			script string
		}
		var updates []siteScript
		for rows.Next() {
			var id int
			var script string
			if err := rows.Scan(&id, &script); err == nil {
				orig := script
				script = strings.ReplaceAll(script, "sudo systemctl reload php$FLUXO_PHP_VERSION-fpm", "")
				script = strings.ReplaceAll(script, "echo \"Deployment complete.\"", "")
				script = strings.TrimSpace(script)
				if script != strings.TrimSpace(orig) {
					updates = append(updates, siteScript{id: id, script: script})
				}
			}
		}
		for _, u := range updates {
			DB.Exec("UPDATE sites SET deploy_script = ? WHERE id = ?", u.script, u.id)
		}
	}

	return nil
}

// EncryptExistingSecrets encrypts any plaintext secrets in the users table that aren't already encrypted.
func EncryptExistingSecrets() {
	var id int
	var pat, mysqlPass, postgresPass, sudoPass, webhookSecret sql.NullString
	err := DB.QueryRow("SELECT id, github_pat, fluxo_mysql_password, fluxo_postgres_password, fluxo_sudo_password, webhook_secret FROM users ORDER BY id ASC LIMIT 1").Scan(&id, &pat, &mysqlPass, &postgresPass, &sudoPass, &webhookSecret)
	if err != nil {
		return
	}

	encPat := pat.String
	if pat.Valid && pat.String != "" && !strings.HasPrefix(pat.String, "enc:") {
		encPat = config.Encrypt(pat.String)
	}

	encMysqlPass := mysqlPass.String
	if mysqlPass.Valid && mysqlPass.String != "" && !strings.HasPrefix(mysqlPass.String, "enc:") {
		encMysqlPass = config.Encrypt(mysqlPass.String)
	}

	encPostgresPass := postgresPass.String
	if postgresPass.Valid && postgresPass.String != "" && !strings.HasPrefix(postgresPass.String, "enc:") {
		encPostgresPass = config.Encrypt(postgresPass.String)
	}

	encSudoPass := sudoPass.String
	if sudoPass.Valid && sudoPass.String != "" && !strings.HasPrefix(sudoPass.String, "enc:") {
		encSudoPass = config.Encrypt(sudoPass.String)
	}

	encWebhookSecret := webhookSecret.String
	if webhookSecret.Valid && webhookSecret.String != "" && !strings.HasPrefix(webhookSecret.String, "enc:") {
		encWebhookSecret = config.Encrypt(webhookSecret.String)
	}

	if encPat != pat.String || encMysqlPass != mysqlPass.String || encPostgresPass != postgresPass.String || encSudoPass != sudoPass.String || encWebhookSecret != webhookSecret.String {
		DB.Exec("UPDATE users SET github_pat = ?, fluxo_mysql_password = ?, fluxo_postgres_password = ?, fluxo_sudo_password = ?, webhook_secret = ? WHERE id = ?", encPat, encMysqlPass, encPostgresPass, encSudoPass, encWebhookSecret, id)
	}

	MigrateLegacyGitHubPAT()
}

// MigrateLegacyGitHubPAT migrates any legacy github_pat in the users table to the github_accounts table.
func MigrateLegacyGitHubPAT() {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM github_accounts").Scan(&count)
	if err != nil || count > 0 {
		return
	}

	var pat string
	err = DB.QueryRow("SELECT github_pat FROM users ORDER BY id ASC LIMIT 1").Scan(&pat)
	if err != nil || pat == "" {
		return
	}

	decrypted := config.Decrypt(pat)
	if decrypted == "" {
		return
	}

	// Insert temporary account, name it "Default GitHub"
	res, err := DB.Exec("INSERT INTO github_accounts (name, token) VALUES (?, ?)", "Default GitHub", pat)
	if err != nil {
		return
	}

	accountID, err := res.LastInsertId()
	if err != nil {
		return
	}

	// Update existing sites with a repository to use this account ID
	DB.Exec("UPDATE sites SET github_account_id = ? WHERE repository IS NOT NULL AND repository != ''", accountID)

	// Clear the legacy PAT field in users
	DB.Exec("UPDATE users SET github_pat = '' WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)")

	// Asynchronously fetch the actual username from GitHub to update the account name
	go func(id int64, rawToken string) {
		provider := git.NewGitHubProvider(rawToken)
		username, err := provider.GetAuthenticatedUsername()
		if err == nil && username != "" {
			DB.Exec("UPDATE github_accounts SET name = ?, username = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", username, username, id)
		}
	}(accountID, decrypted)
}

// migrateSSLCertsToTable copies existing ssl_provider/ssl_active data from sites into the certificates table
// and backfills expiry dates by reading the certificate files on disk.
func migrateSSLCertsToTable() {
	rows, err := DB.Query("SELECT id, domain, ssl_provider, ssl_active FROM sites WHERE ssl_provider != 'none' AND ssl_provider != ''")
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, active int
		var domain, provider string
		if err := rows.Scan(&id, &domain, &provider, &active); err != nil {
			continue
		}
		var count int
		DB.QueryRow("SELECT COUNT(*) FROM certificates WHERE site_id = ? AND provider = ? AND domain = ?", id, provider, domain).Scan(&count)
		if count == 0 {
			var certPath, keyPath string
			if provider == "letsencrypt" {
				certPath = fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem", domain)
				keyPath = fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem", domain)
			} else if provider == "custom" {
				certPath = fmt.Sprintf("/etc/nginx/ssl/%s/server.crt", domain)
				keyPath = fmt.Sprintf("/etc/nginx/ssl/%s/server.key", domain)
			}
			expiresAt := parseCertExpiry(certPath)
			DB.Exec("INSERT INTO certificates (site_id, domain, provider, cert_path, key_path, active, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)", id, domain, provider, certPath, keyPath, active, expiresAt)
		}
	}

	// Backfill expiry for existing certs that don't have it set.
	backfillRows, err := DB.Query("SELECT id, cert_path FROM certificates WHERE expires_at IS NULL OR expires_at = ''")
	if err != nil {
		return
	}
	defer backfillRows.Close()
	for backfillRows.Next() {
		var certID int
		var certPath string
		if err := backfillRows.Scan(&certID, &certPath); err != nil {
			continue
		}
		if expiresAt := parseCertExpiry(certPath); expiresAt != "" {
			DB.Exec("UPDATE certificates SET expires_at = ? WHERE id = ?", expiresAt, certID)
		}
	}
}

// parseCertExpiry reads a PEM certificate file and returns the expiry date as an ISO 8601 string.
// Returns empty string if the file cannot be read or parsed.
func parseCertExpiry(certPath string) string {
	pemBytes, err := os.ReadFile(certPath)
	if err != nil {
		return ""
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return ""
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return ""
	}
	return cert.NotAfter.Format(time.RFC3339)
}
