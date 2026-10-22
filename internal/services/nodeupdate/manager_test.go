package nodeupdate

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/nodetoolchain"
	"github.com/google/uuid"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.DB.Close() })
	m := NewManager(t.TempDir())
	m.Launch = func(context.Context, Operation) error { return nil }
	return m
}
func testRelease(v string) nodetoolchain.Release {
	return nodetoolchain.Release{Version: v, NPM: "11.17.0", Architecture: "x64", Filename: "node-v" + v + "-linux-x64.tar.gz", SHA256: strings.Repeat("a", 64), ManifestSHA256: strings.Repeat("b", 64), Signer: strings.Repeat("C", 40), TrustRevision: nodetoolchain.TrustRevision(), TreeSHA256: strings.Repeat("d", 64)}
}
func TestNodeRequestsReplayBeforeBusyAndBindIntent(t *testing.T) {
	m := testManager(t)
	request := uuid.NewString()
	first, err := m.Enqueue(context.Background(), "check", "", request, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.Enqueue(context.Background(), "check", "", request, "admin", "")
	if err != nil || first.ID != again.ID {
		t.Fatal("idempotent retry was blocked by its own operation", err)
	}
	if _, err = m.Enqueue(context.Background(), "check", "different", request, "admin", ""); !errors.Is(err, ErrChanged) {
		t.Fatal("conflicting intent reused a request ID", err)
	}
	if _, err = m.Enqueue(context.Background(), "check", "", uuid.NewString(), "admin", ""); !errors.Is(err, ErrBusy) {
		t.Fatal("second worker admitted", err)
	}
}
func TestNodeDiscoveryCachesProofAndFailureIsNotCurrent(t *testing.T) {
	m := testManager(t)
	owned := nodetoolchain.OwnedRuntime{Root: "/opt/fluxo/node/v24.19.0", Release: testRelease("24.19.0"), Managers: map[string]string{"bun": "1.3.14"}}
	m.Inspect = func(context.Context) (nodetoolchain.OwnedRuntime, error) { return owned, nil }
	authCalls := 0
	m.Authenticate = func(_ context.Context, v, npm string) (nodetoolchain.Release, error) {
		authCalls++
		return testRelease(v), nil
	}
	m.Stage = func(_ context.Context, r nodetoolchain.Release, _ nodetoolchain.ProgressFunc) (nodetoolchain.Release, error) {
		return r, nil
	}
	m.Find = func(context.Context, string) (nodetoolchain.Release, error) {
		r := testRelease("24.20.0")
		r.TreeSHA256 = ""
		return r, nil
	}
	run := func() {
		o, err := m.Enqueue(context.Background(), "check", "", uuid.NewString(), "admin", "")
		if err != nil {
			t.Fatal(err)
		}
		if err = m.check(context.Background(), o); err != nil {
			t.Fatal(err)
		}
	}
	run()
	run()
	if authCalls != 1 {
		t.Fatalf("verified installed proof was not cached: %d", authCalls)
	}
	s, err := ReadStatus()
	if err != nil || !s.Snapshot.UpdateAvailable || !s.Snapshot.Notice {
		t.Fatal("candidate missing", err)
	}
	if err = Dismiss(s.Snapshot.CandidateID); err != nil {
		t.Fatal(err)
	}
	s, _ = ReadStatus()
	if s.Snapshot.Notice {
		t.Fatal("dismissal not persisted")
	}
	m.Find = func(context.Context, string) (nodetoolchain.Release, error) {
		return nodetoolchain.Release{}, errors.New("signature rejected")
	}
	o, err := m.Enqueue(context.Background(), "check", "", uuid.NewString(), "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.check(context.Background(), o); err == nil {
		t.Fatal("failed discovery succeeded")
	}
	s, err = ReadStatus()
	if err != nil || s.Snapshot.State != "check_failed" || s.Snapshot.UpdateAvailable || s.Snapshot.Candidate == nil || s.Snapshot.Error == "" {
		t.Fatal("failure erased old evidence or claimed current", err, s.Snapshot)
	}
}
func TestNodeApplyBindsFreshDiscoveryButAllowsStagedTreeEvidence(t *testing.T) {
	m := testManager(t)
	r := testRelease("24.20.0")
	r.TreeSHA256 = ""
	s := Snapshot{State: "available", DiscoveryID: uuid.NewString(), CheckedAt: time.Now().Unix(), Candidate: &r}
	if err := saveSnapshot(s, 0); err != nil {
		t.Fatal(err)
	}
	staged := r
	staged.TreeSHA256 = strings.Repeat("e", 64)
	p := Plan{ConsumerPolicy: consumerPolicy, ID: uuid.NewString(), Kind: "apply", CreatedAt: time.Now().Unix(), DiscoveryID: s.DiscoveryID, After: staged, Impact: Impact{BlockedReasons: []string{}}}
	if err := savePlan(p); err != nil {
		t.Fatal(err)
	}
	o, err := m.Enqueue(context.Background(), "apply", p.ID, uuid.NewString(), "admin", "")
	if err != nil {
		t.Fatal("staged hash invalidated immutable selection", err)
	}
	if err = updateOperation(o.ID, "failed", "failed", "test"); err != nil {
		t.Fatal(err)
	}
	s.DiscoveryID = uuid.NewString()
	if err = saveSnapshot(s, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Enqueue(context.Background(), "apply", p.ID, uuid.NewString(), "admin", ""); !errors.Is(err, ErrChanged) {
		t.Fatal("stale review accepted", err)
	}
}
func TestNodeRecoveryEvidenceKeepsAdmissionClosed(t *testing.T) {
	m := testManager(t)
	_ = m
	g := Generation{ID: uuid.NewString(), Head: uuid.NewString(), State: "recovery_required"}
	if err := saveGeneration(g); err != nil {
		t.Fatal(err)
	}
	if pending, err := database.PHPMaintenancePending(); err != nil || !pending {
		t.Fatal("terminal recovery released shared gate", pending, err)
	}
	g.State = "succeeded"
	if err := saveGeneration(g); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("INSERT INTO node_update_barriers(unit,generation_id) VALUES('fluxo-daemon-1.service',?)", g.ID); err != nil {
		t.Fatal(err)
	}
	if pending, err := database.NodeMaintenancePending(); err != nil || !pending {
		t.Fatal("leftover barrier released gate", pending, err)
	}
}
func TestNodeRecoveryCannotTargetSupersededGeneration(t *testing.T) {
	m := testManager(t)
	first := Generation{ID: uuid.NewString(), Head: uuid.NewString(), State: "succeeded"}
	second := Generation{ID: uuid.NewString(), Head: uuid.NewString(), State: "succeeded"}
	if err := saveGeneration(first); err != nil {
		t.Fatal(err)
	}
	if err := saveGeneration(second); err != nil {
		t.Fatal(err)
	}
	p := Plan{ConsumerPolicy: consumerPolicy, ID: uuid.NewString(), Kind: "rollback", CreatedAt: time.Now().Unix(), GenerationID: first.ID, Head: first.Head}
	if err := savePlan(p); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Enqueue(context.Background(), "rollback", p.ID, uuid.NewString(), "admin", ""); !errors.Is(err, ErrChanged) {
		t.Fatal("superseded recovery admitted", err)
	}
}
