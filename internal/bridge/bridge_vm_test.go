package bridge

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/packages"
)

func TestVMBridgeCertifyingBinaryIdentity(t *testing.T) {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("guarded retained acceptance VM only")
	}
	if os.Getenv("FLUXO_BRIDGE_IMAGE_TEST_CHILD") == "1" {
		path, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path+".next", []byte("different installed image"), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.Rename(path+".next", path); err != nil {
			t.Fatal(err)
		}
		if _, err = readCertifyingExecutable(); err == nil {
			t.Fatal("receipt would bind a replaced pathname instead of the executing image")
		}
		return
	}
	data, err := readCertifyingExecutable()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("/root", "fluxo-bridge-image-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "private-test-image")
	if err = os.WriteFile(path, data, 0700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(path, "-test.run=^TestVMBridgeCertifyingBinaryIdentity$", "-test.v")
	command.Env = append(os.Environ(), "FLUXO_BRIDGE_IMAGE_TEST_CHILD=1")
	if out, e := command.CombinedOutput(); e != nil {
		t.Fatalf("replacement rejection: %v %s", e, out)
	}
}

func TestVMBridgeHostGraphAndCancelledCertification(t *testing.T) {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("guarded retained acceptance VM only")
	}
	files := map[string][]string{}
	if err := readNginxGraph("/etc/nginx/nginx.conf", nil, files, map[string]string{}, map[string]bool{}); err != nil {
		t.Fatal("real installed Nginx include graph", err)
	}
	if _, ok := files["/etc/nginx/nginx.conf"]; !ok {
		t.Fatal("master configuration absent")
	}
	dir, err := os.MkdirTemp("/root", "fluxo-bridge-host-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	available := filepath.Join(dir, "available.conf")
	if err = os.WriteFile(available, []byte("server { listen 8080; server_name isolated.test; root /isolated; }"), 0600); err != nil {
		t.Fatal(err)
	}
	master := filepath.Join(dir, "nginx.conf")
	if err = os.WriteFile(master, []byte("http { include "+filepath.Join(dir, "enabled.conf")+"; }"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(available, filepath.Join(dir, "enabled.conf")); err != nil {
		t.Fatal(err)
	}
	files = map[string][]string{}
	if err = readNginxGraph(master, nil, files, map[string]string{}, map[string]bool{}); err != nil || !nginxHTTPIncluded(files, available) {
		t.Fatal("enabled private fixture missing", err)
	}
	if err = os.Remove(filepath.Join(dir, "enabled.conf")); err != nil {
		t.Fatal(err)
	}
	files = map[string][]string{}
	if err = readNginxGraph(master, nil, files, map[string]string{}, map[string]bool{}); err == nil && nginxHTTPIncluded(files, available) {
		t.Fatal("disabled private fixture accepted")
	}
	previous := database.DB
	if err = database.InitDB(filepath.Join(dir, "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer func() { database.DB.Close(); database.DB = previous }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	unlock, err := packages.Lifecycle(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	blocked, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stop()
	report, err := Certify(blocked, dir, "0.0.0-bridge-test")
	if err == nil || report.Ready || report.Recorded {
		t.Fatal("contended certification ignored cancellation", err)
	}
	var count int
	if err = database.DB.QueryRow("SELECT count(*) FROM system_metadata WHERE key=?", receiptKey).Scan(&count); err != nil || count != 0 {
		t.Fatal("cancelled certification recorded success", err)
	}
}

// Exercise an actual retained historical schema on a private copy. Never run
// initialization or conversions on the retained source installation itself.
func TestVMBridgeHistoricalCopyPreparation(t *testing.T) {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("guarded retained acceptance VM only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	const source = "/var/lib/fluxo"
	before := map[string]string{}
	for _, name := range []string{"fluxo.db", "fluxo.db-wal", "fluxo.db-shm"} {
		b, err := os.ReadFile(filepath.Join(source, name))
		if err == nil {
			before[name] = digest(b)
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	path, cleanup, err := snapshot(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	db, err := openSQLite(ctx, path, false)
	if err != nil {
		t.Fatal(err)
	}
	preserve := map[string]string{}
	for _, query := range []string{
		"SELECT id,domain,path,deploy_script FROM sites ORDER BY id",
		"SELECT id,site_id,command FROM daemons ORDER BY id",
		"SELECT id,site_id,command FROM crons ORDER BY id",
		"SELECT id,site_id,status FROM deployments ORDER BY id",
		"SELECT value FROM system_metadata WHERE key='installation_id'",
	} {
		preserve[query] = vmQueryFingerprint(t, db, query)
	}
	db.Close()
	previous := database.DB
	if err = database.InitDB(path); err != nil {
		t.Fatal("historical copy preparation", err)
	}
	defer func() { database.DB.Close(); database.DB = previous }()
	if err = checkSchema(ctx, database.DB); err != nil {
		t.Fatal("prepared historical copy does not satisfy canonical contract", err)
	}
	for query, want := range preserve {
		if got := vmQueryFingerprint(t, database.DB, query); got != want {
			t.Fatal("preparation changed historical identity or user intent", query)
		}
	}
	for name, want := range before {
		b, err := os.ReadFile(filepath.Join(source, name))
		if err != nil || digest(b) != want {
			t.Fatal("historical preparation changed retained source", name, err)
		}
	}
}

func vmQueryFingerprint(t *testing.T, db *sql.DB, query string) string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var data [][]any
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err = rows.Scan(dest...); err != nil {
			t.Fatal(err)
		}
		data = append(data, values)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return digest(b)
}

// This touches only a new private fixture under /root. It never stops the
// retained VM's panel, modifies its database or changes its package fixtures.
func TestVMBridgeInspectionPreservesWALSource(t *testing.T) {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("guarded retained acceptance VM only")
	}
	dir, err := os.MkdirTemp("/root", "fluxo-bridge-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	previous := database.DB
	if err = database.InitDB(filepath.Join(dir, "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer func() { database.DB.Close(); database.DB = previous }()
	if _, err = database.DB.Exec("PRAGMA wal_autocheckpoint=0; INSERT INTO activity(type,summary) VALUES('fixture','committed in WAL')"); err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, name := range []string{"fluxo.db", "fluxo.db-wal", "fluxo.db-shm"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = digest(b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	report, err := Inspect(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if report.Ready || report.Recorded {
		t.Fatal("fixture without credentials/key was certified")
	}
	for name, want := range before {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || digest(b) != want {
			t.Fatal("inspection changed original SQLite state", name, err)
		}
	}
	if _, err = os.Lstat(filepath.Join(dir, "encryption.key")); !os.IsNotExist(err) {
		t.Fatal("inspection created a replacement key")
	}
	var n int
	if err = database.DB.QueryRow("SELECT count(*) FROM system_metadata WHERE key=?", receiptKey).Scan(&n); err != nil || n != 0 {
		t.Fatal("inspection recorded completion", err)
	}
}
