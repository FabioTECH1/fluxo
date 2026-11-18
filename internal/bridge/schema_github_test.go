package bridge

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/database"
)

func TestGitHubMetadataUpgradePreservesExistingConnections(t *testing.T) {
	// The preceding schema has neither token inspection metadata nor a
	// recorded disconnect owner. Migration must not invent either result.
	ddl := database.BaseSchema
	for _, column := range []string{
		"github_disconnected_username TEXT NOT NULL DEFAULT ''",
		"token_expires_at TEXT NOT NULL DEFAULT ''",
		"token_status TEXT NOT NULL DEFAULT 'unchecked'",
		"token_checked_at TEXT NOT NULL DEFAULT ''",
		"deployment_access TEXT NOT NULL DEFAULT '{}'",
	} {
		line := "\t\t" + column + ",\n"
		if strings.Count(ddl, line) != 1 {
			t.Fatalf("expected exactly one current declaration for %s", column)
		}
		ddl = strings.Replace(ddl, line, "", 1)
	}
	path := filepath.Join(t.TempDir(), "existing.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec(ddl); err == nil {
		_, err = old.Exec(`INSERT INTO github_accounts(id,name,username,token) VALUES(7,'Existing','fixture-owner','opaque-stored-credential');
		INSERT INTO sites(id,domain,path,repository,github_account_id) VALUES(9,'connected.test','/srv/connected','fixture-owner/repo',7),(10,'orphaned.test','/srv/orphaned','fixture-owner/old',0)`)
	}
	closeErr := old.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	previous := database.DB
	if err = database.InitDB(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.DB.Close(); database.DB = previous })
	if err = checkSchema(context.Background(), database.DB); err != nil {
		t.Fatal(err)
	}
	var name, username, token, expires, status, checked, access string
	err = database.DB.QueryRow(`SELECT name,username,token,token_expires_at,token_status,token_checked_at,deployment_access FROM github_accounts WHERE id=7`).Scan(&name, &username, &token, &expires, &status, &checked, &access)
	if err != nil || name != "Existing" || username != "fixture-owner" || token != "opaque-stored-credential" || expires != "" || status != "unchecked" || checked != "" || access != "{}" {
		t.Fatal("connection or default inspection metadata changed", err)
	}
	for id, account := range map[int]int{9: 7, 10: 0} {
		var got int
		var marker string
		err = database.DB.QueryRow("SELECT github_account_id,github_disconnected_username FROM sites WHERE id=?", id).Scan(&got, &marker)
		if err != nil || got != account || marker != "" {
			t.Fatal("site connection changed or orphan ownership was invented", id, err)
		}
	}
}
