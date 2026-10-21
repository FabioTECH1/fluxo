package phpupdate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/scheduler"
	"github.com/google/uuid"
)

func TestPackageDiscoveryIgnoresUnsupportedAndStoppedApplicationServices(t *testing.T) {
	testStore(t)
	_, err := database.DB.Exec("INSERT INTO sites(id,domain,path,php_version,app_type) VALUES (42,'discovery.test','/missing','8.4','laravel')")
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.DB.Exec("INSERT INTO daemons(site_id,name,command,directory,user,instances,managed_kind,status) VALUES (42,'Nightwatch','php8.4 artisan nightwatch:agent --custom','/missing','fluxo',1,'laravel_nightwatch','active'),(42,'Custom service','php8.4 artisan unknown','/missing','fluxo',1,'','stopped')")
	if err != nil {
		t.Fatal(err)
	}
	inventory := Inventory{Version: "8.4", Changes: []Package{{Name: "php8.4-cli", Installed: "8.4.26-1", Candidate: "8.4.27-1"}}}
	if err := recordDiscovery(Operation{Version: "8.4"}, []Inventory{inventory}); err != nil {
		t.Fatal(err)
	}
	snapshots, err := Snapshots()
	if err != nil || len(snapshots) != 1 || !snapshots[0].UpdateAvailable || snapshots[0].State != "available" || len(snapshots[0].BlockedReasons) != 0 {
		t.Fatalf("runtime poisoned discovery: %+v %v", snapshots, err)
	}
	if snapshots[0].Plan.Reviewed || len(snapshots[0].Plan.Impact.Workers) != 0 {
		t.Fatal("discovery pretended to review services")
	}
}

func TestDiscoveryCannotAuthorizeInstallation(t *testing.T) {
	m := testStore(t)
	p := testPlan(t)
	if _, err := m.Enqueue(context.Background(), "apply", p.Version, p.ParentPlanID, uuid.NewString(), "admin", ""); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("discovery authorized installation: %v", err)
	}
	if err := admitApply(database.DB, p.Version, p.ID); err != nil {
		t.Fatal(err)
	}
	parent, err := GetPlan(p.ParentPlanID)
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := publishReview(parent, Impact{BlockedReasons: []string{"Disable unsupported service #42 before reviewing installation again", "Disable unsupported service #42 before reviewing installation again"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked.Impact.BlockedReasons) != 1 {
		t.Fatal("blocker repeated")
	}
	if err = admitApply(database.DB, blocked.Version, blocked.ID); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("blocked review authorized installation: %v", err)
	}
	snapshots, _ := Snapshots()
	if !snapshots[0].UpdateAvailable || len(snapshots[0].BlockedReasons) != 0 || snapshots[0].Plan.Reviewed {
		t.Fatal("lifecycle blocker corrupted package discovery")
	}
	stored, _ := GetPlan(p.ID)
	if len(stored.Impact.BlockedReasons) != 0 {
		t.Fatal("a later review mutated an earlier review")
	}
}

func TestReviewInvalidatedByNewOrFailedDiscovery(t *testing.T) {
	for _, state := range []string{"replacement", "failed", "stale", "tampered_inventory", "expired_review", "cross_branch"} {
		t.Run(state, func(t *testing.T) {
			testStore(t)
			p := testPlan(t)
			parent, _ := GetPlan(p.ParentPlanID)
			snapshots, _ := Snapshots()
			s := snapshots[0]
			switch state {
			case "replacement":
				s.Plan.ID = uuid.NewString()
			case "failed":
				s.State = "check_failed"
			case "stale":
				s.CheckedAt = time.Now().Add(-25 * time.Hour).Unix()
			case "tampered_inventory":
				p.Inventory.PolicyHash = "changed"
			case "expired_review":
				p.CreatedAt = time.Now().Add(-16 * time.Minute).Unix()
			case "cross_branch":
				p.Version = "8.3"
			}
			if err := saveSnapshot(s); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(p)
			if _, err := database.DB.Exec("UPDATE php_update_plans SET payload=? WHERE id=?", string(raw), p.ID); err != nil {
				t.Fatal(err)
			}
			expectedErr := ErrPlanChanged
			if state == "expired_review" {
				expectedErr = ErrReviewChanged
			}
			if err := admitApply(database.DB, "8.4", p.ID); !errors.Is(err, expectedErr) {
				t.Fatalf("invalid review accepted: %v", err)
			}
			if state == "replacement" || state == "failed" || state == "stale" {
				if _, err := publishReview(parent, Impact{}); !errors.Is(err, ErrPlanChanged) {
					t.Fatalf("concurrent discovery change allowed publication: %v", err)
				}
			}
		})
	}
}

func TestNightwatchOnlyQualifiesGeneratedCommand(t *testing.T) {
	base := "php8.4 artisan nightwatch:agent --listen-on=127.0.0.1:2407"
	c := Consumer{Kind: "laravel_nightwatch", Command: base, Directory: "/home/fluxo/app", Instances: 1, Port: 2407}
	p := map[string]string{"Type": "simple", "User": "fluxo", "WorkingDirectory": c.Directory, "Restart": "always", "ExecStart": "{ path=/usr/bin/php8.4 ; argv[]=/usr/bin/" + base + " ; ignore_errors=no }"}
	if err := validateUnitDefinition("8.4", &c, p); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{base + " --server=custom", "php8.3 artisan nightwatch:agent --listen-on=127.0.0.1:2407", "php8.4 artisan nightwatch:agent --listen-on=0.0.0.0:2407", "php8.4 artisan nightwatch:agent --listen-on=127.0.0.1:02407", "php8.4 artisan nightwatch:agent --listen-on=127.0.0.1:65536"} {
		if _, ok := managedNightwatchPort("8.4", command); ok {
			t.Fatalf("unsupported command admitted: %s", command)
		}
	}
}

