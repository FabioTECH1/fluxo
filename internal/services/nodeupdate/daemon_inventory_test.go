package nodeupdate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/nodetoolchain"
	"github.com/google/uuid"
)

func TestNodeDaemonInventoryBlocksOpaqueManagedAndCustomConsumers(t *testing.T) {
	for _, tc := range []struct{ kind, command, state string }{
		{"python_app", "/usr/bin/python3 bridge.py", "active"},
		{"python_app", "/usr/bin/python3 bridge.py", "stopped"},
		{"python_app", "/usr/bin/python3 bridge.py", "restarting"},
		{"laravel_queue", "php8.4 artisan queue:work", "active"},
		{"laravel_horizon", "php8.4 artisan horizon", "stopped"},
		{"laravel_octane", "php8.4 artisan octane:start", "active"},
		{"laravel_nightwatch", "php8.4 artisan nightwatch:agent", "active"},
		{"future_kind", "/home/fluxo/wrapper.sh", "active"},
		{"", "/home/fluxo/wrapper.sh", "stopped"},
		{"node_app", "/home/fluxo/wrapper.sh", "stopped"},
	} {
		t.Run(tc.kind+"/"+tc.state, func(t *testing.T) {
			testManager(t)
			// Includes the production custom-python shape and opaque workers
			// with no 'node' in their command. Nothing is executed by this test.
			if _, err := database.DB.Exec("INSERT INTO sites(id,domain,path,app_type,start_command) VALUES(1,'bridge.example','/home/fluxo/bridge','python','/usr/bin/python3 bridge.py')"); err != nil {
				t.Fatal(err)
			}
			if _, err := database.DB.Exec("INSERT INTO daemons(id,site_id,name,managed_kind,command,directory,status) VALUES(1,1,'Opaque consumer',?,?,'/home/fluxo/bridge',?)", tc.kind, tc.command, tc.state); err != nil {
				t.Fatal(err)
			}
			for _, original := range []*Impact{nil, {}} {
				impact, _ := inspectConsumers(context.Background(), nil, "", original, observeUnit)
				found := false
				for _, reason := range impact.BlockedReasons {
					if strings.Contains(reason, `Background process "Opaque consumer" on bridge.example`) && !strings.Contains(reason, "#1") {
						found = true
					}
				}
				if !found {
					t.Fatal("normal/recovery inventory omitted opaque process", impact.BlockedReasons)
				}
			}
		})
	}
}

func TestNodeDaemonInventoryRequiresPositiveExactNodeIdentity(t *testing.T) {
	testManager(t)
	if _, err := database.DB.Exec("INSERT INTO daemons(id,site_id,name,managed_kind,command,directory) VALUES(1,1,'Node.js','node_app','node server.js','/home/fluxo/one'),(2,2,'Node.js','node_app','bun run start','/home/fluxo/two')"); err != nil {
		t.Fatal(err)
	}
	units := []Unit{{ID: 1, SiteID: 1, Name: "fluxo-daemon-1.service"}, {ID: 2, SiteID: 2, Name: "fluxo-daemon-2.service", BunOnly: true}}
	reasons, err := inspectDaemonInventory(context.Background(), units)
	if err != nil || len(reasons) != 0 {
		t.Fatal("positively inspected Node/Bun services were rejected", reasons, err)
	}
	reasons, err = inspectDaemonInventory(context.Background(), nil)
	if err != nil || len(reasons) != 2 {
		t.Fatal("a node_app label alone authorized an uninspected service", reasons, err)
	}
	for _, change := range []string{"UPDATE daemons SET managed_kind='python_app' WHERE id=1", "UPDATE daemons SET managed_kind='node_app',site_id=0 WHERE id=1", "DELETE FROM daemons WHERE id=1"} {
		if _, err = database.DB.Exec(change); err != nil {
			t.Fatal(err)
		}
		if reasons, err = inspectDaemonInventory(context.Background(), units); err != nil || len(reasons) == 0 {
			t.Fatal("changed/missing node identity was accepted", reasons, err)
		}
	}
}

func TestNodeDaemonInventoryIncludesGlobalOrphanAndDuplicateRecords(t *testing.T) {
	testManager(t)
	for _, siteID := range []int{0, 999, 999} {
		if _, err := database.DB.Exec("INSERT INTO daemons(site_id,name,managed_kind,command,directory) VALUES(?,'Node.js','node_app','node server.js','/home/fluxo/orphan')", siteID); err != nil {
			t.Fatal(err)
		}
	}
	if reasons, err := inspectDaemonInventory(context.Background(), nil); err != nil || len(reasons) != 3 {
		t.Fatal("global/orphan/duplicate daemon records were omitted", reasons, err)
	}
}

