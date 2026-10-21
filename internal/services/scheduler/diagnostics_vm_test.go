package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVMDiagnosticsVerifyDatabaseAndPreserveEvidence(t *testing.T) {
	e, _ := vmFixture(t)
	ctx := context.Background()
	before, err := os.ReadFile(filepath.Join(stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	bundle := DiagnosticBundle(ctx)
	var registration map[string]any
	for _, value := range bundle["registrations"].([]any) {
		item := value.(map[string]any)
		if item["id"] == e.ID {
			registration = item
		}
	}
	if registration == nil || registration["committed_database_binding"] != "matched" || registration["database_path_matches"] != true {
		t.Fatal("unverified committed binding", registration)
	}
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("DELETE FROM crons WHERE id=?", e.ID); err != nil {
		t.Fatal(err)
	}
	const custom = "# PRIVATE_CUSTOM_COMMAND_SENTINEL\n"
	if err = os.WriteFile(CronPath(e.ID), []byte(custom), 0644); err != nil {
		t.Fatal(err)
	}
	bundle = DiagnosticBundle(ctx)
	registration = bundle["registrations"].([]any)[0].(map[string]any)
	if registration["database_path_matches"] != true || registration["committed_database_binding"] != "mismatch" {
		t.Fatal("path equality concealed deleted row", registration)
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "PRIVATE_CUSTOM_COMMAND_SENTINEL") || strings.Contains(string(raw), e.Command) {
		t.Fatal("diagnostics exposed command content")
	}
	after, err := os.ReadFile(filepath.Join(stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("diagnostic collection mutated the journal")
	}
	trigger, _ := os.ReadFile(CronPath(e.ID))
	if string(trigger) != custom {
		t.Fatal("diagnostics changed a custom trigger")
	}
}
