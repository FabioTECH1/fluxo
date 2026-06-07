package database

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func InitDB(filepath string) error {
	var err error
	DB, err = sql.Open("sqlite", filepath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

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
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS deployments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		commit_hash TEXT,
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
	`
	_, err = DB.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Simple migrations for MVP
	DB.Exec("ALTER TABLE sites ADD COLUMN repository TEXT")
	DB.Exec("ALTER TABLE sites ADD COLUMN branch TEXT")
	DB.Exec("ALTER TABLE sites ADD COLUMN deployment_strategy TEXT DEFAULT 'standard'")
	DB.Exec("ALTER TABLE sites ADD COLUMN app_type TEXT DEFAULT 'php'")
	DB.Exec("ALTER TABLE sites ADD COLUMN app_port INTEGER")
	DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_app_port ON sites (app_port) WHERE app_port IS NOT NULL")
	DB.Exec("ALTER TABLE sites ADD COLUMN ssl_provider TEXT DEFAULT 'none'")
	DB.Exec("ALTER TABLE users ADD COLUMN github_pat TEXT")
	DB.Exec("ALTER TABLE users ADD COLUMN admin_email TEXT")
	DB.Exec("ALTER TABLE users ADD COLUMN default_php TEXT DEFAULT '8.4'")
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

	return nil
}