func TestNodeDaemonAddedAfterReviewRejectsWorkerBeforeStaging(t *testing.T) {
	m := testManager(t)
	owned := nodetoolchain.OwnedRuntime{Root: "/opt/fluxo/node/v24.19.0", Release: testRelease("24.19.0")}
	m.Inspect = func(context.Context) (nodetoolchain.OwnedRuntime, error) { return owned, nil }
	m.Impact = func(ctx context.Context, _, _ string) (Impact, error) {
		reasons, err := inspectDaemonInventory(ctx, nil)
		return Impact{BlockedReasons: reasons}, err
	}
	m.Stage = func(context.Context, nodetoolchain.Release, nodetoolchain.ProgressFunc) (nodetoolchain.Release, error) {
		t.Fatal("new opaque consumer reached staging")
		return nodetoolchain.Release{}, nil
	}
	p := Plan{ConsumerPolicy: consumerPolicy, ID: uuid.NewString(), Kind: "apply", CreatedAt: time.Now().Unix(), Before: owned, After: testRelease("24.20.0"), Impact: Impact{BlockedReasons: []string{}}}
	if err := savePlan(p); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("INSERT INTO daemons(site_id,name,managed_kind,command,directory) VALUES(0,'New bridge','python_app','python3 bridge.py','/home/fluxo/bridge')"); err != nil {
		t.Fatal(err)
	}
	if err := m.apply(context.Background(), Operation{ID: uuid.NewString(), Kind: "apply", PlanID: p.ID}); !errors.Is(err, ErrChanged) {
		t.Fatal("worker accepted a consumer added after review", err)
	}
	var generations, barriers int
	if err := database.DB.QueryRow("SELECT (SELECT COUNT(*) FROM node_update_generations),(SELECT COUNT(*) FROM node_update_barriers)").Scan(&generations, &barriers); err != nil || generations != 0 || barriers != 0 {
		t.Fatal("rejected preflight created mutation intent or holds", generations, barriers, err)
	}
}

func TestNodeDaemonChangedAfterRecoveryReviewRejectsBeforeProofsOrHolds(t *testing.T) {
	m, original := retainedTestManager(t)
	original.Impact.Units = []Unit{{ID: 1, SiteID: 1, Name: "fluxo-daemon-1.service"}}
	if _, err := database.DB.Exec("INSERT INTO daemons(id,site_id,name,managed_kind,command,directory) VALUES(1,1,'Node.js','node_app','node server.js','/home/fluxo/one')"); err != nil {
		t.Fatal(err)
	}
	g := Generation{ID: uuid.NewString(), Head: uuid.NewString(), State: "succeeded", Plan: original}
	if err := saveGeneration(g); err != nil {
		t.Fatal(err)
	}
	owned := nodetoolchain.OwnedRuntime{Root: "/opt/fluxo/node/v24.20.0", Release: original.After}
	m.Inspect = func(context.Context) (nodetoolchain.OwnedRuntime, error) { return owned, nil }
	m.RecoveryImpact = func(ctx context.Context, saved Generation) (Impact, error) {
		reasons, err := inspectDaemonInventory(ctx, saved.Plan.Impact.Units)
		return Impact{Units: saved.Plan.Impact.Units, BlockedReasons: reasons}, err
	}
	m.Authenticate = func(context.Context, string, string) (nodetoolchain.Release, error) {
		t.Fatal("changed recovery consumer reached proof staging")
		return nodetoolchain.Release{}, nil
	}
	p := Plan{ConsumerPolicy: consumerPolicy, ID: uuid.NewString(), Kind: "rollback", CreatedAt: time.Now().Unix(), GenerationID: g.ID, Head: g.Head, Before: owned, After: original.Before.Release, Impact: original.Impact}
	if err := savePlan(p); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("UPDATE daemons SET managed_kind='python_app',command='python3 bridge.py' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	o, err := m.Enqueue(context.Background(), "rollback", p.ID, uuid.NewString(), "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.apply(context.Background(), o); !errors.Is(err, ErrChanged) {
		t.Fatal("changed recovery consumer was accepted", err)
	}
	actual, err := getGeneration(g.ID)
	if err != nil || actual.State != "succeeded" {
		t.Fatal("rejected recovery opened a mutation generation", actual, err)
	}
	var barriers int
	if err = database.DB.QueryRow("SELECT COUNT(*) FROM node_update_barriers").Scan(&barriers); err != nil || barriers != 0 {
		t.Fatal("rejected recovery installed holds", barriers, err)
	}
}
