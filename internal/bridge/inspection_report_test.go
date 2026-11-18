package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestInspectionCollectsIndependentFailuresAndBlocksDependentProbes(t *testing.T) {
	var calls []string
	probe := func(name string, fail bool) func() error {
		return func() error {
			calls = append(calls, name)
			if fail {
				return errors.New(name + " needs repair")
			}
			return nil
		}
	}
	checks := collectReadiness([]readinessProbe{
		{"service", nil, probe("service", true)},
		{"unsafe_probe", []string{"service"}, func() error { t.Fatal("failed prerequisite still executed dependent probe"); return nil }},
		{"schema", nil, probe("schema", true)},
		{"key", nil, probe("key", false)},
		{"records", []string{"schema", "key"}, func() error { t.Fatal("untrusted schema still queried application records"); return nil }},
		{"missing_prerequisite", []string{"unknown"}, nil},
	})
	if strings.Join(calls, ",") != "service,schema,key" {
		t.Fatal(calls)
	}
	if !checks[1].Blocked || !checks[4].Blocked || !checks[5].Blocked || !checks[3].Passed || checks[0].Blocked || checks[2].Blocked {
		t.Fatal(checks)
	}
	err := readinessFailures(checks)
	if err == nil || !strings.Contains(err.Error(), "service needs repair") || !strings.Contains(err.Error(), "schema needs repair") {
		t.Fatal(err)
	}
	r := newReport()
	r.Checks = checks
	r.finish()
	if r.Ready {
		t.Fatal("partial or blocked inspection granted readiness")
	}
	raw, err := json.Marshal(r)
	if err != nil || !strings.Contains(string(raw), `"blocked":true`) {
		t.Fatalf("blocked status missing: %s %v", raw, err)
	}
}

func TestInspectionReportsMultipleSchemaObjectsWithoutChangingThem(t *testing.T) {
	db := fixture(t)
	if _, err := db.Exec(`ALTER TABLE sites ADD COLUMN unexpected_site TEXT;
		ALTER TABLE backup_destinations ADD COLUMN unexpected_backup TEXT;
		DROP INDEX idx_sites_app_port;
		DROP TRIGGER prevent_panel_domain_conflict`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	before := vmQueryFingerprint(t, db, "SELECT type,name,tbl_name,sql FROM sqlite_master ORDER BY type,name")
	strict := checkSchema(ctx, db)
	if strict == nil || strings.Contains(strict.Error(), "\n") {
		t.Fatal("strict admission no longer stops at its original first failure", strict)
	}
	err := checkSchemaMode(ctx, db, true)
	for _, name := range []string{"sites", "backup_destinations", "idx_sites_app_port", "prevent_panel_domain_conflict"} {
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("missing schema failure %s: %v", name, err)
		}
	}
	if after := vmQueryFingerprint(t, db, "SELECT type,name,tbl_name,sql FROM sqlite_master ORDER BY type,name"); before != after {
		t.Fatal("schema inspection changed metadata")
	}
}

func TestInspectionReportsIndependentConversionAndPendingFailures(t *testing.T) {
	db := fixture(t)
	if _, err := db.Exec(`INSERT INTO users(username,token_hash,github_pat) VALUES('admin','hash','private-legacy-token');
		CREATE TABLE databases_next(id INTEGER);
		INSERT INTO node_update_operations VALUES('op','apply','','request','running',1,'{}');
		INSERT INTO node_update_generations VALUES('gen','op','succeeded','{}');
		INSERT INTO node_update_barriers VALUES('unit','gen')`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	err := checkConversionsMode(ctx, db, true)
	if err == nil || !strings.Contains(err.Error(), "conversion tables") || !strings.Contains(err.Error(), "legacy settings PAT") || strings.Contains(err.Error(), "private-legacy-token") {
		t.Fatal("conversion failures missing or private data exposed", err)
	}
	err = checkPendingMode(ctx, db, true)
	if err == nil || !strings.Contains(err.Error(), "node_update_operations") || !strings.Contains(err.Error(), "node_update_barriers") {
		t.Fatal("pending failures missing", err)
	}
	for _, err := range []error{checkConversions(ctx, db), checkPending(ctx, db)} {
		if err == nil || strings.Contains(err.Error(), "\n") {
			t.Fatal("strict admission changed", err)
		}
	}
}

func TestInspectionReadyRequiresEveryCheckToPass(t *testing.T) {
	r := newReport()
	r.Checks = collectReadiness([]readinessProbe{{"a", nil, func() error { return nil }}, {"b", []string{"a"}, func() error { return nil }}})
	r.finish()
	if !r.Ready || readinessFailures(r.Checks) != nil {
		t.Fatal(r)
	}
	for _, c := range r.Checks {
		if c.Blocked || !c.Passed {
			t.Fatal(c)
		}
	}
	for _, detail := range []Check{{Component: "pending", Passed: false}, {Component: "blocked", Passed: true, Blocked: true}} {
		r.Checks = []Check{{Component: "host", Passed: true, Details: []Check{detail}}}
		r.finish()
		if r.Ready {
			t.Fatal("nested failure or blocked check granted readiness", r)
		}
	}
}

func TestInspectionMissingKeyOrSchemaDoesNotHideIndependentHostChecks(t *testing.T) {
	checks := collectReadinessAfter([]readinessProbe{
		{"service", nil, func() error { return nil }},
		{"package_state", []string{"service"}, func() error { return nil }},
		{"sites", []string{"service", "schema"}, func() error { t.Fatal("queried unsupported schema"); return nil }},
		{"database_access", []string{"schema", "encryption_key"}, func() error { t.Fatal("used unavailable key"); return nil }},
	}, map[string]bool{"schema": false, "encryption_key": false})
	if !checks[0].Passed || !checks[1].Passed || !checks[2].Blocked || !checks[3].Blocked {
		t.Fatal(checks)
	}
}
