package postgres

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRolePasswordSQLKeepsPasswordOutOfSQLSyntax(t *testing.T) {
	password := `abc\' REQUIRE SUPERUSER --`
	statement, err := rolePasswordSQL("CREATE", "app_user", password, false)
	if err != nil {
		t.Fatalf("rolePasswordSQL() error: %v", err)
	}
	if strings.Contains(statement, password) {
		t.Fatal("plaintext password was interpolated into SQL")
	}
	if !strings.Contains(statement, base64.StdEncoding.EncodeToString([]byte(password))) {
		t.Fatal("encoded password payload is missing")
	}
	if !strings.Contains(statement, "format('CREATE ROLE %I WITH LOGIN PASSWORD %L'") {
		t.Fatalf("statement does not use PostgreSQL identifier/literal formatting: %s", statement)
	}
	if strings.Contains(statement, "SUPERUSER") {
		t.Fatal("ordinary application role unexpectedly receives SUPERUSER")
	}
}

func TestCreateRolePreservesQuoteAndBackslashPassword(t *testing.T) {
	if os.Getenv("FLUXO_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set FLUXO_POSTGRES_INTEGRATION=1 on a disposable PostgreSQL host")
	}
	const user = "fx_codex_pg_bind_audit"
	const password = `abc\' SUPERUSER --still-password`
	_ = DropRole(user)
	t.Cleanup(func() { _ = DropRole(user) })

	if err := CreateRole(user, password); err != nil {
		t.Fatalf("CreateRole() error: %v", err)
	}
	if err := VerifyDatabaseAccess("postgres", user, password); err != nil {
		t.Fatalf("created password did not authenticate exactly: %v", err)
	}
	const rotatedPassword = `rotated\' CREATEDB --still-password`
	if err := UpdateRolePassword(user, rotatedPassword); err != nil {
		t.Fatalf("UpdateRolePassword() error: %v", err)
	}
	if err := VerifyDatabaseAccess("postgres", user, rotatedPassword); err != nil {
		t.Fatalf("rotated password did not authenticate exactly: %v", err)
	}
	out, err := runPSQL(t.Context(), 10*time.Second, "", "SELECT rolsuper FROM pg_roles WHERE rolname = '"+user+"';")
	if err != nil {
		t.Fatalf("inspect created role: %v", err)
	}
	if strings.TrimSpace(out) != "f" {
		t.Fatalf("password escaped into role syntax; rolsuper = %q", strings.TrimSpace(out))
	}
}

func TestRolePasswordSQLRejectsInvalidInputs(t *testing.T) {
	for _, test := range []struct {
		action, user, password string
	}{
		{action: "DROP", user: "app_user", password: "secret"},
		{action: "CREATE", user: "bad user", password: "secret"},
		{action: "CREATE", user: "app_user", password: ""},
		{action: "CREATE", user: "app_user", password: "bad\npassword"},
	} {
		if _, err := rolePasswordSQL(test.action, test.user, test.password, false); err == nil {
			t.Fatalf("expected invalid input to be rejected: %#v", test)
		}
	}
}
