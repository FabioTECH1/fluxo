package nodeupdate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/nodetoolchain"
	"fluxo/internal/services/phpupdate"
	"github.com/google/uuid"
)

func reviewTestImpact() Impact {
	c := phpTestConsumer()
	c.Units[0].Runtime.MainPID = 20
	c.Units[0].Runtime.Restarts = "0"
	c.Units[0].Runtime.PIDs = map[int]string{20: "boot:php-controller", 21: "boot:php-job"}
	return Impact{PHPPolicy: 1, Sites: []Site{{ID: 1, Domain: "app.test", AppType: "node"}}, Units: []Unit{{ID: 1, SiteID: 1, Name: "fluxo-daemon-1.service", Active: true, Enabled: "disabled", MainPID: 10, PIDs: map[int]string{10: "boot:node-controller", 11: "boot:node-job"}}}, PHP: []phpupdate.MaintenanceConsumer{c}, BlockedReasons: []string{}}
}

func copyReviewImpact(t *testing.T, old Impact) Impact {
	t.Helper()
	b, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	var result Impact
	if err = json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestApplyReviewAllowsAuthenticatedJobsButRejectsIntentControllerAndContractDrift(t *testing.T) {
	old := reviewTestImpact()
	now := copyReviewImpact(t, old)
	now.Units[0].PIDs = map[int]string{10: "boot:node-controller", 12: "boot:new-node-job"}
	now.PHP[0].Units[0].Runtime.PIDs = map[int]string{20: "boot:php-controller", 22: "boot:new-php-job"}
	if !sameApplyConsumers(now, old) {
		t.Fatal("authenticated ordinary jobs invalidated review")
	}
	changes := map[string]func(*Impact){
		"node stopped":    func(i *Impact) { i.Units[0].Active = false },
		"php stopped":     func(i *Impact) { i.PHP[0].Units[0].Runtime.Active = false },
		"node controller": func(i *Impact) { i.Units[0].MainPID = 12 },
		"node PID reused": func(i *Impact) { i.Units[0].PIDs[10] = "boot:reused-controller" },
		"php controller":  func(i *Impact) { i.PHP[0].Units[0].Runtime.MainPID = 22 },
		"php PID reused":  func(i *Impact) { i.PHP[0].Units[0].Runtime.PIDs[20] = "boot:reused-controller" },
		"php restart":     func(i *Impact) { i.PHP[0].Units[0].Runtime.Restarts = "1" },
		"node enabled":    func(i *Impact) { i.Units[0].Enabled = "enabled" },
		"php enabled":     func(i *Impact) { i.PHP[0].Units[0].Enabled = "disabled" },
		"node definition": func(i *Impact) { i.Units[0].Definition = "changed" },
		"php user":        func(i *Impact) { i.PHP[0].Units[0].Definition["User"] = "root" },
		"php environment": func(i *Impact) { i.PHP[0].Units[0].Definition["Environment"] = "LD_PRELOAD=custom" },
		"php command":     func(i *Impact) { i.PHP[0].Consumer.Command = "php8.4 artisan other:command" },
		"php interpreter": func(i *Impact) { i.PHP[0].PHP.SHA256 = "changed" },
		"node command":    func(i *Impact) { i.Units[0].Command = "other-command" },
		"site":            func(i *Impact) { i.Sites[0].Domain = "changed.test" },
		"opaque daemon":   func(i *Impact) { i.BlockedReasons = []string{"late unsupported daemon"} },
		"foreign runtime": func(i *Impact) { i.BlockedReasons = []string{"wrong Node executable"} },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			changed := copyReviewImpact(t, now)
			change(&changed)
			if sameApplyConsumers(changed, old) || sameApplyConsumers(old, changed) {
				t.Fatal("review accepted changed identity, contract or original intent")
			}
		})
	}
}

