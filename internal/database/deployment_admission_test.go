package database

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestOctaneEnableAndDeploymentClaimsAreMutuallyExclusive(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = DB.Close() })
	// Exercise both deterministic orderings and concurrent writers using the
	// same production claim functions used by the handler and deployment queue.
	for round := 0; round < 42; round++ {
		siteID := round + 1
		if _, err := DB.Exec("INSERT INTO sites(id,domain,path) VALUES (?,?,?)", siteID, fmt.Sprintf("claim-%d.test", siteID), "/unused"); err != nil {
			t.Fatal(err)
		}
		if _, err := DB.Exec("INSERT INTO deployments(id,site_id,status) VALUES (?,?,'pending')", siteID, siteID); err != nil {
			t.Fatal(err)
		}
		type result struct {
			claimed bool
			err     error
		}
		deploy := func() result {
			ok, err := ClaimPendingDeployment(int64(siteID), siteID)
			return result{ok, err}
		}
		enable := func() result {
			_, ok, err := CreatePendingOctaneDaemon(siteID, "Laravel Octane", "unused", "/unused")
			return result{ok, err}
		}
		var deployment, octane result
		switch round {
		case 0:
			deployment, octane = deploy(), enable()
			if !deployment.claimed || octane.claimed {
				t.Fatal("enable was not excluded by the deployment-first ordering")
			}
		case 1:
			octane, deployment = enable(), deploy()
			if !octane.claimed || deployment.claimed {
				t.Fatal("deployment was not excluded by the enable-first ordering")
			}
		default:
			start := make(chan struct{})
			deployResult, octaneResult := make(chan result, 1), make(chan result, 1)
			go func() { <-start; deployResult <- deploy() }()
			go func() { <-start; octaneResult <- enable() }()
			close(start)
			deployment, octane = <-deployResult, <-octaneResult
		}
		if deployment.err != nil || octane.err != nil {
			t.Fatalf("claim %d failed: deployment=%v Octane=%v", round, deployment.err, octane.err)
		}
		if deployment.claimed == octane.claimed {
			t.Fatalf("claim %d must have exactly one winner: deployment=%v Octane=%v", round, deployment.claimed, octane.claimed)
		}
		var running, pending int
		if err := DB.QueryRow("SELECT COUNT(*) FROM deployments WHERE site_id=? AND status='running'", siteID).Scan(&running); err != nil {
			t.Fatal(err)
		}
		if err := DB.QueryRow("SELECT COUNT(*) FROM daemons WHERE site_id=? AND managed_kind='laravel_octane' AND status='enable_pending'", siteID).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if running+pending != 1 {
			t.Fatalf("claim %d persisted overlapping or missing ownership: running=%d pending=%d", round, running, pending)
		}
	}
}
