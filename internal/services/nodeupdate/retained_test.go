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

func retainedTestPlan() Plan {
	before, after := testRelease("24.19.0"), testRelease("24.20.0")
	before.TrustRevision, after.TrustRevision = "historical-keyring-A", "historical-keyring-A"
	return Plan{ConsumerPolicy: consumerPolicy, ID: uuid.NewString(), Kind: "apply", Before: nodetoolchain.OwnedRuntime{Root: "/opt/fluxo/node/v24.19.0", Release: before}, After: after, Impact: Impact{BlockedReasons: []string{}}}
}

func retainedTestManager(t *testing.T) (*Manager, Plan) {
	m, original := testManager(t), retainedTestPlan()
	m.Authenticate = func(_ context.Context, version, _ string) (nodetoolchain.Release, error) {
		fresh := testRelease(version)
		fresh.ManifestSHA256 = strings.Repeat("e", 64)
		fresh.TreeSHA256 = ""
		return fresh, nil
	}
	m.Stage = func(_ context.Context, r nodetoolchain.Release, _ nodetoolchain.ProgressFunc) (nodetoolchain.Release, error) {
		r.TreeSHA256 = testRelease(r.Version).TreeSHA256
		return r, nil
	}
	m.ValidateRelease = func(r nodetoolchain.Release) error {
		if r.TrustRevision != nodetoolchain.TrustRevision() {
			return errors.New("old keyring")
		}
		return nil
	}
	return m, original
}

func TestNodeRetainedProofRefreshPreservesPayloadAndOriginalPlan(t *testing.T) {
	m, original := retainedTestManager(t)
	before := digest(original)
	refreshed, err := m.refreshRetained(context.Background(), original)
	if err != nil {
		t.Fatal(err)
	}
	if digest(original) != before || original.Before.Release.TrustRevision != "historical-keyring-A" || !sameRetainedPayload(original.Before.Release, refreshed.Before) || !sameRetainedPayload(original.After, refreshed.After) {
		t.Fatal("refresh changed original history or payload", original, refreshed)
	}
	if refreshed.Before.TrustRevision != nodetoolchain.TrustRevision() || refreshed.After.TrustRevision != nodetoolchain.TrustRevision() {
		t.Fatal("proof did not use current trust")
	}
}

func TestNodeRetainedProofRefreshRejectsChangedOrUntrustedPayload(t *testing.T) {
	for _, condition := range []string{"removed/revoked signer", "archive", "tree", "activation prerequisite"} {
		t.Run(condition, func(t *testing.T) {
			m, original := retainedTestManager(t)
			stages := 0
			m.Authenticate = func(_ context.Context, v, _ string) (nodetoolchain.Release, error) {
				if condition == "removed/revoked signer" {
					return nodetoolchain.Release{}, errors.New("untrusted signature")
				}
				r := testRelease(v)
				r.TreeSHA256 = ""
				if condition == "archive" {
					r.SHA256 = strings.Repeat("f", 64)
				}
				return r, nil
			}
			m.Stage = func(_ context.Context, r nodetoolchain.Release, _ nodetoolchain.ProgressFunc) (nodetoolchain.Release, error) {
				stages++
				r.TreeSHA256 = testRelease(r.Version).TreeSHA256
				if condition == "tree" {
					r.TreeSHA256 = strings.Repeat("f", 64)
				}
				return r, nil
			}
			if condition == "activation prerequisite" {
				m.ValidateRelease = func(nodetoolchain.Release) error { return errors.New("cannot activate") }
			}
			if _, err := m.refreshRetained(context.Background(), original); err == nil {
				t.Fatal("unsafe retained proof refreshed")
			}
			if (condition == "archive" || condition == "removed/revoked signer") && stages != 0 {
				t.Fatal("changed/untrusted archive reached staging")
			}
		})
	}
}