func TestApplyCoordinationRetainsAllObservationsWithoutChangingApprovedPlan(t *testing.T) {
	old := reviewTestImpact()
	pid := os.Getpid()
	key, err := processKey(pid)
	if err != nil {
		t.Fatal(err)
	}
	old.PHP[0].Units[0].Runtime.PIDs[pid] = key
	original := digest(old)
	first, last := copyReviewImpact(t, old), copyReviewImpact(t, old)
	first.Units[0].PIDs = map[int]string{10: "boot:node-controller", 12: "boot:initial-job"}
	last.Units[0].PIDs = map[int]string{10: "boot:node-controller", 13: "boot:post-stage-job"}
	first.PHP[0].Units[0].Runtime.PIDs = map[int]string{20: "boot:php-controller", 22: "boot:initial-job"}
	last.PHP[0].Units[0].Runtime.PIDs = map[int]string{20: "boot:php-controller", 23: "boot:post-stage-job"}
	coordination, err := applyCoordination(old, first, last)
	if err != nil || digest(old) != original || !sameOriginalConsumers(coordination, old) {
		t.Fatal("approved plan or original intent changed", err)
	}
	for _, pid := range []int{10, 11, 12, 13} {
		if coordination.Units[0].PIDs[pid] == "" {
			t.Fatal("lost prior Node descendant", pid)
		}
	}
	for _, pid := range []int{20, 21, 22, 23} {
		if coordination.PHP[0].Units[0].Runtime.PIDs[pid] == "" {
			t.Fatal("lost prior PHP descendant", pid)
		}
	}
	retained := phpupdate.MaintenanceRuntime{PIDs: map[int]string{pid: coordination.PHP[0].Units[0].Runtime.PIDs[pid]}}
	if drained, err := phpupdate.MaintenanceDrained(retained, phpupdate.MaintenanceRuntime{State: "inactive", PIDs: map[int]string{}}); err != nil || drained {
		t.Fatal("omitted still-live reviewed descendant escaped drain responsibility", err)
	}
	coordination.PHP[0].Units[0].Definition["ExecStart"] = "mutated copy"
	coordination.Units[0].PIDs[10] = "mutated copy"
	if digest(old) != original {
		t.Fatal("coordination aliases approved history")
	}
	last.PHP[0].Units[0].Runtime.Active = false
	if _, err := applyCoordination(old, first, last); !errors.Is(err, ErrChanged) {
		t.Fatal("post-staging intent drift accepted", err)
	}
}

func TestApplyWorkerAllowsJobTurnoverAtBothPreflightsAndStopsOnTrueDrift(t *testing.T) {
	for _, at := range []int{-1, 0, 1} {
		t.Run(map[int]string{-1: "jobs only", 0: "initial drift", 1: "post-stage drift"}[at], func(t *testing.T) {
			m := testManager(t)
			owned := nodetoolchain.OwnedRuntime{Root: "/opt/fluxo/node/v24.19.0", Release: testRelease("24.19.0")}
			p := Plan{ConsumerPolicy: consumerPolicy, Kind: "apply", ID: uuid.NewString(), DiscoveryID: uuid.NewString(), CreatedAt: time.Now().Unix(), Before: owned, After: testRelease("24.20.0"), Impact: reviewTestImpact()}
			if err := savePlan(p); err != nil {
				t.Fatal(err)
			}
			if err := saveSnapshot(Snapshot{State: "available", DiscoveryID: p.DiscoveryID, CheckedAt: time.Now().Unix(), Candidate: &p.After}, 0); err != nil {
				t.Fatal(err)
			}
			m.Inspect = func(context.Context) (nodetoolchain.OwnedRuntime, error) { return owned, nil }
			observations, stages := 0, 0
			m.Impact = func(context.Context, string, string) (Impact, error) {
				i := copyReviewImpact(t, p.Impact)
				delete(i.PHP[0].Units[0].Runtime.PIDs, 21)
				i.PHP[0].Units[0].Runtime.PIDs[30+observations] = "boot:new-job"
				if observations == at {
					i.PHP[0].Units[0].Runtime.Active = false
				}
				observations++
				return i, nil
			}
			m.Stage = func(_ context.Context, r nodetoolchain.Release, _ nodetoolchain.ProgressFunc) (nodetoolchain.Release, error) {
				stages++
				return r, nil
			}
			stop := errors.New("both preflights passed; stop before generation and holds")
			m.Authenticate = func(context.Context, string, string) (nodetoolchain.Release, error) {
				return nodetoolchain.Release{}, stop
			}
			o, err := m.Enqueue(context.Background(), "apply", p.ID, uuid.NewString(), "test", "")
			if err != nil {
				t.Fatal(err)
			}
			err = m.apply(context.Background(), o)
			if at < 0 {
				if !errors.Is(err, stop) || observations != 2 || stages != 1 {
					t.Fatal("busy worker did not pass both preflights", err, observations, stages)
				}
			} else if !errors.Is(err, ErrChanged) || observations != at+1 || stages != at {
				t.Fatal("true drift passed a preflight", err, observations, stages)
			}
			stored, err := GetPlan(p.ID)
			if err != nil || digest(stored) != digest(p) {
				t.Fatal("worker changed approved plan", err)
			}
			var mutations int
			if err = database.DB.QueryRow("SELECT (SELECT COUNT(*) FROM node_update_generations)+(SELECT COUNT(*) FROM node_update_barriers)+(SELECT COUNT(*) FROM node_update_php_barriers)").Scan(&mutations); err != nil || mutations != 0 {
				t.Fatal("preflight test mutated runtime coordination", err, mutations)
			}
		})
	}
}
