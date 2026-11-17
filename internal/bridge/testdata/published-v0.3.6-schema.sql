
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
		deploy_script_mode TEXT DEFAULT 'managed',
		post_deploy_script TEXT DEFAULT '',
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
		restart_on_deploy INTEGER DEFAULT 1,
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
		password TEXT DEFAULT '',
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
		pending_new_password_engine TEXT DEFAULT '',
		credentials_generation INTEGER DEFAULT 0,
		credentials_download_generation INTEGER DEFAULT -1,
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
	