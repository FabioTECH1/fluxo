package phpupdate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"github.com/google/uuid"
)

func testStore(t *testing.T) *Manager {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.DB.Close() })
	m := NewManager(t.TempDir())
	m.Launch = func(context.Context, Operation) error { return nil }
	return m
}
func testPlan(t *testing.T) Plan {
	t.Helper()
	p := Plan{ID: uuid.NewString(), Version: "8.4", CandidateID: "candidate-one", CreatedAt: time.Now().Unix(), Inventory: Inventory{Version: "8.4", Changes: []Package{{Name: "php8.4-fpm", Installed: "8.4.1-1", Candidate: "8.4.1-2"}}}}
	p.Rollback = RollbackCapability{Available: true}
	raw, _ := json.Marshal(p)
	if _, err := database.DB.Exec("INSERT INTO php_update_plans(id,version,payload,created_at) VALUES (?,?,?,?)", p.ID, p.Version, string(raw), p.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if err := saveSnapshot(Snapshot{Version: p.Version, State: "available", CheckedAt: p.CreatedAt, Plan: &p, Notice: true}); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestSnapshotsExpireWithoutClaimingNoUpdate(t *testing.T) {
	testStore(t)
	p := testPlan(t)
	items, err := Snapshots()
	if err != nil || len(items) != 1 || !items[0].UpdateAvailable || !items[0].Notice {
		t.Fatalf("available snapshot: %#v %v", items, err)
	}
	s := items[0]
	s.CheckedAt = time.Now().Add(-25 * time.Hour).Unix()
	if err = saveSnapshot(s); err != nil {
		t.Fatal(err)
	}
	items, _ = Snapshots()
	if items[0].UpdateAvailable || items[0].State != "stale" || !items[0].Notice {
		t.Fatalf("stale candidate lost: %#v", items[0])
	}
	if err = Dismiss(p.Version, p.CandidateID); err != nil {
		t.Fatal(err)
	}
	items, _ = Snapshots()
	if items[0].Notice {
		t.Fatal("dismissal was not persisted")
	}
	s = items[0]
	s.CheckedAt = time.Now().Unix()
	s.Plan.CreatedAt = time.Now().Unix() + 1
	_ = saveSnapshot(s)
	items, _ = Snapshots()
	if items[0].Notice {
		t.Fatal("timestamps resurrected a dismissed candidate")
	}
	s = items[0]
	s.Plan.Inventory.Changes[0].Candidate = "8.4.1-3"
	s.Plan.CandidateID = candidateIdentity(s.Plan.Inventory.Changes)
	s.Notice, _ = shouldNotify(s.Plan.Inventory)
	_ = saveSnapshot(s)
	items, _ = Snapshots()
	if !items[0].Notice {
		t.Fatal("new package candidate did not notify")
	}
}
func TestApplyIdempotencyAndSingleOperation(t *testing.T) {
	m := testStore(t)
	p := testPlan(t)
	requestID := uuid.NewString()
	a, err := m.Enqueue(context.Background(), "apply", p.Version, p.ID, requestID, "admin", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Enqueue(context.Background(), "apply", p.Version, p.ID, requestID, "admin", "127.0.0.1")
	if err != nil || a.ID != b.ID {
		t.Fatalf("retry duplicated operation: %+v %v", b, err)
	}
	if _, err = m.Enqueue(context.Background(), "apply", "8.3", p.ID, requestID, "admin", ""); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("request ID reused with different intent: %v", err)
	}
	if _, err = m.Enqueue(context.Background(), "check", "", "", "", "admin", ""); !errors.Is(err, ErrBusy) {
		t.Fatalf("parallel maintenance admitted: %v", err)
	}
	busy, _ := Applying()
	if !busy {
		t.Fatal("queued apply did not close mutation gate")
	}
	_ = finish(a.ID, "failed", "preflight failed")
	items, _ := Failures()
	if len(items) != 1 {
		t.Fatalf("failure not retained: %+v", items)
	}
	_, err = m.Enqueue(context.Background(), "apply", p.Version, p.ID, uuid.NewString(), "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	items, _ = Failures()
	if len(items) != 1 {
		t.Fatal("queued retry hid unresolved failure")
	}
}

func TestNoticeIdentityAndDebianRevisionHighWater(t *testing.T) {
	testStore(t)
	p := testPlan(t)
	p.Inventory.Version = p.Version
	p.Inventory.Changes[0].Candidate = "1:8.4.1-2ubuntu0.1"
	p.CandidateID = candidateIdentity(p.Inventory.Changes)
	s := Snapshot{Version: p.Version, State: "available", CheckedAt: p.CreatedAt, Plan: &p, Notice: true}
	if err := saveSnapshot(s); err != nil {
		t.Fatal(err)
	}
	if err := Dismiss(p.Version, p.CandidateID); err != nil {
		t.Fatal(err)
	}
	changed := p.Inventory.Changes[0]
	changed.Automatic = true
	changed.Held = true
	changed.Installed = "8.4.0-1"
	changed.Origins = []Origin{{Site: "new mirror"}}
	if candidateIdentity([]Package{changed}) != p.CandidateID {
		t.Fatal("noncandidate metadata changed notice identity")
	}
	for _, revision := range []string{"1:8.4.1-2ubuntu0.1", "8.4.99-100", "1:8.4.1-2ubuntu0.1~backport1"} {
		changed.Candidate = revision
		if newer, err := candidateNewer(p.Version, changed); err != nil || newer {
			t.Fatalf("older/equal candidate notified: %s %v", revision, err)
		}
	}
	changed.Candidate = "1:8.4.1-2ubuntu0.2"
	if newer, err := candidateNewer(p.Version, changed); err != nil || !newer {
		t.Fatalf("new distro security revision omitted: %v", err)
	}
}

func TestRecoveryKeepsNewerCandidateAndUsesLatestSignalState(t *testing.T) {
	m := testStore(t)
	a := testPlan(t)
	o, err := m.Enqueue(context.Background(), "apply", a.Version, a.ID, uuid.NewString(), "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.DB.Exec("UPDATE php_update_operations SET result=? WHERE id=?", `{"units":[{"name":"worker"}]}`, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = finish(o.ID, "health_failed", "repair config")
	v1, err := m.EnqueueVerification(context.Background(), o.ID, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = database.DB.Exec("UPDATE php_update_operations SET result=? WHERE id=?", `{"units":[{"name":"worker"}],"signalled":{"worker":true}}`, v1.ID)
	_ = finish(v1.ID, "restart_required", "draining")
	retry, err := m.EnqueueVerification(context.Background(), o.ID, "admin", "")
	if err != nil || retry.ID != v1.ID || !strings.Contains(string(retry.Result), `"signalled"`) {
		t.Fatalf("historical retry lost signal intent: %+v %v", retry, err)
	}
	v2, err := m.EnqueueVerification(context.Background(), v1.ID, "admin", "")
	if err != nil || !strings.Contains(string(v2.Result), `"signalled"`) {
		t.Fatalf("recovery lineage lost signal state: %+v %v", v2, err)
	}
	b := testPlan(t)
	b.Inventory.Changes[0].Candidate = "8.4.1-3"
	b.CandidateID = candidateIdentity(b.Inventory.Changes)
	if err := saveSnapshot(Snapshot{Version: b.Version, State: "available", CheckedAt: b.CreatedAt, Plan: &b, Notice: true}); err != nil {
		t.Fatal(err)
	}
	if err := resolveAvailability(a.Version, a.ID); err != nil {
		t.Fatal(err)
	}
	items, _ := Snapshots()
	if len(items) != 1 || items[0].Plan.ID != b.ID || !items[0].UpdateAvailable || !items[0].Notice {
		t.Fatalf("old recovery erased newer candidate: %+v", items)
	}
}

func TestCompleteInventoryRemovesAvailabilityButRetainsHistory(t *testing.T) {
	m := testStore(t)
	p := testPlan(t)
	o, err := m.Enqueue(context.Background(), "apply", p.Version, p.ID, uuid.NewString(), "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = finish(o.ID, "partial", "needs repair")
	if err := reconcileInstalledSnapshots(map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	items, _ := Snapshots()
	if len(items) != 0 {
		t.Fatal("removed branch retained notice")
	}
	if _, err := GetPlan(p.ID); err != nil {
		t.Fatal("immutable audit plan removed")
	}
	failures, _ := Failures()
	if len(failures) != 1 {
		t.Fatal("unresolved failure lost")
	}
}

func TestUnresolvedPostTransactionRecoveryBlocksNewApply(t *testing.T) {
	m := testStore(t)
	p := testPlan(t)
	a, err := m.Enqueue(context.Background(), "apply", p.Version, p.ID, uuid.NewString(), "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = database.DB.Exec("UPDATE php_update_operations SET checkpoint='packages_updated',result=? WHERE id=?", `{"units":[{"name":"worker"}],"signalled":{"worker":true}}`, a.ID)
	_ = finish(a.ID, "restart_required", "draining")
	if _, err := m.Enqueue(context.Background(), "apply", p.Version, p.ID, uuid.NewString(), "admin", ""); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("later preflight could hide recovery: %v", err)
	}
	failures, _ := Failures()
	if len(failures) != 1 || failures[0].ID != a.ID {
		t.Fatal("recoverable operation hidden")
	}
	v, err := m.EnqueueVerification(context.Background(), a.ID, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	if v.Checkpoint != "packages_updated" || !strings.Contains(string(v.Result), "signalled") {
		t.Fatal("recovery boundary/state lost")
	}
	_ = finish(v.ID, "succeeded", "healthy")
	if _, err := m.Enqueue(context.Background(), "apply", p.Version, p.ID, uuid.NewString(), "admin", ""); err != nil {
		t.Fatalf("resolved lineage still blocks apply: %v", err)
	}
}
func TestFailedCheckNeverEnablesCachedApply(t *testing.T) {
	m := testStore(t)
	p := testPlan(t)
	s, _ := Snapshots()
	s[0].State = "check_failed"
	s[0].Error = "repository timeout"
	_ = saveSnapshot(s[0])
	if _, err := m.Enqueue(context.Background(), "apply", p.Version, p.ID, uuid.NewString(), "admin", ""); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("failed refresh candidate applied: %v", err)
	}
}
func TestCheckCoalescesAndLaunchUncertaintyPreservesClaim(t *testing.T) {
	m := testStore(t)
	m.Launch = func(context.Context, Operation) error { return errors.New("systemd reply lost") }
	a, err := m.Enqueue(context.Background(), "check", "", "", "", "scheduler", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Enqueue(context.Background(), "check", "", "", "", "admin", "")
	if err != nil || a.ID != b.ID {
		t.Fatalf("duplicate refresh: %v", err)
	}
	o, _ := GetOperation(a.ID)
	if !o.Active() {
		t.Fatal("lost supervisor reply incorrectly marked work failed")
	}
}
func TestAPTPlannerFixtures(t *testing.T) {
	if err := exec.Command("/usr/bin/python3", "-I", "-c", "import apt, apt_pkg").Run(); err != nil {
		t.Skip("python3-apt is needed for resolver fixtures")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "bridge.py")
	if err := os.WriteFile(path, []byte(aptBridge), 0600); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("apt_bridge_test.py")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/usr/bin/python3", "-I", "-c", string(fixture), path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("APT planning fixtures: %v\n%s", err, out)
	}
}
