package nodeupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"github.com/google/uuid"
)

func TestNodeOldConsumerReviewsCannotExecuteOrBeApproved(t *testing.T) {
	for _, kind := range []string{"apply", "repair", "rollback"} {
		t.Run(kind, func(t *testing.T) {
			m := testManager(t)
			p := Plan{ID: uuid.NewString(), Kind: kind, CreatedAt: time.Now().Unix(), Impact: Impact{Units: []Unit{{BunOnly: true, Active: true}}}}
			if err := savePlan(p); err != nil {
				t.Fatal(err)
			}
			if _, err := m.Enqueue(context.Background(), kind, p.ID, uuid.NewString(), "admin", ""); !errors.Is(err, ErrChanged) {
				t.Fatal("old exemption review approved", err)
			}
			if err := m.apply(context.Background(), Operation{Kind: kind, PlanID: p.ID}); !errors.Is(err, ErrChanged) {
				t.Fatal("queued old policy executed", err)
			}
			var n int
			if err := database.DB.QueryRow("SELECT COUNT(*) FROM node_update_generations").Scan(&n); err != nil || n != 0 {
				t.Fatal("old review created mutation intent", n, err)
			}
		})
	}
}

func TestNodeHistoricalBunRecoveryAddsEvidenceWithoutChangingIntent(t *testing.T) {
	original := Impact{Units: []Unit{{ID: 1, Name: "fluxo-daemon-1.service", Active: true, Enabled: "enabled", BunOnly: true}}}
	fresh := Impact{Units: append([]Unit(nil), original.Units...)}
	fresh.Units[0].Bun = &BunRuntime{Path: managedBunPath, Version: "1.3.14", SHA256: strings.Repeat("a", 64)}
	before := digest(original)
	if !sameOriginalConsumers(fresh, original) || sameConsumers(fresh, original) || digest(original) != before {
		t.Fatal("fresh recovery evidence must be reviewable separately from immutable history")
	}
	changed := Impact{Units: append([]Unit(nil), fresh.Units...)}
	changed.Units[0].Bun = &BunRuntime{Path: managedBunPath, Version: "1.3.14", SHA256: strings.Repeat("b", 64)}
	if sameConsumers(changed, fresh) {
		t.Fatal("reviewed Bun receipt drift accepted")
	}
	for _, active := range []bool{true, false} {
		original.Units[0].Active, fresh.Units[0].Active = active, active
		if !sameOriginalConsumers(fresh, original) {
			t.Fatal("unchanged original state rejected")
		}
		fresh.Units[0].Active = !active
		if sameOriginalConsumers(fresh, original) {
			t.Fatal("recovery changed original active/stopped intent")
		}
		fresh.Units[0].Active = active
	}
	changed.Units[0].Command = "changed wrapper"
	if sameOriginalConsumers(changed, original) {
		t.Fatal("fresh receipts exempt changed configuration")
	}
}

func TestNodeBunMissingCoordinationCannotReachHolds(t *testing.T) {
	for _, coordinated := range []bool{false, true} {
		m, original := retainedTestManager(t)
		original.Impact.Units = []Unit{{ID: 1, Name: "fluxo-daemon-1.service", BunOnly: true, Active: true}}
		g := Generation{ID: uuid.NewString(), Plan: original}
		if coordinated {
			impact := original.Impact
			g.Coordination = &impact
		}
		if err := m.transition(context.Background(), Operation{}, &g); err == nil {
			t.Fatal("missing fresh Bun evidence reached transition")
		}
		var n int
		if err := database.DB.QueryRow("SELECT COUNT(*) FROM node_update_barriers").Scan(&n); err != nil || n != 0 {
			t.Fatal("unverified Bun coordination created holds", n, err)
		}
	}
}

func TestNodeBunExecutableNameAloneIsNotRuntimeProof(t *testing.T) {
	// The test executable is harmless sleep. A Bun-shaped filename cannot
	// authorize an unrelated executable as the managed Bun runtime.
	bunRoot := consumerTestRuntime(t)
	// Reuse the process harness, then rename its executable while running.
	pid, key := consumerTestProcess(t, bunRoot)
	if err := os.Rename(filepath.Join(bunRoot, "bin", "node"), filepath.Join(bunRoot, "bin", "bun")); err != nil {
		t.Fatal(err)
	}
	u := Unit{PIDs: map[int]string{pid: key}}
	for _, proof := range []*BunRuntime{nil, {Path: managedBunPath, Version: "1.3.14", SHA256: strings.Repeat("a", 64)}} {
		u.Bun = proof
		if _, _, err := runtimeProcesses(u, consumerTestRuntime(t)); err == nil {
			t.Fatal("foreign Bun process adopted from its basename")
		}
	}
}
