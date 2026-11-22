package database

// BaseSchema is the complete current fresh-install schema. Historical ALTERs
// remain in InitDB for the bridge and are not required to create a fresh DB.
const BaseSchema = `
	CREATE TABLE IF NOT EXISTS bridge_orphan_history (
		source_table TEXT NOT NULL,
		source_id INTEGER NOT NULL,
		source_schema TEXT NOT NULL,
		payload TEXT NOT NULL,
		archived_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY(source_table, source_id)
	);

	CREATE TABLE IF NOT EXISTS sites (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		domain TEXT NOT NULL UNIQUE,
		path TEXT NOT NULL,
		github_deploy_key_id INTEGER DEFAULT 0,
		github_webhook_id INTEGER DEFAULT 0,
		github_webhook_url TEXT DEFAULT '',
		github_account_id INTEGER DEFAULT 0,
		github_disconnected_user_id INTEGER NOT NULL DEFAULT 0,
		github_disconnected_username TEXT NOT NULL DEFAULT '',
		repository TEXT,
		branch TEXT,
		php_version TEXT,
		app_type TEXT DEFAULT 'php',
		app_port INTEGER,
		node_preset TEXT DEFAULT '',
		node_mode TEXT DEFAULT '',
		python_preset TEXT DEFAULT '',
		python_entrypoint TEXT DEFAULT '',
		app_directory TEXT DEFAULT '.',
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
		expose_env INTEGER DEFAULT 0,
		cache_config_after_env_save INTEGER NOT NULL DEFAULT 0,
		db_engine TEXT DEFAULT '',
		deletion_status TEXT DEFAULT '',
		deletion_error TEXT DEFAULT '',
		deletion_stage TEXT DEFAULT '',
		deletion_delete_databases INTEGER DEFAULT 0,
		deletion_database_ids TEXT DEFAULT '',
		www_redirect TEXT NOT NULL DEFAULT 'none',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS deployments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		branch TEXT,
		trigger_source TEXT DEFAULT 'manual',
		target_commit_hash TEXT,
		commit_hash TEXT,
		webhook_commit_hash TEXT DEFAULT '',
		commit_message TEXT,
		commit_author TEXT,
		status TEXT NOT NULL,
		output TEXT,
		failure_reason TEXT DEFAULT '',
		failure_dismissed_at DATETIME,
		started_at DATETIME,
		finished_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(site_id) REFERENCES sites(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS daemons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		name TEXT DEFAULT '',
		managed_kind TEXT DEFAULT '',
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

	CREATE TABLE IF NOT EXISTS laravel_queue_workers (
		site_id INTEGER PRIMARY KEY,
		daemon_id INTEGER DEFAULT 0,
		enabled INTEGER DEFAULT 0,
		connection TEXT NOT NULL DEFAULT 'database',
		queues TEXT NOT NULL DEFAULT 'default',
		processes INTEGER NOT NULL DEFAULT 1,
		sleep_seconds INTEGER NOT NULL DEFAULT 3,
		tries INTEGER NOT NULL DEFAULT 3,
		timeout_seconds INTEGER NOT NULL DEFAULT 60,
		backoff_seconds INTEGER NOT NULL DEFAULT 0,
		memory_mb INTEGER NOT NULL DEFAULT 128,
		max_time_seconds INTEGER NOT NULL DEFAULT 3600,
		force INTEGER NOT NULL DEFAULT 0,
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

	CREATE TABLE IF NOT EXISTS managed_database_users (
		engine TEXT NOT NULL,
		username TEXT NOT NULL,
		host TEXT NOT NULL DEFAULT '',
		state TEXT NOT NULL DEFAULT 'pending',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY(engine, username, host)
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
		managed_by TEXT NOT NULL DEFAULT 'dashboard',
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
		ssl_disabled INTEGER NOT NULL DEFAULT 0,
		www_redirect TEXT NOT NULL DEFAULT 'none',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(site_id) REFERENCES sites(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS site_vhost_overrides (
		site_id INTEGER PRIMARY KEY,
		config TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(site_id) REFERENCES sites(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS panel_domain (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		domain TEXT NOT NULL DEFAULT '',
		ssl_provider TEXT NOT NULL DEFAULT '',
		cert_path TEXT NOT NULL DEFAULT '',
		key_path TEXT NOT NULL DEFAULT '',
		source_certificate_id INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	INSERT OR IGNORE INTO panel_domain (id) VALUES (1);

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

	CREATE TRIGGER IF NOT EXISTS prevent_site_domain_matching_panel
	BEFORE INSERT ON sites
	WHEN EXISTS (
		SELECT 1 FROM panel_domain
		WHERE domain != '' AND domain = NEW.domain COLLATE NOCASE
	)
	BEGIN
		SELECT RAISE(ABORT, 'domain reserved for panel');
	END;

	CREATE TRIGGER IF NOT EXISTS prevent_updated_site_domain_matching_panel
	BEFORE UPDATE OF domain ON sites
	WHEN EXISTS (
		SELECT 1 FROM panel_domain
		WHERE domain != '' AND domain = NEW.domain COLLATE NOCASE
	)
	BEGIN
		SELECT RAISE(ABORT, 'domain reserved for panel');
	END;

	CREATE TRIGGER IF NOT EXISTS prevent_alias_domain_matching_panel
	BEFORE INSERT ON domain_aliases
	WHEN EXISTS (
		SELECT 1 FROM panel_domain
		WHERE domain != '' AND domain = NEW.domain COLLATE NOCASE
	)
	BEGIN
		SELECT RAISE(ABORT, 'domain reserved for panel');
	END;

	CREATE TRIGGER IF NOT EXISTS prevent_updated_alias_domain_matching_panel
	BEFORE UPDATE OF domain ON domain_aliases
	WHEN EXISTS (
		SELECT 1 FROM panel_domain
		WHERE domain != '' AND domain = NEW.domain COLLATE NOCASE
	)
	BEGIN
		SELECT RAISE(ABORT, 'domain reserved for panel');
	END;

	CREATE TRIGGER IF NOT EXISTS prevent_panel_domain_conflict
	BEFORE UPDATE OF domain ON panel_domain
	WHEN NEW.domain != '' AND (
		EXISTS (SELECT 1 FROM sites WHERE domain = NEW.domain COLLATE NOCASE)
		OR EXISTS (SELECT 1 FROM domain_aliases WHERE domain = NEW.domain COLLATE NOCASE)
	)
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
		webhook_secret TEXT,
		token_version INTEGER DEFAULT 0,
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
		username TEXT DEFAULT '',
		ip_address TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS certificates (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		domain TEXT NOT NULL,
		provider TEXT NOT NULL,
		expires_at DATETIME,
		cert_path TEXT,
		key_path TEXT,
		active INTEGER DEFAULT 0,
		source_certificate_id INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(site_id) REFERENCES sites(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS certificate_domain_bindings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		domain TEXT NOT NULL,
		certificate_id INTEGER NOT NULL,
		origin TEXT NOT NULL DEFAULT 'manual',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(site_id, domain),
		FOREIGN KEY(site_id) REFERENCES sites(id) ON DELETE CASCADE,
		FOREIGN KEY(certificate_id) REFERENCES certificates(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_certificate_domain_bindings_certificate
	ON certificate_domain_bindings (certificate_id);

	CREATE TRIGGER IF NOT EXISTS cleanup_domain_certificate_binding
	AFTER DELETE ON domain_aliases
	BEGIN
		DELETE FROM certificate_domain_bindings
		WHERE site_id = OLD.site_id AND domain = OLD.domain COLLATE NOCASE;
	END;

	CREATE TRIGGER IF NOT EXISTS update_domain_certificate_binding
	AFTER UPDATE OF domain ON domain_aliases
	BEGIN
		UPDATE certificate_domain_bindings
		SET domain = NEW.domain, updated_at = CURRENT_TIMESTAMP
		WHERE site_id = OLD.site_id AND domain = OLD.domain COLLATE NOCASE;
	END;

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

	CREATE TABLE IF NOT EXISTS system_metadata (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS github_accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		username TEXT NOT NULL DEFAULT '',
		token TEXT NOT NULL,
		github_user_id INTEGER NOT NULL DEFAULT 0,
		token_expires_at TEXT NOT NULL DEFAULT '',
		token_status TEXT NOT NULL DEFAULT 'unchecked',
		token_checked_at TEXT NOT NULL DEFAULT '',
		deployment_access TEXT NOT NULL DEFAULT '{}',
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
		prefix TEXT NOT NULL DEFAULT 'fluxo-backups',
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
		encryption_password TEXT NOT NULL DEFAULT '',
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
		encrypted INTEGER NOT NULL DEFAULT 0,
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

// CurrentSchemaSQL contains no data conversions or external effects.
func CurrentSchemaSQL() string {
	return BaseSchema + SiteNetworkSchema + `CREATE TABLE IF NOT EXISTS github_webhook_observations (
		repository TEXT NOT NULL,
		hook_id INTEGER NOT NULL,
		last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY(repository, hook_id)
	);
CREATE INDEX IF NOT EXISTS idx_backup_runs_manifest_lookup ON backup_runs (manifest_key) WHERE manifest_key != '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_github_accounts_user_id ON github_accounts (github_user_id) WHERE github_user_id > 0;
CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_app_port ON sites (app_port) WHERE app_port > 0;
CREATE INDEX IF NOT EXISTS idx_deployments_site_id ON deployments (site_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_deployments_site_status_id ON deployments (site_id, status, id DESC);
CREATE INDEX IF NOT EXISTS idx_orphaned_certificates_cleanup ON orphaned_certificates (cleanup_status, archived_at);
CREATE INDEX IF NOT EXISTS idx_github_webhook_observations_seen ON github_webhook_observations (last_seen_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_domain_nocase ON sites (domain COLLATE NOCASE);
CREATE UNIQUE INDEX IF NOT EXISTS idx_domain_aliases_domain_nocase ON domain_aliases (domain COLLATE NOCASE);` + phpUpdatesSchema + nodeUpdatesSchema
}
