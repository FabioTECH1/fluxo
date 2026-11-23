package database

import (
	"context"
	"testing"
)

func TestWordPressCronAdditiveStoragePreservesIdentityAndRejectsDamage(t *testing.T) {
	file := networkDatabase(t)
	var before string
	DB.QueryRow("SELECT value FROM system_metadata WHERE key='installation_id'").Scan(&before)
	if _, err := DB.Exec("DROP TABLE wordpress_cron_settings"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCurrentSchema(context.Background(), DB); err != nil {
		t.Fatal("old complete base refused", err)
	}
	DB.Close()
	if err := InitBaseDB(file); err != nil {
		t.Fatal(err)
	}
	var after string
	DB.QueryRow("SELECT value FROM system_metadata WHERE key='installation_id'").Scan(&after)
	if before == "" || after != before {
		t.Fatal("identity changed")
	}
	if _, err := DB.Exec("DROP TABLE wordpress_cron_settings; CREATE TABLE wordpress_cron_settings(site_id INTEGER PRIMARY KEY, cron_id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCurrentSchema(context.Background(), DB); err == nil {
		t.Fatal("partial feature state accepted")
	}
}
