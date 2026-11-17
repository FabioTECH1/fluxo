package mysql

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
)

func TestBridgeLiteralGrantAndAuthenticationProfiles(t *testing.T) {
	if grantDatabasePatternForMode("app_db", false) != `app\_db` || grantDatabasePatternForMode("app_db", true) != "app_db" {
		t.Fatal("literal schema matching mode changed")
	}
	good := []string{"GRANT USAGE ON *.* TO `app`@`127.0.0.1`", `GRANT ALL PRIVILEGES ON ` + "`app\\_db`" + `.* TO ` + "`app`@`127.0.0.1`"}
	if err := bridgeGrants("app_db", good, false); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]string{
		{"GRANT ALL PRIVILEGES ON *.* TO `app`@`127.0.0.1`"},
		{"GRANT ALL PRIVILEGES ON `app_db`.* TO `app`@`127.0.0.1`"},
		{"GRANT ALL PRIVILEGES ON `app%`.* TO `app`@`127.0.0.1`"},
		{good[1] + " WITH GRANT OPTION"},
		{"GRANT ALL PRIVILEGES ON `other`.* TO `app`@`127.0.0.1`"},
		{"GRANT `elevated_role` TO `app`@`127.0.0.1`"},
	} {
		if bridgeGrants("app_db", bad, false) == nil {
			t.Fatal("overbroad/missing database grant accepted")
		}
	}
	if err := bridgeGrants("app_db", []string{"GRANT ALL PRIVILEGES ON `app_db`.* TO `app`@`127.0.0.1`"}, true); err != nil {
		t.Fatal("partial-revokes literal mode refused", err)
	}
	if !bridgeAuthentication("mysql_native_password", "*"+strings.Repeat("A", 40)) {
		t.Fatal("standard native authentication refused")
	}
	for _, bad := range []string{"", "*short", "plaintext"} {
		if bridgeAuthentication("mysql_native_password", bad) {
			t.Fatal("invalid authentication hash accepted")
		}
	}
	if bridgeAuthentication("unix_socket", "*"+strings.Repeat("A", 40)) {
		t.Fatal("socket-only application credential admitted")
	}
}

func TestVMBridgeOneTimeCredentialsAndLiteralSchemaGrants(t *testing.T) {
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || os.Getenv("FLUXO_MYSQL_INTEGRATION") != "1" || (host != "fluxo-bridge-prod-20261101" && host != "fluxo-bridge-jammy-20261101") {
		t.Skip("private disposable bridge database VM only")
	}
	var random [5]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	name := "fx_bridge_" + hex.EncodeToString(random[:])
	shadow := strings.ReplaceAll(name, "_", "x")
	user := name
	password := "fixture-" + hex.EncodeToString(random[:]) + "-not-retained"
	if err := CreateDatabase(name, user, password); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = DropManagedLocalUser(user); _ = DeleteDatabase(name); _ = DeleteDatabase(shadow) })
	if err := CreateDatabaseOnly(shadow); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := BridgeDatabaseReady(ctx, name, user, false); err != nil {
		t.Fatal("one-time credential catalog validation failed", err)
	}
	cfg := driver.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = LocalTCPHost + ":3306"
	cfg.DBName = name
	app, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err = app.PingContext(ctx); err != nil {
		t.Fatal("dedicated password changed", err)
	}
	if _, err = app.ExecContext(ctx, "CREATE TABLE `"+shadow+"`.forbidden(id INTEGER)"); err == nil {
		t.Fatal("underscore wildcard leaked access to a different schema")
	}
	admin, err := openAdmin()
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	// Reproduce a legacy grant and require explicit selected-scope repair.
	if _, err = admin.Exec("GRANT ALL PRIVILEGES ON `" + name + "`.* TO '" + user + "'@'" + LocalTCPHost + "'"); err != nil {
		t.Fatal(err)
	}
	if BridgeDatabaseReady(ctx, name, user, false) == nil {
		t.Fatal("legacy wildcard grant qualified")
	}
	if err = ReplaceDatabaseAccess(user, []string{name}); err != nil {
		t.Fatal(err)
	}
	if err = BridgeDatabaseReady(ctx, name, user, false); err != nil {
		t.Fatal("explicit safe grant repair failed", err)
	}
	if err = VerifyDatabaseAccess(name, user, password); err != nil {
		t.Fatal("grant repair changed one-time credentials", err)
	}
	if _, err = app.ExecContext(ctx, "CREATE TABLE `"+shadow+"`.forbidden(id INTEGER)"); err == nil {
		t.Fatal("grant repair retained wildcard access")
	}
	if _, err = admin.Exec("GRANT SELECT ON *.* TO '" + user + "'@'" + LocalTCPHost + "'"); err != nil {
		t.Fatal(err)
	}
	if BridgeDatabaseReady(ctx, name, user, false) == nil {
		t.Fatal("global application privilege admitted")
	}
}

func TestBridgeRejectsSystemSchemasAndDelegationOnUsage(t *testing.T) {
	for _, grants := range [][]string{
		{"GRANT USAGE ON *.* TO `app`@`127.0.0.1` WITH GRANT OPTION", "GRANT ALL PRIVILEGES ON `app`.* TO `app`@`127.0.0.1`"},
		{"GRANT ALL PRIVILEGES ON `app`.* TO `app`@`127.0.0.1`", "GRANT SELECT ON `mysql`.* TO `app`@`127.0.0.1`"},
	} {
		if bridgeGrants("app", grants, false) == nil {
			t.Fatal("system or delegation authority admitted")
		}
	}
}
