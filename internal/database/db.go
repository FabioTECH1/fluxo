// Package database manages the SQLite database connection, schema, and migrations.
package database

import (
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fluxo/internal/config"

	_ "modernc.org/sqlite" // CGo-free SQLite driver
)

// DB is the global database handle, initialized by InitDB at startup.
var DB *sql.DB

// ReadAdminUsername retrieves the administrator identity without opening the database for writes.
func ReadAdminUsername(dbPath string) (string, error) {
	absolutePath, err := filepath.Abs(dbPath)
	if err != nil {
		return "", fmt.Errorf("resolve database path: %w", err)
	}
	uri := url.URL{Scheme: "file", Path: absolutePath}
	query := uri.Query()
	query.Set("mode", "ro")
	query.Add("_pragma", "busy_timeout(5000)")
	uri.RawQuery = query.Encode()

	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return "", fmt.Errorf("open database read-only: %w", err)
	}
	defer db.Close()

	var username string
	err = db.QueryRow("SELECT username FROM users ORDER BY id ASC LIMIT 1").Scan(&username)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read admin username: %w", err)
	}
	return username, nil
}

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

	if _, err := DB.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return fmt.Errorf("enable SQLite WAL: %w", err)
	}

	// Fresh creation is complete; checked conversions below still support old installations.

	_, err = DB.Exec(BaseSchema)
	if err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}
	installationID, err := InstallationID()
	if err != nil {
		return fmt.Errorf("failed to initialize installation identity: %w", err)
	}
	if _, err := DB.Exec("UPDATE backup_destinations SET server_id = ? WHERE server_id != ?", installationID, installationID); err != nil {
		return fmt.Errorf("failed to align backup destinations with installation identity: %w", err)
	}
	// Replace the short-lived global uniqueness guard with a lookup index;
	// identical object keys are valid when destinations use different buckets.
	if _, err := DB.Exec("DROP INDEX IF EXISTS idx_backup_runs_manifest_key"); err != nil {
		return fmt.Errorf("failed to migrate backup manifest index: %w", err)
	}
	if _, err := DB.Exec("CREATE INDEX IF NOT EXISTS idx_backup_runs_manifest_lookup ON backup_runs (manifest_key) WHERE manifest_key != ''"); err != nil {
		return fmt.Errorf("failed to index backup manifests: %w", err)
	}

	// Bridge preparation: existing columns are explicit no-ops; other errors fail.
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN repository TEXT"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN branch TEXT"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN deployment_strategy TEXT DEFAULT 'standard'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN app_type TEXT DEFAULT 'php'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN app_port INTEGER"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN node_preset TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN node_mode TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN python_preset TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN python_entrypoint TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN app_directory TEXT DEFAULT '.'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN package_manager TEXT DEFAULT 'npm'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN build_command TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN start_command TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN static_output_dir TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_app_port ON sites (app_port) WHERE app_port > 0"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN ssl_provider TEXT DEFAULT 'none'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN ssl_active INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN web_root TEXT DEFAULT '/public'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN push_to_deploy INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN deploy_script TEXT DEFAULT ''"); err != nil {
		return err
	}
	// Existing scripts contain the complete deployment lifecycle and must remain
	// legacy until the owner explicitly resets them to managed application commands.
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN deploy_script_mode TEXT DEFAULT 'legacy'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN expose_env INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN cache_config_after_env_save INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN db_engine TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN deletion_status TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN deletion_error TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN deletion_stage TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN deletion_delete_databases INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN deletion_database_ids TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE deployments ADD COLUMN commit_message TEXT"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE deployments ADD COLUMN commit_author TEXT"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE deployments ADD COLUMN branch TEXT"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE deployments ADD COLUMN trigger_source TEXT DEFAULT 'manual'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE deployments ADD COLUMN target_commit_hash TEXT"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE deployments ADD COLUMN webhook_commit_hash TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE deployments ADD COLUMN failure_reason TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE deployments ADD COLUMN failure_dismissed_at DATETIME"); err != nil {
		return err
	}
	if err := applyLegacyStatement(`UPDATE deployments SET trigger_source = 'repo_sync'
		WHERE COALESCE(trigger_source, 'manual') = 'manual' AND commit_hash IS NULL
			AND (commit_message LIKE ('Git sync ' || char(8212) || ' %') OR commit_message LIKE 'Failed to sync %')`); err != nil {
		return err
	}
	if err := applyLegacyStatement("CREATE INDEX IF NOT EXISTS idx_deployments_site_id ON deployments (site_id, id DESC)"); err != nil {
		return err
	}
	if err := applyLegacyStatement("CREATE INDEX IF NOT EXISTS idx_deployments_site_status_id ON deployments (site_id, status, id DESC)"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE users ADD COLUMN github_pat TEXT"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE users ADD COLUMN admin_email TEXT"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE users ADD COLUMN default_php TEXT DEFAULT '8.4'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE users ADD COLUMN webhook_secret TEXT"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE users ADD COLUMN token_version INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE users ADD COLUMN fluxo_db_password TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE users ADD COLUMN fluxo_sudo_password TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE users ADD COLUMN fluxo_mysql_password TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE users ADD COLUMN fluxo_postgres_password TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE users ADD COLUMN credentials_copied INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE users ADD COLUMN credentials_generation INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE users ADD COLUMN credentials_download_generation INTEGER DEFAULT -1"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE firewall_rules ADD COLUMN rule_type TEXT DEFAULT 'allow'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE firewall_rules ADD COLUMN managed_by TEXT NOT NULL DEFAULT 'dashboard'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE daemons ADD COLUMN name TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE daemons ADD COLUMN managed_kind TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE daemons ADD COLUMN user TEXT DEFAULT 'fluxo'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE daemons ADD COLUMN start_seconds INTEGER DEFAULT 1"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE daemons ADD COLUMN stop_seconds INTEGER DEFAULT 15"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE daemons ADD COLUMN stop_signal TEXT DEFAULT 'SIGTERM'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE daemons ADD COLUMN restart_on_deploy INTEGER DEFAULT 1"); err != nil {
		return err
	}
	if err := applyLegacyStatement("UPDATE daemons SET restart_on_deploy = 0 WHERE site_id = 0"); err != nil {
		return err
	}
	if err := applyLegacyStatement("UPDATE daemons SET managed_kind = 'laravel_horizon' WHERE COALESCE(managed_kind, '') = '' AND (name = 'Laravel Horizon' OR command LIKE '% artisan horizon' OR command LIKE '% artisan horizon %')"); err != nil {
		return err
	}
	if err := applyLegacyStatement("UPDATE daemons SET managed_kind = 'laravel_octane' WHERE COALESCE(managed_kind, '') = '' AND (name = 'Laravel Octane' OR command LIKE '%artisan octane:start%')"); err != nil {
		return err
	}
	if err := applyLegacyStatement("UPDATE daemons SET managed_kind = 'laravel_nightwatch' WHERE COALESCE(managed_kind, '') = '' AND (name = 'Nightwatch' OR command LIKE '%nightwatch:agent%')"); err != nil {
		return err
	}
	if err := applyLegacyStatement("UPDATE daemons SET managed_kind = 'node_app' WHERE COALESCE(managed_kind, '') = '' AND name = 'Node.js'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE crons ADD COLUMN user TEXT DEFAULT 'fluxo'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE crons ADD COLUMN name TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE activity ADD COLUMN username TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE activity ADD COLUMN ip_address TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE certificates ADD COLUMN expires_at DATETIME"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE certificates ADD COLUMN source_certificate_id INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE domain_aliases ADD COLUMN ssl_disabled INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE domain_aliases ADD COLUMN www_redirect TEXT NOT NULL DEFAULT 'none'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE certificate_domain_bindings ADD COLUMN origin TEXT NOT NULL DEFAULT 'manual'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE orphaned_certificates ADD COLUMN cleanup_status TEXT DEFAULT 'pending'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE orphaned_certificates ADD COLUMN cleanup_origin TEXT DEFAULT 'legacy'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE orphaned_certificates ADD COLUMN cleanup_error TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE orphaned_certificates ADD COLUMN cleanup_attempts INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE orphaned_certificates ADD COLUMN cleaned_at DATETIME"); err != nil {
		return err
	}
	if err := applyLegacyStatement("CREATE INDEX IF NOT EXISTS idx_orphaned_certificates_cleanup ON orphaned_certificates (cleanup_status, archived_at)"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN github_deploy_key_id INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN github_webhook_id INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN github_webhook_url TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN github_account_id INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE sites ADD COLUMN www_redirect TEXT NOT NULL DEFAULT 'none'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE github_accounts ADD COLUMN username TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE users ADD COLUMN pending_new_password_engine TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE databases ADD COLUMN password TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE backup_destinations ADD COLUMN jurisdiction TEXT DEFAULT 'default'"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE backup_runs ADD COLUMN manifest_version_id TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE backup_artifacts ADD COLUMN object_version_id TEXT DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE backup_plans ADD COLUMN encryption_password TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := applyLegacyStatement("ALTER TABLE backup_runs ADD COLUMN encrypted INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := applyLegacyStatement(`CREATE TABLE IF NOT EXISTS github_webhook_observations (
		repository TEXT NOT NULL,
		hook_id INTEGER NOT NULL,
		last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY(repository, hook_id)
	)`); err != nil {
		return err
	}
	if err := applyLegacyStatement("CREATE INDEX IF NOT EXISTS idx_github_webhook_observations_seen ON github_webhook_observations (last_seen_at)"); err != nil {
		return err
	}
	if err := migrateStandaloneSiteTables(); err != nil {
		return fmt.Errorf("failed to migrate standalone process tables: %w", err)
	}

	// Fix: app_port unique index only applies to ports > 0, not the default 0
	if _, err := DB.Exec("DROP INDEX IF EXISTS idx_sites_app_port"); err != nil {
		return err
	}
	if _, err := DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_app_port ON sites (app_port) WHERE app_port > 0"); err != nil {
		return err
	}

	// Migrate existing SSL data to certificates table
	if err := migrateSSLCertsToTable(); err != nil {
		return fmt.Errorf("certificate conversion: %w", err)
	}

	// Migrate existing fluxo_db_password to engine-specific columns.
	var existingMysqlPass, existingDbPass string
	DB.QueryRow("SELECT fluxo_mysql_password FROM users ORDER BY id ASC LIMIT 1").Scan(&existingMysqlPass)
	if existingMysqlPass == "" {
		DB.QueryRow("SELECT fluxo_db_password FROM users ORDER BY id ASC LIMIT 1").Scan(&existingDbPass)
		if existingDbPass != "" {
			DB.Exec("UPDATE users SET fluxo_mysql_password = ?, fluxo_postgres_password = ? WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)", existingDbPass, existingDbPass)
		}
	}
	// Migrate: replace old UNIQUE(name) with UNIQUE(engine, name) on databases table.
	if err := migrateDatabaseRegistryUniqueness(); err != nil {
		return fmt.Errorf("failed to migrate database registry uniqueness: %w", err)
	}

	// Stored deployment scripts are user source. The bridge reports old
	// fragments for deliberate review; initialization never rewrites them.

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
	if err := initPHPUpdatesSchema(); err != nil {
		return fmt.Errorf("initialize PHP update state: %w", err)
	}
	if err := initNodeUpdatesSchema(); err != nil {
		return fmt.Errorf("initialize Node.js update state: %w", err)
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

func migrateDatabaseRegistryUniqueness() error {
	var unfinishedNext int
	if err := DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='databases_next'").Scan(&unfinishedNext); err != nil {
		return err
	}
	if unfinishedNext != 0 {
		return errors.New("unfinished databases_next conversion exists; preserve its rows and reconcile before retrying")
	}
	hasConstraint, err := databaseRegistryHasEngineNameConstraint()
	if err != nil {
		return err
	}
	var staleTable int
	if err := DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'databases_migrate'").Scan(&staleTable); err != nil {
		return err
	}
	if hasConstraint && staleTable == 0 {
		return nil
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DROP TABLE IF EXISTS databases_next"); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE TABLE databases_next (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		engine TEXT NOT NULL,
		name TEXT NOT NULL,
		username TEXT NOT NULL,
		password TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(engine, name)
	)`); err != nil {
		return err
	}
	copySQL := `INSERT INTO databases_next
		(id, site_id, engine, name, username, password, created_at)
		SELECT id, site_id, engine, name, username, password, created_at FROM `
	if _, err := tx.Exec(copySQL + "databases"); err != nil {
		return err
	}
	if staleTable != 0 {
		var conflicts int
		if err := tx.QueryRow(`SELECT count(*) FROM databases_migrate s WHERE EXISTS (
		 SELECT 1 FROM databases_next n WHERE (n.id=s.id OR (n.engine=s.engine AND n.name=s.name))
		 AND NOT(n.id IS s.id AND n.site_id IS s.site_id AND n.engine IS s.engine AND n.name IS s.name
		 AND n.username IS s.username AND n.password IS s.password AND n.created_at IS s.created_at))`).Scan(&conflicts); err != nil {
			return err
		}
		if conflicts > 0 {
			return errors.New("database registry conversion has conflicting rows; original tables were preserved")
		}
		if _, err := tx.Exec(copySQL + `databases_migrate s WHERE NOT EXISTS (SELECT 1 FROM databases_next n WHERE n.id=s.id OR (n.engine=s.engine AND n.name=s.name))`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("DROP TABLE databases"); err != nil {
		return err
	}
	if _, err := tx.Exec("DROP TABLE IF EXISTS databases_migrate"); err != nil {
		return err
	}
	if _, err := tx.Exec("ALTER TABLE databases_next RENAME TO databases"); err != nil {
		return err
	}
	return tx.Commit()
}

func databaseRegistryHasEngineNameConstraint() (bool, error) {
	rows, err := DB.Query("PRAGMA index_list(databases)")
	if err != nil {
		return false, err
	}
	type indexInfo struct {
		name    string
		unique  int
		partial int
	}
	indexes := make([]indexInfo, 0)
	for rows.Next() {
		var seq, unique, partial int
		var name, origin string
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			return false, err
		}
		indexes = append(indexes, indexInfo{name: name, unique: unique, partial: partial})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()

	hasEngineName := false
	hasNameOnly := false
	for _, index := range indexes {
		if index.unique == 0 || index.partial != 0 {
			continue
		}
		columnRows, err := DB.Query("SELECT COALESCE(name, '') FROM pragma_index_info(?) ORDER BY seqno", index.name)
		if err != nil {
			return false, err
		}
		columns := make([]string, 0, 2)
		for columnRows.Next() {
			var name string
			if err := columnRows.Scan(&name); err != nil {
				columnRows.Close()
				return false, err
			}
			columns = append(columns, name)
		}
		if err := columnRows.Err(); err != nil {
			columnRows.Close()
			return false, err
		}
		columnRows.Close()
		if len(columns) == 2 && columns[0] == "engine" && columns[1] == "name" {
			hasEngineName = true
		}
		if len(columns) == 1 && columns[0] == "name" {
			hasNameOnly = true
		}
	}
	return hasEngineName && !hasNameOnly, nil
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
			INSERT INTO daemons_no_fk_migrate
				(id, site_id, name, managed_kind, command, directory, user, instances, status, start_seconds, stop_seconds, stop_signal, restart_on_deploy, created_at, updated_at)
			SELECT id, site_id, name, managed_kind, command, directory, user, instances, status, start_seconds, stop_seconds, stop_signal, restart_on_deploy, created_at, updated_at FROM daemons;
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

	tables := []string{"deployments", "commands", "domain_aliases", "site_vhost_overrides"}
	for _, table := range tables {
		if err := archiveOrphanedRows(table); err != nil {
			return fmt.Errorf("clean %s: %w", table, err)
		}
	}
	return nil
}

// Preserve the complete original row and table definition before historical
// orphan cleanup. Conflicting archives are refused rather than overwritten.
func archiveOrphanedRows(table string) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var definition string
	if err = tx.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&definition); err != nil {
		return err
	}
	rows, err := tx.Query("SELECT * FROM " + table + " WHERE site_id NOT IN (SELECT id FROM sites)")
	if err != nil {
		return err
	}
	columns, err := rows.Columns()
	if err != nil {
		rows.Close()
		return err
	}
	type saved struct {
		id      int64
		payload string
	}
	var items []saved
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err = rows.Scan(dest...); err != nil {
			rows.Close()
			return err
		}
		row := map[string]any{}
		var id int64
		for i, name := range columns {
			row[name] = values[i]
			if name == "id" {
				var ok bool
				id, ok = values[i].(int64)
				if !ok {
					rows.Close()
					return errors.New("orphan history has an invalid identity")
				}
			}
		}
		raw, e := json.Marshal(row)
		if e != nil {
			rows.Close()
			return e
		}
		items = append(items, saved{id, string(raw)})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range items {
		var previous string
		e := tx.QueryRow("SELECT payload FROM bridge_orphan_history WHERE source_table=? AND source_id=?", table, item.id).Scan(&previous)
		if e == nil && previous != item.payload {
			return errors.New("orphan history conflicts with a retained original row")
		}
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if errors.Is(e, sql.ErrNoRows) {
			if _, err = tx.Exec("INSERT INTO bridge_orphan_history(source_table,source_id,source_schema,payload) VALUES(?,?,?,?)", table, item.id, definition, item.payload); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec("DELETE FROM " + table + " WHERE site_id NOT IN (SELECT id FROM sites)"); err != nil {
		return err
	}
	return tx.Commit()
}

func archiveOrphanedCertificates() error {
	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("begin orphan certificate archive: %w", err)
	}
	defer tx.Rollback()
	var conflicts int
	if err := tx.QueryRow(`SELECT count(*) FROM certificates c JOIN orphaned_certificates o ON o.certificate_id=c.id
	 WHERE c.site_id NOT IN (SELECT id FROM sites) AND NOT(o.former_site_id IS c.site_id AND o.domain IS c.domain
	 AND o.provider IS c.provider AND o.cert_path IS COALESCE(c.cert_path,'') AND o.key_path IS COALESCE(c.key_path,'')
	 AND o.active IS c.active AND o.expires_at IS c.expires_at AND o.source_certificate_id IS COALESCE(c.source_certificate_id,0)
	 AND o.certificate_created_at IS c.created_at)`).Scan(&conflicts); err != nil {
		return err
	}
	if conflicts > 0 {
		return errors.New("orphan certificate history conflicts; original certificate rows preserved")
	}

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

// EncryptExistingSecrets atomically converts every managed secret store. A
// missing key, corrupt ciphertext or SQL error is never treated as completion.
func EncryptExistingSecrets() error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, store := range []struct{ table, columns string }{
		{"users", "github_pat,fluxo_mysql_password,fluxo_postgres_password,fluxo_sudo_password,fluxo_db_password,webhook_secret"},
		{"github_accounts", "token"}, {"databases", "password"}, {"backup_destinations", "access_key,secret_key"}, {"backup_plans", "encryption_password"},
	} {
		columns := strings.Split(store.columns, ",")
		rows, err := tx.Query("SELECT id," + store.columns + " FROM " + store.table)
		if err != nil {
			return err
		}
		type change struct {
			id            int64
			column, value string
		}
		var changes []change
		for rows.Next() {
			var id int64
			values := make([]sql.NullString, len(columns))
			dest := []any{&id}
			for i := range values {
				dest = append(dest, &values[i])
			}
			if err = rows.Scan(dest...); err != nil {
				rows.Close()
				return err
			}
			for i, v := range values {
				if v.String == "" {
					continue
				}
				if strings.HasPrefix(v.String, "enc:") {
					if _, err = config.DecryptSecret(v.String); err != nil {
						rows.Close()
						return fmt.Errorf("invalid stored secret in %s.%s", store.table, columns[i])
					}
					continue
				}
				encrypted, e := config.EncryptSecret(v.String)
				if e != nil {
					rows.Close()
					return e
				}
				changes = append(changes, change{id, columns[i], encrypted})
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, c := range changes {
			if _, err = tx.Exec("UPDATE "+store.table+" SET "+c.column+"=? WHERE id=?", c.value, c.id); err != nil {
				return err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return MigrateLegacyGitHubPAT()
}

// MigrateLegacyGitHubPAT preserves assigned accounts and commits account
// creation, unbound-site attachment and clearing the old field together.
func MigrateLegacyGitHubPAT() error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var userID int
	var stored sql.NullString
	err = tx.QueryRow("SELECT id,github_pat FROM users ORDER BY id ASC LIMIT 1").Scan(&userID, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if stored.String == "" {
		return nil
	}
	token, err := config.DecryptSecret(stored.String)
	if err != nil {
		return errors.New("legacy GitHub token requires key recovery")
	}
	rows, err := tx.Query("SELECT id,token FROM github_accounts ORDER BY id")
	if err != nil {
		return err
	}
	var accountID int64
	for rows.Next() {
		var id int64
		var value string
		if err = rows.Scan(&id, &value); err != nil {
			rows.Close()
			return err
		}
		plain, e := config.DecryptSecret(value)
		if e != nil {
			rows.Close()
			return errors.New("GitHub account token requires key recovery")
		}
		if plain == token && accountID == 0 {
			accountID = id
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if accountID == 0 {
		result, e := tx.Exec("INSERT INTO github_accounts(name,token) VALUES('Default GitHub',?)", stored.String)
		if e != nil {
			return e
		}
		accountID, err = result.LastInsertId()
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec("UPDATE sites SET github_account_id=? WHERE COALESCE(github_account_id,0)=0 AND COALESCE(repository,'')!=''", accountID); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE users SET github_pat='' WHERE id=?", userID); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateSSLCertsToTable copies legacy SSL state and reconciles expiry dates from disk.
func migrateSSLCertsToTable() error {
	rows, err := DB.Query("SELECT id, domain, ssl_provider, ssl_active FROM sites WHERE ssl_provider != 'none' AND ssl_provider != ''")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, active int
		var domain, provider string
		if err := rows.Scan(&id, &domain, &provider, &active); err != nil {
			return err
		}
		var count int
		if err := DB.QueryRow("SELECT COUNT(*) FROM certificates WHERE site_id = ? AND provider = ? AND domain = ?", id, provider, domain).Scan(&count); err != nil {
			return err
		}
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
			if _, err := DB.Exec("INSERT INTO certificates (site_id, domain, provider, cert_path, key_path, active, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)", id, domain, provider, certPath, keyPath, active, expiresAt); err != nil {
				return err
			}
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	// Certbot renews files in place, so refresh stored expiry dates on every startup.
	backfillRows, err := DB.Query("SELECT id, cert_path, COALESCE(expires_at, '') FROM certificates")
	if err != nil {
		return err
	}
	defer backfillRows.Close()
	for backfillRows.Next() {
		var certID int
		var certPath, currentExpiry string
		if err := backfillRows.Scan(&certID, &certPath, &currentExpiry); err != nil {
			return err
		}
		if expiresAt := parseCertExpiry(certPath); expiresAt != "" && expiresAt != currentExpiry {
			if _, err := DB.Exec("UPDATE certificates SET expires_at = ? WHERE id = ?", expiresAt, certID); err != nil {
				return err
			}
		}
	}
	return backfillRows.Err()
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
