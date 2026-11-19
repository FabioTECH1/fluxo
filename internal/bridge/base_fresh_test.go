package bridge

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/services/executiondomain"
)

func TestFreshBaseIntentRequiresExactRecoveryIdentity(t *testing.T) {
	pending := BaseFreshIntent{Format: 1, ImageSHA256: strings.Repeat("a", 64), Version: "0.0.0-base-test", Phase: "pending"}
	raw, _ := json.Marshal(pending)
	if _, err := decodeBaseFreshIntent(raw); err != nil {
		t.Fatal(err)
	}
	complete := pending
	complete.Phase, complete.InstallationID, complete.OperationID, complete.PlanSHA256 = "complete", strings.Repeat("b", 32), "1af605c7-82ca-4245-9fc5-1240cae7d8ab", strings.Repeat("c", 64)
	raw, _ = json.Marshal(complete)
	if _, err := decodeBaseFreshIntent(raw); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []func(*BaseFreshIntent){
		func(v *BaseFreshIntent) { v.Phase = "arbitrary" },
		func(v *BaseFreshIntent) { v.OperationID = "" },
		func(v *BaseFreshIntent) { v.PlanSHA256 = strings.Repeat("A", 64) },
		func(v *BaseFreshIntent) { v.ImageSHA256 = "0.4.51" },
		func(v *BaseFreshIntent) { v.InstallationID = strings.Repeat("b", 64) },
		func(v *BaseFreshIntent) { v.Phase = "pending" },
	} {
		changed := complete
		mutation(&changed)
		raw, _ = json.Marshal(changed)
		if _, err := decodeBaseFreshIntent(raw); err == nil {
			t.Fatal("invalid fresh authority accepted", changed)
		}
	}
	for _, raw := range []string{
		`{"format":1,"image_sha256":"` + pending.ImageSHA256 + `","version":"0.0.0-base-test","phase":"pending"}`,
		`{"format":1,"image_sha256":"` + pending.ImageSHA256 + `","version":"0.0.0-base-test","phase":"pending","installation_id":null,"operation_id":"","plan_sha256":""}`,
	} {
		if _, err := decodeBaseFreshIntent([]byte(raw)); err == nil {
			t.Fatal("partial marker accepted")
		}
	}
	if requireFreshImage(pending, pending.ImageSHA256, pending.Version) != nil || requireFreshImage(pending, strings.Repeat("b", 64), pending.Version) == nil || requireFreshImage(pending, pending.ImageSHA256, "0.4.51") == nil {
		t.Fatal("fresh retry did not require the exact executing artifact")
	}
}

func TestFreshBaseRefusesEveryPartialRuntimeArtifact(t *testing.T) {
	absent := func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	noMatches := func(string) ([]string, error) { return nil, nil }
	if err := freshRuntimeAbsent(absent, noMatches); err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"/var/lib/fluxo-scheduler", "/var/lib/fluxo-scheduler.initialized", "/var/lib/fluxo-scheduler.init.lock", "/var/lib/fluxo-system-tasks", "/var/lib/fluxo-system-tasks.initialized", "/var/lib/fluxo-system-tasks.lock"} {
		if err := freshRuntimeAbsent(func(path string) (os.FileInfo, error) {
			if path == marker {
				return nil, nil // lstat existence, including a dangling symlink
			}
			return absent(path)
		}, noMatches); err == nil {
			t.Fatal("partial runtime considered new", marker)
		}
	}
	if err := freshRuntimeAbsent(func(string) (os.FileInfo, error) { return nil, os.ErrPermission }, noMatches); err == nil {
		t.Fatal("unreadable state considered absent")
	}
	if err := freshRuntimeAbsent(absent, func(string) ([]string, error) { return []string{"dangling-old-trigger"}, nil }); err == nil {
		t.Fatal("trigger-only installation considered new")
	}
}

