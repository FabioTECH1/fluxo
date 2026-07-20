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
	DB, err = sql.Open("sqlite", sqliteConnectionString(filepath))
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

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
		deletion_status TEXT DEFAULT '',
		deletion_error TEXT DEFAULT '',
		deletion_stage TEXT DEFAULT '',
		deletion_delete_databases INTEGER DEFAULT 0,
		deletion_database_ids TEXT DEFAULT '',
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
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS crons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		name TEXT DEFAULT '',
		expression TEXT NOT NULL,
		command TEXT NOT NULL,
		user TEXT DEFAULT 'fluxo',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
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

	CREATE TRIGGER IF NOT EXISTS prevent_duplicate_site_domain
	BEFORE INSERT ON sites
	WHEN EXISTS (SELECT 1 FROM sites WHERE domain = NEW.domain COLLATE NOCASE)
		OR EXISTS (SELECT 1 FROM domain_aliases WHERE domain = NEW.domain COLLATE NOCASE)
	BEGIN
		SELECT RAISE(ABORT, 'domain already in use');
	END;

	CREATE TRIGGER IF NOT EXISTS prevent_duplicate_domain_alias
	BEFORE INSERT ON domain_aliases
	WHEN EXISTS (SELECT 1 FROM sites WHERE domain = NEW.domain COLLATE NOCASE)
		OR EXISTS (SELECT 1 FROM domain_aliases WHERE domain = NEW.domain COLLATE NOCASE)
	BEGIN
		SELECT RAISE(ABORT, 'domain already in use');
	END;

	CREATE TRIGGER IF NOT EXISTS prevent_updated_duplicate_site_domain
	BEFORE UPDATE OF domain ON sites
	WHEN EXISTS (SELECT 1 FROM sites WHERE id != NEW.id AND domain = NEW.domain COLLATE NOCASE)
		OR EXISTS (SELECT 1 FROM domain_aliases WHERE domain = NEW.domain COLLATE NOCASE)
	BEGIN
		SELECT RAISE(ABORT, 'domain already in use');
	END;

	CREATE TRIGGER IF NOT EXISTS prevent_updated_duplicate_domain_alias
	BEFORE UPDATE OF domain ON domain_aliases
	WHEN EXISTS (SELECT 1 FROM sites WHERE domain = NEW.domain COLLATE NOCASE)
		OR EXISTS (SELECT 1 FROM domain_aliases WHERE id != NEW.id AND domain = NEW.domain COLLATE NOCASE)
	BEGIN
		SELECT RAISE(ABORT, 'domain already in use');
	END;

	CREATE TRIGGER IF NOT EXISTS cleanup_site_daemons
	AFTER DELETE ON sites
	BEGIN
		DELETE FROM daemons WHERE site_id = OLD.id;
	END;

	CREATE TRIGGER IF NOT EXISTS cleanup_site_crons
	AFTER DELETE ON sites
	BEGIN
		DELETE FROM crons WHERE site_id = OLD.id;
	END;

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
		source_certificate_id INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(site_id) REFERENCES sites(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS orphaned_certificates (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		certificate_id INTEGER NOT NULL UNIQUE,
		former_site_id INTEGER NOT NULL,
		domain TEXT NOT NULL,
		provider TEXT NOT NULL,
		cert_path TEXT DEFAULT '',
		key_path TEXT DEFAULT '',
		active INTEGER DEFAULT 0,
		expires_at DATETIME,
		source_certificate_id INTEGER DEFAULT 0,
		certificate_created_at DATETIME,
		cleanup_origin TEXT DEFAULT 'legacy',
		cleanup_status TEXT DEFAULT 'pending',
		cleanup_error TEXT DEFAULT '',
		cleanup_attempts INTEGER DEFAULT 0,
		cleaned_at DATETIME,
		archived_at DATETIME DEFAULT CURRENT_TIMESTAMP
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

	CREATE TABLE IF NOT EXISTS backup_destinations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		provider TEXT NOT NULL,
		bucket TEXT NOT NULL,
		region TEXT DEFAULT '',
		account_id TEXT DEFAULT '',
		jurisdiction TEXT DEFAULT 'default',
		prefix TEXT NOT NULL DEFAULT 'fluxo',
		server_id TEXT NOT NULL,
		access_key TEXT DEFAULT '',
		secret_key TEXT DEFAULT '',
		use_instance_role INTEGER NOT NULL DEFAULT 0,
		is_default INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS backup_plans (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		site_id INTEGER NOT NULL,
		destination_id INTEGER NOT NULL,
		include_files INTEGER NOT NULL DEFAULT 1,
		schedule TEXT NOT NULL DEFAULT 'daily',
		backup_hour INTEGER NOT NULL DEFAULT 2,
		retention_profile TEXT NOT NULL DEFAULT 'recommended',
		enabled INTEGER NOT NULL DEFAULT 1,
		next_run_at DATETIME,
		last_run_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS backup_plan_databases (
		plan_id INTEGER NOT NULL,
		database_id INTEGER NOT NULL,
		PRIMARY KEY (plan_id, database_id)
	);

	CREATE TABLE IF NOT EXISTS backup_runs (
		id TEXT PRIMARY KEY,
		plan_id INTEGER NOT NULL,
		plan_name TEXT NOT NULL,
		destination_id INTEGER NOT NULL,
		destination_name TEXT NOT NULL,
		site_id INTEGER NOT NULL,
		site_domain TEXT NOT NULL,
		trigger TEXT NOT NULL DEFAULT 'manual',
		status TEXT NOT NULL DEFAULT 'queued',
		total_size_bytes INTEGER NOT NULL DEFAULT 0,
		manifest_key TEXT DEFAULT '',
		manifest_version_id TEXT DEFAULT '',
		error TEXT DEFAULT '',
		started_at DATETIME,
		completed_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS backup_artifacts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		run_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		database_id INTEGER DEFAULT 0,
		database_name TEXT DEFAULT '',
		engine TEXT DEFAULT '',
		object_key TEXT NOT NULL,
		object_version_id TEXT DEFAULT '',
		filename TEXT NOT NULL,
		size_bytes INTEGER NOT NULL DEFAULT 0,
		sha256 TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_backup_plans_due ON backup_plans (enabled, next_run_at);
	CREATE INDEX IF NOT EXISTS idx_backup_runs_plan_created ON backup_runs (plan_id, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_backup_runs_status ON backup_runs (status);
	CREATE INDEX IF NOT EXISTS idx_backup_artifacts_run ON backup_artifacts (run_id);
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
	DB.Exec("ALTER TABLE sites ADD COLUMN deletion_status TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE sites ADD COLUMN deletion_error TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE sites ADD COLUMN deletion_stage TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE sites ADD COLUMN deletion_delete_databases INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE sites ADD COLUMN deletion_database_ids TEXT DEFAULT ''")
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
	DB.Exec("ALTER TABLE certificates ADD COLUMN source_certificate_id INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE orphaned_certificates ADD COLUMN cleanup_status TEXT DEFAULT 'pending'")
	DB.Exec("ALTER TABLE orphaned_certificates ADD COLUMN cleanup_origin TEXT DEFAULT 'legacy'")
	DB.Exec("ALTER TABLE orphaned_certificates ADD COLUMN cleanup_error TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE orphaned_certificates ADD COLUMN cleanup_attempts INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE orphaned_certificates ADD COLUMN cleaned_at DATETIME")
	DB.Exec("CREATE INDEX IF NOT EXISTS idx_orphaned_certificates_cleanup ON orphaned_certificates (cleanup_status, archived_at)")
	DB.Exec("ALTER TABLE sites ADD COLUMN github_deploy_key_id INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE sites ADD COLUMN github_webhook_id INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE sites ADD COLUMN github_account_id INTEGER DEFAULT 0")
	DB.Exec("ALTER TABLE github_accounts ADD COLUMN username TEXT NOT NULL DEFAULT ''")
	DB.Exec("ALTER TABLE users ADD COLUMN pending_new_password_engine TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE backup_destinations ADD COLUMN jurisdiction TEXT DEFAULT 'default'")
	DB.Exec("ALTER TABLE backup_runs ADD COLUMN manifest_version_id TEXT DEFAULT ''")
	DB.Exec("ALTER TABLE backup_artifacts ADD COLUMN object_version_id TEXT DEFAULT ''")
	if err := migrateStandaloneSiteTables(); err != nil {
		return fmt.Errorf("failed to migrate standalone process tables: %w", err)
	}

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

	// Migrate: replace old UNIQUE(name) with UNIQUE(engine, name) on databases table.
	DB.Exec("CREATE TABLE IF NOT EXISTS databases_migrate (id INTEGER PRIMARY KEY AUTOINCREMENT, site_id INTEGER NOT NULL, engine TEXT NOT NULL, name TEXT NOT NULL, username TEXT NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP, UNIQUE(engine, name))")
	DB.Exec("INSERT OR IGNORE INTO databases_migrate (id, site_id, engine, name, username, created_at) SELECT id, site_id, engine, name, username, created_at FROM databases")
	DB.Exec("DROP TABLE databases")
	DB.Exec("ALTER TABLE databases_migrate RENAME TO databases")

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

	if err := cleanupOrphanedSiteRecords(); err != nil {
		return fmt.Errorf("failed to clean orphaned site records: %w", err)
	}
	if err := validateDomainOwnership(); err != nil {
		return err
	}
	if _, err := DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_domain_nocase ON sites (domain COLLATE NOCASE)"); err != nil {
		return fmt.Errorf("failed to enforce unique site domains: %w", err)
	}
	if _, err := DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_domain_aliases_domain_nocase ON domain_aliases (domain COLLATE NOCASE)"); err != nil {
		return fmt.Errorf("failed to enforce unique domain aliases: %w", err)
	}
	if err := validateForeignKeys(); err != nil {
		return err
	}

	return nil
}

func sqliteConnectionString(filepath string) string {
	separator := "?"
	if strings.Contains(filepath, "?") {
		separator = "&"
	}
	return filepath + separator + "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
}

func migrateStandaloneSiteTables() error {
	daemonsHaveFK, err := tableHasForeignKeys("daemons")
	if err != nil {
		return err
	}
	cronsHaveFK, err := tableHasForeignKeys("crons")
	if err != nil {
		return err
	}
	if !daemonsHaveFK && !cronsHaveFK {
		return nil
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if daemonsHaveFK {
		if _, err := tx.Exec(`
			DROP TRIGGER IF EXISTS cleanup_site_daemons;
			CREATE TABLE daemons_no_fk_migrate (
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
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
			);
			INSERT INTO daemons_no_fk_migrate
				(id, site_id, name, command, directory, user, instances, status, start_seconds, stop_seconds, stop_signal, created_at, updated_at)
			SELECT id, site_id, name, command, directory, user, instances, status, start_seconds, stop_seconds, stop_signal, created_at, updated_at FROM daemons;
			DROP TABLE daemons;
			ALTER TABLE daemons_no_fk_migrate RENAME TO daemons;
			CREATE TRIGGER cleanup_site_daemons
			AFTER DELETE ON sites
			BEGIN
				DELETE FROM daemons WHERE site_id = OLD.id;
			END;
		`); err != nil {
			return err
		}
	}

	if cronsHaveFK {
		if _, err := tx.Exec(`
			DROP TRIGGER IF EXISTS cleanup_site_crons;
			CREATE TABLE crons_no_fk_migrate (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				site_id INTEGER NOT NULL,
				name TEXT DEFAULT '',
				expression TEXT NOT NULL,
				command TEXT NOT NULL,
				user TEXT DEFAULT 'fluxo',
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP
			);
			INSERT INTO crons_no_fk_migrate (id, site_id, name, expression, command, user, created_at)
			SELECT id, site_id, name, expression, command, user, created_at FROM crons;
			DROP TABLE crons;
			ALTER TABLE crons_no_fk_migrate RENAME TO crons;
			CREATE TRIGGER cleanup_site_crons
			AFTER DELETE ON sites
			BEGIN
				DELETE FROM crons WHERE site_id = OLD.id;
			END;
		`); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func tableHasForeignKeys(table string) (bool, error) {
	rows, err := DB.Query("PRAGMA foreign_key_list(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), rows.Err()
}

func cleanupOrphanedSiteRecords() error {
	if err := archiveOrphanedCertificates(); err != nil {
		return err
	}

	tables := []string{"deployments", "commands", "domain_aliases"}
	for _, table := range tables {
		if _, err := DB.Exec("DELETE FROM " + table + " WHERE site_id NOT IN (SELECT id FROM sites)"); err != nil {
			return fmt.Errorf("clean %s: %w", table, err)
		}
	}
	return nil
}

func archiveOrphanedCertificates() error {
	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("begin orphan certificate archive: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO orphaned_certificates (
			certificate_id, former_site_id, domain, provider, cert_path, key_path,
			active, expires_at, source_certificate_id, certificate_created_at
		)
		SELECT id, site_id, domain, provider, COALESCE(cert_path, ''), COALESCE(key_path, ''),
		       active, expires_at, COALESCE(source_certificate_id, 0), created_at
		FROM certificates
		WHERE site_id NOT IN (SELECT id FROM sites)
	`); err != nil {
		return fmt.Errorf("archive orphan certificates: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM certificates WHERE site_id NOT IN (SELECT id FROM sites)"); err != nil {
		return fmt.Errorf("remove archived orphan certificates: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit orphan certificate archive: %w", err)
	}
	return nil
}

func validateDomainOwnership() error {
	rows, err := DB.Query(`
		SELECT normalized_domain, GROUP_CONCAT(owner, ', ')
		FROM (
			SELECT LOWER(TRIM(domain)) AS normalized_domain,
			       'site #' || id || ' (' || domain || ')' AS owner
			FROM sites
			UNION ALL
			SELECT LOWER(TRIM(domain)) AS normalized_domain,
			       'alias #' || id || ' (' || domain || ')' AS owner
			FROM domain_aliases
		)
		WHERE normalized_domain != ''
		GROUP BY normalized_domain
		HAVING COUNT(*) > 1
		ORDER BY normalized_domain
		LIMIT 10`)
	if err != nil {
		return fmt.Errorf("failed to audit domain ownership: %w", err)
	}
	defer rows.Close()

	var conflicts []string
	for rows.Next() {
		var domain, owners string
		if err := rows.Scan(&domain, &owners); err != nil {
			return fmt.Errorf("failed to read domain ownership conflict: %w", err)
		}
		conflicts = append(conflicts, fmt.Sprintf("%s: %s", domain, owners))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to audit domain ownership: %w", err)
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("duplicate domain ownership detected (%s); resolve the conflicting sites or aliases before starting Fluxo", strings.Join(conflicts, "; "))
	}
	return nil
}

func validateForeignKeys() error {
	rows, err := DB.Query("PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("failed to verify database relationships: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent string
		var rowID sql.NullInt64
		var foreignKeyID int
		if err := rows.Scan(&table, &rowID, &parent, &foreignKeyID); err != nil {
			return fmt.Errorf("failed to read database relationship violation: %w", err)
		}
		return fmt.Errorf("database relationship violation in %s row %d referencing %s (foreign key %d)", table, rowID.Int64, parent, foreignKeyID)
	}
	return rows.Err()
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

// migrateSSLCertsToTable copies legacy SSL state and reconciles expiry dates from disk.
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

	// Certbot renews files in place, so refresh stored expiry dates on every startup.
	backfillRows, err := DB.Query("SELECT id, cert_path, COALESCE(expires_at, '') FROM certificates")
	if err != nil {
		return
	}
	defer backfillRows.Close()
	for backfillRows.Next() {
		var certID int
		var certPath, currentExpiry string
		if err := backfillRows.Scan(&certID, &certPath, &currentExpiry); err != nil {
			continue
		}
		if expiresAt := parseCertExpiry(certPath); expiresAt != "" && expiresAt != currentExpiry {
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
