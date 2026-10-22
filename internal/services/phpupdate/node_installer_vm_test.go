package phpupdate

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// Read-only installer admission against a new isolated fixture database. It
// never changes the panel, a runtime, service, cron or existing VM fixture.
func TestVMInstallerNodeTrustRefreshAdmission(t *testing.T) {
	requireRecoveryVM(t)
	dir, err := os.MkdirTemp("/var/lib", "fluxo-node-installer-gate-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	db, err := sql.Open("sqlite", filepath.Join(dir, "fluxo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE node_update_generations(id TEXT,state TEXT,payload TEXT);
		CREATE TABLE node_update_operations(id TEXT,status TEXT);
		CREATE TABLE node_update_barriers(unit TEXT,generation_id TEXT);
		INSERT INTO node_update_generations VALUES('old','succeeded','{"retained":{"trust_revision":"historical-keyring-A"}}');`)
	if err != nil {
		t.Fatal(err)
	}
	if err = CheckInstallerDatabase(context.Background(), dir); err != nil {
		t.Fatal("completed historical Node proof blocked a trusted panel upgrade", err)
	}
	for _, state := range []string{"prepared", "recovery_required"} {
		if _, err = db.Exec("UPDATE node_update_generations SET state=?", state); err != nil {
			t.Fatal(err)
		}
		if err = CheckInstallerDatabase(context.Background(), dir); err == nil {
			t.Fatal("pending Node generation permitted installer", state)
		}
	}
	if _, err = db.Exec("UPDATE node_update_generations SET state='succeeded'; INSERT INTO node_update_barriers VALUES('unit','old')"); err != nil {
		t.Fatal(err)
	}
	if err = CheckInstallerDatabase(context.Background(), dir); err == nil {
		t.Fatal("leftover Node barrier permitted installer")
	}
	if _, err = db.Exec("DELETE FROM node_update_barriers; INSERT INTO node_update_operations VALUES('new','queued')"); err != nil {
		t.Fatal(err)
	}
	if err = CheckInstallerDatabase(context.Background(), dir); err == nil {
		t.Fatal("queued Node proof refresh permitted installer")
	}
}
