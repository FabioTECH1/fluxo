// Package database manages the SQLite database connection and schema.
// The schema is declared as a single CREATE TABLE IF NOT EXISTS block
// followed by idempotent ALTER TABLE migrations — safe to run every startup.
package database

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite" // CGo-free SQLite driver: pure Go, no libsqlite3 needed
)

// DB is the global database handle, initialized by InitDB at startup.
var DB *sql.DB

// InitDB opens the SQLite database at the given file path, pings it,
// applies the schema, and runs incremental migrations. It returns an
// error if the database cannot be opened or the schema fails.
// On first run this creates the file; on subsequent runs all statements
// are idempotent (IF NOT EXISTS / ADD COLUMN + error ignored).
func InitDB(filepath string) error {
	var err error
	DB, err = sql.Open("sqlite", filepath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Base schema — all tables created here with IF NOT EXISTS.
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
		name TEXT NOT NULL UNIQUE,
		username TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
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

	CREATE TABLE IF NOT EXISTS github_cache (
		key TEXT PRIMARY KEY,
		data TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err = DB.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Incremental migrations — each ALTER ADD COLUMN is ignored by SQLite
	// if the column already exists, making them safe to run on every startup.
	DB.Exec("ALTER TABLE sites ADD COLUMN repository TEXT")
	DB.Exec("ALTER TABLE sites ADD COLUMN branch TEXT")
	DB.Exec("ALTER TABLE sites ADD COLUMN deployment_strategy TEXT DEFAULT 'standard'")
	DB.Exec("ALTER TABLE sites ADD COLUMN app_type TEXT DEFAULT 'php'")
	DB.Exec("ALTER TABLE sites ADD COLUMN app_port INTEGER")
	DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_app_port ON sites (app_port) WHERE app_port IS NOT NULL")
	DB.Exec("ALTER TABLE sites ADD COLUMN ssl_provider TEXT DEFAULT 'none'")
	DB.Exec("ALTER TABLE sites ADD COLUMN ssl_active INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE sites ADD COLUMN web_root TEXT DEFAULT '/public'")
	DB.Exec("ALTER TABLE sites ADD COLUMN push_to_deploy INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE sites ADD COLUMN deploy_script TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE sites ADD COLUMN expose_env INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE sites ADD COLUMN db_engine TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE deployments ADD COLUMN commit_message TEXT")
	DB.Exec("ALTER TABLE deployments ADD COLUMN branch TEXT")
	DB.Exec("ALTER TABLE deployments ADD COLUMN trigger_source TEXT DEFAULT 'manual'")
	DB.Exec("ALTER TABLE users ADD COLUMN github_pat TEXT")
	DB.Exec("ALTER TABLE users ADD COLUMN admin_email TEXT")
	DB.Exec("ALTER TABLE users ADD COLUMN default_php TEXT DEFAULT '8.4'")
	DB.Exec("ALTER TABLE users ADD COLUMN webhook_secret TEXT")
	DB.Exec("ALTER TABLE users ADD COLUMN fluxo_db_password TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE users ADD COLUMN fluxo_sudo_password TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE firewall_rules ADD COLUMN rule_type TEXT DEFAULT 'allow'")
	DB.Exec("ALTER TABLE daemons ADD COLUMN name TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE daemons ADD COLUMN user TEXT DEFAULT 'fluxo'")
	DB.Exec("ALTER TABLE daemons ADD COLUMN start_seconds INTEGER DEFAULT 1")
	DB.Exec("ALTER TABLE daemons ADD COLUMN stop_seconds INTEGER DEFAULT 15")
	DB.Exec("ALTER TABLE daemons ADD COLUMN stop_signal TEXT DEFAULT 'SIGTERM'")
	DB.Exec("ALTER TABLE crons ADD COLUMN user TEXT DEFAULT 'fluxo'")
	DB.Exec("ALTER TABLE crons ADD COLUMN name TEXT DEFAULT ''")
	DB.Exec(`CREATE TABLE IF NOT EXISTS databases (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		engine TEXT NOT NULL,
		name TEXT NOT NULL UNIQUE,
		username TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)

	// Clean up existing deploy scripts to remove the lines we are moving under the hood
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
				// Replace the reload command and complete command with empty strings
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
