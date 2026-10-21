package database

func initPHPUpdatesSchema() error {
	_, err := DB.Exec(`
	CREATE TABLE IF NOT EXISTS php_repository_plans (
		id TEXT PRIMARY KEY, payload TEXT NOT NULL, created_at INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS php_repository_migrations (
		plan_id TEXT PRIMARY KEY, state TEXT NOT NULL, before_policy TEXT NOT NULL,
		after_policy TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, bindings TEXT NOT NULL DEFAULT '[]'
	);
	CREATE TABLE IF NOT EXISTS php_repository_recovery_authorizations (
		plan_id TEXT NOT NULL, generation_id TEXT NOT NULL, manifest_hash TEXT NOT NULL,
		before_policy TEXT NOT NULL, after_policy TEXT NOT NULL,
		PRIMARY KEY(plan_id,generation_id)
	);
 CREATE TABLE IF NOT EXISTS php_update_retention (
  inventory_hash TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL,
  reason TEXT NOT NULL,
  available INTEGER NOT NULL,
  checked_at INTEGER NOT NULL
 );

	CREATE TABLE IF NOT EXISTS php_update_snapshots (
		version TEXT PRIMARY KEY, payload TEXT NOT NULL, dismissed_candidate TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE IF NOT EXISTS php_update_plans (
		id TEXT PRIMARY KEY, version TEXT NOT NULL, payload TEXT NOT NULL, created_at INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS php_update_notice_revisions (
		version TEXT NOT NULL, package TEXT NOT NULL, architecture TEXT NOT NULL,
		revision TEXT NOT NULL, PRIMARY KEY(version,package,architecture)
	);
	CREATE TABLE IF NOT EXISTS php_update_operations (
		id TEXT PRIMARY KEY, kind TEXT NOT NULL, version TEXT NOT NULL DEFAULT '',
		plan_id TEXT NOT NULL DEFAULT '', request_id TEXT UNIQUE,
		status TEXT NOT NULL DEFAULT 'queued', phase TEXT NOT NULL DEFAULT 'queued',
		message TEXT NOT NULL DEFAULT '', output TEXT NOT NULL DEFAULT '',
		username TEXT NOT NULL DEFAULT '', ip_address TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
		started_at INTEGER NOT NULL DEFAULT 0, finished_at INTEGER NOT NULL DEFAULT 0,
		checkpoint TEXT NOT NULL DEFAULT '', result TEXT NOT NULL DEFAULT '{}'
	);
	CREATE UNIQUE INDEX IF NOT EXISTS php_update_one_active_operation
	ON php_update_operations ((1)) WHERE status IN ('queued', 'running');
	CREATE TABLE IF NOT EXISTS php_update_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT, operation_id TEXT NOT NULL,
		phase TEXT NOT NULL, message TEXT NOT NULL, created_at INTEGER NOT NULL,
		FOREIGN KEY(operation_id) REFERENCES php_update_operations(id) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS php_update_verifications (
		parent_id TEXT PRIMARY KEY, operation_id TEXT NOT NULL UNIQUE,
		FOREIGN KEY(parent_id) REFERENCES php_update_operations(id),
		FOREIGN KEY(operation_id) REFERENCES php_update_operations(id)
	);
	CREATE TABLE IF NOT EXISTS php_update_schedule (
		id INTEGER PRIMARY KEY CHECK(id = 1), next_check INTEGER NOT NULL DEFAULT 0
	);
	INSERT OR IGNORE INTO php_update_schedule(id, next_check) VALUES (1, 0);
	CREATE TABLE IF NOT EXISTS php_update_generations (
		id TEXT PRIMARY KEY, version TEXT NOT NULL, head_operation_id TEXT NOT NULL,
		state TEXT NOT NULL, payload TEXT NOT NULL, created_at INTEGER NOT NULL,
		FOREIGN KEY(id) REFERENCES php_update_operations(id),
		FOREIGN KEY(head_operation_id) REFERENCES php_update_operations(id)
	);
	CREATE TABLE IF NOT EXISTS php_update_approvals (
		operation_id TEXT PRIMARY KEY, acknowledge_no_rollback INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY(operation_id) REFERENCES php_update_operations(id)
	);
	CREATE TABLE IF NOT EXISTS php_update_recovery_plans (
		id TEXT PRIMARY KEY, generation_id TEXT NOT NULL, payload TEXT NOT NULL, created_at INTEGER NOT NULL,
		FOREIGN KEY(generation_id) REFERENCES php_update_generations(id)
	);
	CREATE TABLE IF NOT EXISTS php_update_recovery_attempts (
		operation_id TEXT PRIMARY KEY, generation_id TEXT NOT NULL, parent_id TEXT NOT NULL, recovery_plan_id TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(operation_id) REFERENCES php_update_operations(id),
		FOREIGN KEY(generation_id) REFERENCES php_update_generations(id)
	);
	CREATE TABLE IF NOT EXISTS php_update_barriers (
		unit_name TEXT PRIMARY KEY, generation_id TEXT NOT NULL, payload TEXT NOT NULL,
		FOREIGN KEY(generation_id) REFERENCES php_update_generations(id)
	);
	`)
	if err != nil {
		return err
	}
	rows, checkErr := DB.Query("SELECT recovery_plan_id FROM php_update_recovery_attempts LIMIT 0")
	if checkErr == nil {
		return rows.Close()
	}
	_, err = DB.Exec("ALTER TABLE php_update_recovery_attempts ADD COLUMN recovery_plan_id TEXT NOT NULL DEFAULT ''")
	return err
}
