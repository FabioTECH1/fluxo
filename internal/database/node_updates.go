package database

func initNodeUpdatesSchema() error {
	_, err := DB.Exec(`
CREATE TABLE IF NOT EXISTS node_update_snapshot(id INTEGER PRIMARY KEY CHECK(id=1),payload TEXT NOT NULL DEFAULT '{}',next_check INTEGER NOT NULL DEFAULT 0,dismissed_candidate TEXT NOT NULL DEFAULT '');
INSERT OR IGNORE INTO node_update_snapshot(id) VALUES(1);
CREATE TABLE IF NOT EXISTS node_update_plans(id TEXT PRIMARY KEY,payload TEXT NOT NULL,created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS node_update_operations(id TEXT PRIMARY KEY,kind TEXT NOT NULL,plan_id TEXT NOT NULL DEFAULT '',request_id TEXT UNIQUE,status TEXT NOT NULL,updated_at INTEGER NOT NULL,payload TEXT NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS node_update_one_active ON node_update_operations((1)) WHERE status IN ('queued','running');
CREATE TABLE IF NOT EXISTS node_update_generations(id TEXT PRIMARY KEY,head TEXT NOT NULL,state TEXT NOT NULL,payload TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS node_update_barriers(unit TEXT PRIMARY KEY,generation_id TEXT NOT NULL);
`)
	return err
}

// A terminal worker can still own recovery evidence or a persistent start hold.
func NodeMaintenancePending() (bool, error) {
	var pending bool
	err := DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM node_update_operations WHERE kind!='check' AND status IN ('queued','running')) OR EXISTS(SELECT 1 FROM node_update_generations WHERE state NOT IN ('succeeded','rolled_back')) OR EXISTS(SELECT 1 FROM node_update_barriers)`).Scan(&pending)
	return pending, err
}
