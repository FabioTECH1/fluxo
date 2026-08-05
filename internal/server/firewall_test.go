package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"fluxo/internal/database"
)

func TestDeleteFirewallRuleProtectsInstallerManagedRules(t *testing.T) {
	previousDB := database.DB
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	t.Cleanup(func() {
		_ = database.DB.Close()
		database.DB = previousDB
	})

	result, err := database.DB.Exec(`INSERT INTO firewall_rules
		(name, rule_type, port, from_ip, managed_by)
		VALUES ('SSH', 'allow', '22/tcp', 'Any', 'installer')`)
	if err != nil {
		t.Fatalf("insert installer firewall rule: %v", err)
	}
	id, _ := result.LastInsertId()

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/firewall/1", nil)
	request.SetPathValue("id", "1")
	recorder := httptest.NewRecorder()
	(&Server{}).handleDeleteFirewallRule().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("delete status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	var count int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM firewall_rules WHERE id = ?", id).Scan(&count); err != nil {
		t.Fatalf("count protected firewall rule: %v", err)
	}
	if count != 1 {
		t.Fatal("installer-managed firewall rule was deleted")
	}
}
