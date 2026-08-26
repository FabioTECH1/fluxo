package database

import (
	"path/filepath"
	"testing"
)

func TestInitDBMigratesLaravelQueueWorkerSchema(t *testing.T) {
	previousDB := DB
	path := filepath.Join(t.TempDir(), "fluxo.db")
	if err := InitDB(path); err != nil {
		t.Fatalf("initialize current database: %v", err)
	}
	if _, err := DB.Exec("INSERT INTO sites (id, domain, path) VALUES (1, 'queue.example.com', '/home/fluxo/queue.example.com')"); err != nil {
		t.Fatalf("insert legacy site: %v", err)
	}
	if _, err := DB.Exec(`INSERT INTO daemons
		(id, site_id, name, command, directory, user, instances, status, start_seconds, stop_seconds, stop_signal, restart_on_deploy)
		VALUES (7, 1, 'Laravel Horizon', 'php8.4 artisan horizon', '/home/fluxo/queue.example.com', 'fluxo', 1, 'active', 1, 30, 'SIGTERM', 1)`); err != nil {
		t.Fatalf("insert legacy Horizon daemon: %v", err)
	}
	if _, err := DB.Exec(`INSERT INTO daemons
		(id, site_id, name, command, directory, user, instances, status, start_seconds, stop_seconds, stop_signal, restart_on_deploy)
		VALUES (8, 1, 'Node.js', 'npm run start', '/home/fluxo/queue.example.com', 'fluxo', 1, 'active', 1, 15, 'SIGTERM', 1)`); err != nil {
		t.Fatalf("insert legacy Node.js daemon: %v", err)
	}
	if _, err := DB.Exec(`
		DROP TABLE laravel_queue_workers;
		DROP TRIGGER IF EXISTS cleanup_site_daemons;
		CREATE TABLE daemons_legacy (
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
		INSERT INTO daemons_legacy
			(id, site_id, name, command, directory, user, instances, status, start_seconds, stop_seconds, stop_signal, restart_on_deploy, created_at, updated_at)
		SELECT id, site_id, name, command, directory, user, instances, status, start_seconds, stop_seconds, stop_signal, restart_on_deploy, created_at, updated_at FROM daemons;
		DROP TABLE daemons;
		ALTER TABLE daemons_legacy RENAME TO daemons;
		CREATE TRIGGER cleanup_site_daemons
		AFTER DELETE ON sites
		BEGIN
			DELETE FROM daemons WHERE site_id = OLD.id;
		END;
	`); err != nil {
		t.Fatalf("prepare legacy daemon schema: %v", err)
	}
	if err := DB.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	if err := InitDB(path); err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	t.Cleanup(func() {
		_ = DB.Close()
		DB = previousDB
	})

	var managedKind string
	if err := DB.QueryRow("SELECT managed_kind FROM daemons WHERE id = 7").Scan(&managedKind); err != nil {
		t.Fatalf("load migrated daemon: %v", err)
	}
	if managedKind != "laravel_horizon" {
		t.Fatalf("managed kind = %q, want laravel_horizon", managedKind)
	}
	if err := DB.QueryRow("SELECT managed_kind FROM daemons WHERE id = 8").Scan(&managedKind); err != nil {
		t.Fatalf("load migrated Node.js daemon: %v", err)
	}
	if managedKind != "node_app" {
		t.Fatalf("managed kind = %q, want node_app", managedKind)
	}

	if _, err := DB.Exec(`INSERT INTO laravel_queue_workers
		(site_id, daemon_id, enabled, connection, queues, processes)
		VALUES (1, 9, 1, 'redis', 'high,default', 2)`); err != nil {
		t.Fatalf("insert queue worker configuration after migration: %v", err)
	}
	if _, err := DB.Exec("DELETE FROM sites WHERE id = 1"); err != nil {
		t.Fatalf("delete migrated site: %v", err)
	}
	var configCount int
	if err := DB.QueryRow("SELECT COUNT(*) FROM laravel_queue_workers WHERE site_id = 1").Scan(&configCount); err != nil {
		t.Fatalf("count queue worker configuration: %v", err)
	}
	if configCount != 0 {
		t.Fatalf("queue worker configuration count = %d, want cascade deletion", configCount)
	}
}
