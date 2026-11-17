package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"fluxo/internal/syscmd"
)

func TestVMBridgeCatalogIsIndependentOfApplicationSearchPath(t *testing.T) {
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || os.Getenv("FLUXO_POSTGRES_INTEGRATION") != "1" || (host != "fluxo-bridge-prod-20261101" && host != "fluxo-bridge-jammy-20261101") {
		t.Skip("private disposable bridge database VM only")
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir("/"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	var random [5]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	name := "fx_bridge_" + hex.EncodeToString(random[:])
	user := name
	password := "fixture-" + hex.EncodeToString(random[:]) + "-not-retained"
	if err := CreateDatabase(name, user, password); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = DeleteDatabase(name); _ = DropRole(user) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := BridgeDatabaseReady(ctx, name, user, false); err != nil {
		t.Fatal("one-time credentials rejected", err)
	}
	statement := `CREATE FUNCTION public.has_schema_privilege(name,text,text) RETURNS boolean LANGUAGE plpgsql AS $body$ BEGIN RAISE EXCEPTION 'application-controlled function must never run during privileged bridge inspection'; END $body$; ALTER DATABASE "` + name + `" SET search_path=public,pg_catalog;`
	if _, err := syscmd.RunEnv(ctx, 10*time.Second, []string{"PGPASSWORD=" + password, "PGCONNECT_TIMEOUT=5"}, "/usr/bin/psql", "-X", "-h", "127.0.0.1", "-U", user, "-d", name, "-v", "ON_ERROR_STOP=1", "-c", statement); err != nil {
		t.Fatal(err)
	}
	if err := BridgeDatabaseReady(ctx, name, user, false); err != nil {
		t.Fatal("privileged catalog inspection followed application search_path", err)
	}
	if err := VerifyDatabaseAccess(name, user, password); err != nil {
		t.Fatal("inspection changed application password", err)
	}
	for _, target := range []string{"ROLE \"" + user + "\"", "DATABASE \"" + name + "\""} {
		if _, err := runPSQL(ctx, 5*time.Second, "postgres", "ALTER "+target+" CONNECTION LIMIT 0;"); err != nil {
			t.Fatal(err)
		}
		if BridgeDatabaseReady(ctx, name, user, false) == nil {
			t.Fatal("disabled connection limit admitted", target)
		}
		if _, err := runPSQL(ctx, 5*time.Second, "postgres", "ALTER "+target+" CONNECTION LIMIT -1;"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runPSQL(ctx, 5*time.Second, "postgres", "GRANT pg_read_server_files TO \""+user+"\";"); err != nil {
		t.Fatal(err)
	}
	if BridgeDatabaseReady(ctx, name, user, false) == nil {
		t.Fatal("privileged inherited role admitted")
	}
}
