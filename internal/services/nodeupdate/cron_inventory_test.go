package nodeupdate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/nodetoolchain"
	"fluxo/internal/services/scheduler"
	"github.com/google/uuid"
)

func TestNodeCronInventoryIncludesGlobalOrphanAndCustomJobs(t *testing.T) {
	for _, tc := range []struct {
		name, command, user string
		siteID              int
		createSite          bool
	}{
		{"global node", "/usr/local/bin/node /home/fluxo/job.js", "fluxo", 0, false},
		{"global wrapper", "/home/fluxo/maintenance.sh", "fluxo", 0, false},
		{"global root wrapper", "/usr/local/bin/maintenance", "root", 0, false},
		{"global root node", "node /root/job.js", "root", 0, false},
		{"orphan scheduler-looking command", "php8.4 /home/fluxo/orphan/artisan schedule:run", "fluxo", 999, false},
		{"site custom wrapper", "./maintenance.sh", "fluxo", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testManager(t)
			if tc.createSite {
				if _, err := database.DB.Exec("INSERT INTO sites(id,domain,path,app_type) VALUES(1,'cron.example','/home/fluxo/cron','php')"); err != nil {
					t.Fatal(err)
				}
			}
			result, err := database.DB.Exec("INSERT INTO crons(site_id,name,expression,command,user) VALUES(?,?,'* * * * *',?,?)", tc.siteID, tc.name, tc.command, tc.user)
			if err != nil {
				t.Fatal(err)
			}
			id, _ := result.LastInsertId()
			// Call the same production inventory used by BOTH normal and recovery
			// inspection. No cron file is installed and no command is executed.
			for _, original := range []*Impact{nil, {}} {
				impact, _ := inspectConsumers(context.Background(), nil, "", original, observeUnit)
				expected := fmt.Sprintf("Custom cron #%d", id)
				found := false
				for _, reason := range impact.BlockedReasons {
					if strings.Contains(reason, expected) && strings.Contains(reason, tc.name) {
						found = true
					}
				}
				if !found {
					t.Fatalf("job omitted from maintenance blockers: %v", impact.BlockedReasons)
				}
			}
		})
	}
}

func TestNodeCronInventoryRequiresQualifiedSchedulerAdmission(t *testing.T) {
	testManager(t)
	if _, err := database.DB.Exec("INSERT INTO sites(id,domain,path,app_type) VALUES(1,'scheduler.example','/home/fluxo/scheduler','laravel')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("INSERT INTO crons(id,site_id,name,expression,command,user) VALUES(1,1,'Laravel Scheduler','* * * * *','php8.4 /home/fluxo/scheduler/artisan schedule:run','fluxo')"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	admission := func(_ context.Context, id int, entry scheduler.Entry) error {
		calls++
		if id != 1 || entry.SiteID != 1 || entry.Directory != "/home/fluxo/scheduler" {
			t.Fatal("wrong qualified Scheduler identity", id, entry)
		}
		return nil
	}
	reasons, err := inspectCronInventory(context.Background(), admission)
	if err != nil || len(reasons) != 0 || calls != 1 {
		t.Fatal("qualified verified Scheduler was not admitted", reasons, calls, err)
	}
	reasons, err = inspectCronInventory(context.Background(), func(context.Context, int, scheduler.Entry) error {
		return errors.New("Scheduler fixture is not ready")
	})
	if err != nil || len(reasons) != 1 || reasons[0] != "Scheduler fixture is not ready" {
		t.Fatal("qualification bypassed readiness verification", reasons, err)
	}
	if _, err = database.DB.Exec("UPDATE crons SET command=command || ' && node job.js' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	calls = 0
	reasons, err = inspectCronInventory(context.Background(), admission)
	if err != nil || len(reasons) != 1 || calls != 0 {
		t.Fatal("custom script was treated as a safe Scheduler command", reasons, calls, err)
	}
}

func TestNodeCronAddedAfterReviewRejectsWorkerBeforeStaging(t *testing.T) {
	m := testManager(t)
	owned := nodetoolchain.OwnedRuntime{Root: "/opt/fluxo/node/v24.19.0", Release: testRelease("24.19.0")}
	m.Inspect = func(context.Context) (nodetoolchain.OwnedRuntime, error) { return owned, nil }
	// Isolate the scheduling boundary from host /proc and systemd state. The
	// cron inventory and worker revalidation remain the real production code.
	m.Impact = func(ctx context.Context, _, _ string) (Impact, error) {
		reasons, err := inspectCronInventory(ctx, verifySchedulerAdmission)
		return Impact{BlockedReasons: reasons}, err
	}
	m.Stage = func(context.Context, nodetoolchain.Release, nodetoolchain.ProgressFunc) (nodetoolchain.Release, error) {
		t.Fatal("staging reached after a global cron invalidated review")
		return nodetoolchain.Release{}, nil
	}
	p := Plan{ConsumerPolicy: consumerPolicy, ID: uuid.NewString(), Kind: "apply", CreatedAt: time.Now().Unix(), Before: owned, After: testRelease("24.20.0"), Impact: Impact{BlockedReasons: []string{}}}
	if err := savePlan(p); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("INSERT INTO crons(site_id,name,expression,command,user) VALUES(0,'new global job','* * * * *','/home/fluxo/wrapper.sh','fluxo')"); err != nil {
		t.Fatal(err)
	}
	if err := m.apply(context.Background(), Operation{ID: uuid.NewString(), Kind: "apply", PlanID: p.ID}); !errors.Is(err, ErrChanged) {
		t.Fatal("worker accepted a new uncoordinated global job", err)
	}
	var generations int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM node_update_generations").Scan(&generations); err != nil || generations != 0 {
		t.Fatal("rejected preflight created a mutating generation", generations, err)
	}
}