func TestFreshBaseRetryOnlyAllowsEmptyCoordinationLock(t *testing.T) {
	lock := "/var/lib/fluxo-scheduler.init.lock"
	lstat := func(path string) (os.FileInfo, error) {
		if path == lock {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	glob := func(string) ([]string, error) { return nil, nil }
	read := func(string, int64) ([]byte, error) { return nil, nil }
	if err := freshInitializationRuntimeAbsent(lstat, glob, read); err != nil {
		t.Fatal("empty coordination-only residue blocked exact fresh retry", err)
	}
	for _, read := range []func(string, int64) ([]byte, error){
		func(string, int64) ([]byte, error) { return []byte("authority"), nil },
		func(string, int64) ([]byte, error) { return nil, os.ErrPermission },
	} {
		if err := freshInitializationRuntimeAbsent(lstat, glob, read); err == nil {
			t.Fatal("fresh retry adopted changed/unsafe coordination evidence")
		}
	}
}

func freshCreationFixture() executionActivation {
	p := absenceFixture(true, true)
	p.Format, p.Kind, p.TargetSchedulerSchema, p.TargetSystemTaskSchema = 4, activationFresh, 5, 3
	for i := range p.Files {
		if p.Files[i].Path == "/var/lib/fluxo-scheduler/state.json" {
			p.Files[i].Data = []byte(`{"loader_epochs_established":false,"last_sweep":{},"schema":5,"entries":{},"invocations":{},"holds":{},"released":{},"upgrade":null,"legacy":{},"migrations":{},"panel_proof":null}`)
		}
	}
	p.Files = append(p.Files,
		executiondomain.ActivationFile{Path: "/var/lib/fluxo-scheduler/helpers", Before: "absent", Directory: true, Mode: 0700},
		executiondomain.ActivationFile{Path: "/var/lib/fluxo-scheduler/helpers/" + p.BinarySHA256, Before: "absent", Image: true, Mode: 0700})
	return p
}

func TestFreshBaseOnlyPublishesCurrentEmptyCores(t *testing.T) {
	if err := validateFreshCreationPlan(freshCreationFixture()); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*executionActivation){
		func(p *executionActivation) { p.Kind = activationHistoricalRetirement },
		func(p *executionActivation) { p.Files[0].Before = strings.Repeat("a", 64) },
		func(p *executionActivation) {
			for i := range p.Files {
				if p.Files[i].Path == "/var/lib/fluxo-scheduler/state.json" {
					p.Files[i].Data = []byte(`{"schema":2}`)
				}
			}
		},
		func(p *executionActivation) {
			for i := range p.Files {
				if p.Files[i].Path == "/var/lib/fluxo-system-tasks/state.json" {
					p.Files[i].Data = []byte(`{"schema":3,"entries":{},"invocations":{"history":{}}}`)
				}
			}
		},
	} {
		p := freshCreationFixture()
		mutate(&p)
		if validateFreshCreationPlan(p) == nil {
			t.Fatal("fresh origin adopted existing or noncurrent execution state")
		}
	}
}

func TestFreshBaseDatabaseCannotAdoptHistory(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE system_metadata(key TEXT,value TEXT); INSERT INTO system_metadata VALUES('installation_id','identity'); CREATE TABLE retained_history(id INTEGER);`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = checkFreshDatabaseEmpty(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO retained_history VALUES(42)"); err != nil {
		t.Fatal(err)
	}
	if err = checkFreshDatabaseEmpty(ctx, db); err == nil {
		t.Fatal("history was adopted as empty")
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM retained_history WHERE id=42").Scan(&count); err != nil || count != 1 {
		t.Fatal("refusal changed retained history", err)
	}
}

func TestFreshBaseAcceptsOnlyCanonicalSeedRows(t *testing.T) {
	db := fixture(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO system_metadata(key,value) VALUES('installation_id','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')`); err != nil {
		t.Fatal(err)
	}
	if err := checkFreshDatabaseEmpty(ctx, db); err != nil {
		t.Fatal("canonical fresh schema refused", err)
	}
	for _, change := range []string{
		`UPDATE panel_domain SET domain='retained.example'`,
		`UPDATE php_update_schedule SET next_check=100`,
		`UPDATE node_update_snapshot SET payload='{"retained":true}'`,
	} {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(change); err != nil {
			t.Fatal(err)
		}
		if err = checkFreshDatabaseEmpty(ctx, tx); err == nil {
			t.Fatal("non-fresh data admitted", change)
		}
		tx.Rollback()
	}
}