func TestNodeRecoveryProofFailureDoesNotHoldCompletedGeneration(t *testing.T) {
	for _, failure := range []string{"reauthentication", "persistence"} {
		t.Run(failure, func(t *testing.T) {
			m, original := retainedTestManager(t)
			refreshed, err := m.refreshRetained(context.Background(), original)
			if err != nil {
				t.Fatal(err)
			}
			g := Generation{ID: uuid.NewString(), Head: uuid.NewString(), State: "succeeded", Plan: original, Retained: &RetainedReleases{Before: original.Before.Release, After: original.After}}
			if err = saveGeneration(g); err != nil {
				t.Fatal(err)
			}
			owned := nodetoolchain.OwnedRuntime{Root: "/opt/fluxo/node/v24.20.0", Release: original.After}
			m.Inspect = func(context.Context) (nodetoolchain.OwnedRuntime, error) { return owned, nil }
			m.RecoveryImpact = func(context.Context, Generation) (Impact, error) { return original.Impact, nil }
			p := Plan{ConsumerPolicy: consumerPolicy, ID: uuid.NewString(), Kind: "rollback", CreatedAt: time.Now().Unix(), GenerationID: g.ID, Head: g.Head, Before: owned, After: refreshed.Before, Retained: &refreshed, Impact: original.Impact}
			if err = savePlan(p); err != nil {
				t.Fatal(err)
			}
			o, err := m.Enqueue(context.Background(), "rollback", p.ID, uuid.NewString(), "admin", "")
			if err != nil {
				t.Fatal(err)
			}
			if failure == "reauthentication" {
				m.Authenticate = func(context.Context, string, string) (nodetoolchain.Release, error) {
					return nodetoolchain.Release{}, errors.New("signer removed by current trust policy")
				}
			} else {
				_, err = database.DB.Exec(`CREATE TRIGGER reject_node_proof BEFORE UPDATE ON node_update_generations WHEN NEW.state='prepared' BEGIN SELECT RAISE(ABORT,'proof persistence fixture'); END`)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err = m.apply(context.Background(), o); err == nil {
				t.Fatal("proof failure reached service transition")
			}
			actual, err := getGeneration(g.ID)
			if err != nil || actual.State != "succeeded" || digest(actual.Retained) != digest(g.Retained) || digest(actual.Coordination) != digest(g.Coordination) || digest(actual.Plan) != digest(original) {
				t.Fatal("failed preflight created an open generation or overwrote provenance", actual, err)
			}
			var barriers int
			if err = database.DB.QueryRow("SELECT COUNT(*) FROM node_update_barriers").Scan(&barriers); err != nil || barriers != 0 {
				t.Fatal("failed preflight installed service barriers", barriers, err)
			}
			// RunWorker records this failure after apply returns; prove the queued
			// reservation is the only remaining gate, not a spurious generation.
			if err = updateOperation(o.ID, "failed", "failed", "preflight rejected"); err != nil {
				t.Fatal(err)
			}
			if pending, err := database.NodeMaintenancePending(); err != nil || pending {
				t.Fatal("non-mutating failure stranded runtime admission", pending, err)
			}
		})
	}
}

func TestNodeTransitionRejectsOldProofBeforeServiceHolds(t *testing.T) {
	m, original := retainedTestManager(t)
	calls := 0
	m.ValidateRelease = func(nodetoolchain.Release) error { calls++; return errors.New("old or invalid activation proof") }
	g := Generation{ID: uuid.NewString(), Plan: original, Coordination: &original.Impact, Retained: &RetainedReleases{Before: original.Before.Release, After: original.After}}
	if err := m.transition(context.Background(), Operation{}, &g); err == nil {
		t.Fatal("invalid proof reached stop/activation")
	}
	if calls != 1 {
		t.Fatal("activation prerequisites were not inspected", calls)
	}
	var barriers int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM node_update_barriers").Scan(&barriers); err != nil || barriers != 0 {
		t.Fatal("activation prerequisites were checked after service holds", barriers, err)
	}
}