func TestSchedulerUsesActualInterpreterAfterSiteSwitch(t *testing.T) {
	testStore(t)
	_, err := database.DB.Exec("INSERT INTO sites(id,domain,path,php_version,app_type) VALUES(43,'switched.test','/home/fluxo/switched','8.3','laravel')")
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.DB.Exec("INSERT INTO crons(site_id,name,expression,command,user) VALUES(43,'Laravel Scheduler','* * * * *','php8.4 /home/fluxo/switched/artisan schedule:run','fluxo')")
	if err != nil {
		t.Fatal(err)
	}
	reasons, err := inspectSchedulerBlockers("8.4")
	if err != nil || len(reasons) != 1 {
		t.Fatalf("old interpreter missed: %v %v", reasons, err)
	}
	if reasons, err := inspectSchedulerBlockers("8.2"); err != nil || len(reasons) != 0 {
		t.Fatalf("unrelated branch blocked: %v %v", reasons, err)
	}
	if _, err = database.DB.Exec("UPDATE crons SET command='custom-wrapper schedule' WHERE site_id=43"); err != nil {
		t.Fatal(err)
	}
	if reasons, err := inspectSchedulerBlockers("8.4"); err != nil || len(reasons) != 1 {
		t.Fatalf("ambiguous interpreter accepted: %v %v", reasons, err)
	}
}

func TestSchedulerDatabaseBindingRejectsMovedPanel(t *testing.T) {
	testStore(t)
	var dbPath string
	if err := database.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&dbPath); err != nil {
		t.Fatal(err)
	}
	entry := scheduler.Entry{Database: dbPath}
	if !schedulerDatabaseMatches(entry, dbPath) {
		t.Fatal("current panel database rejected")
	}
	if schedulerDatabaseMatches(entry, filepath.Join(t.TempDir(), "fluxo.db")) || schedulerDatabaseMatches(entry, "") {
		t.Fatal("old dispatcher database could bypass the current generation gate")
	}
}

func TestRuntimeReviewFailurePreservesPackageDiscovery(t *testing.T) {
	m := testStore(t)
	p := testPlan(t)
	o, err := m.Enqueue(context.Background(), "apply", p.Version, p.ID, uuid.NewString(), "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	recordOperationFailure(o, ErrReviewChanged)
	items, err := Snapshots()
	if err != nil || len(items) != 1 || !items[0].UpdateAvailable || items[0].State != "available" {
		t.Fatalf("runtime change poisoned discovery: %+v %v", items, err)
	}
	op, err := GetOperation(o.ID)
	if err != nil || op.Status != "failed" || op.Message != ErrReviewChanged.Error() {
		t.Fatalf("review failure not reported: %+v %v", op, err)
	}
}

func TestRollbackFailureRevalidationRecoversNormalConfirmation(t *testing.T) {
	m := testStore(t)
	p := testPlan(t)
	p.Rollback = RollbackCapability{Available: true}
	failed := uuid.NewString()
	now := time.Now().Unix()
	if _, err := database.DB.Exec(`INSERT INTO php_update_operations(id,kind,version,status,created_at,updated_at) VALUES(?,'apply',?,'failed',?,?)`, failed, p.Version, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`INSERT INTO php_update_retention(inventory_hash,operation_id,reason,available,checked_at) VALUES(?,?,?,0,?)`, digest(p.Inventory), failed, "Temporary archive fetch outage", now); err != nil {
		t.Fatal(err)
	}
	available := false
	calls := 0
	var probeParents []string
	prepare := func(ctx context.Context, o Operation, review Plan, ack bool, silent ...bool) (archiveManifest, error) {
		calls++
		if o.ID == failed || !ack || len(silent) != 1 || !silent[0] {
			t.Fatal("probe reused an immutable failed manifest or changed the normal approval")
		}
		if digest(review.Inventory) != digest(p.Inventory) {
			t.Fatal("failure evidence was not bound to inventory")
		}
		dir, err := m.archiveDirectory(o.ID)
		if err != nil {
			t.Fatal(err)
		}
		probeParents = append(probeParents, filepath.Dir(dir))
		if err = os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("temporary probe"), 0600); err != nil {
			t.Fatal(err)
		}
		return archiveManifest{RollbackAvailable: available, RollbackReason: "Temporary archive fetch outage"}, nil
	}
	capability, err := m.reviewRollbackCapability(context.Background(), p, prepare)
	if err != nil || capability.Available || calls != 1 {
		t.Fatal("persistent failure did not request exceptional confirmation", capability, err, calls)
	}
	available = true
	capability, err = m.reviewRollbackCapability(context.Background(), p, prepare)
	if err != nil || !capability.Available || calls != 2 {
		t.Fatal("recovered archives did not restore normal confirmation", capability, err, calls)
	}
	capability, err = m.reviewRollbackCapability(context.Background(), p, prepare)
	if err != nil || !capability.Available || calls != 2 {
		t.Fatal("historical failure remained active after successful revalidation", capability, err, calls)
	}
	if p.Rollback.Available != true {
		t.Fatal("probe mutated the original plan")
	}
	for _, path := range probeParents {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("temporary retention probe was not cleaned up", path, err)
		}
	}
}
