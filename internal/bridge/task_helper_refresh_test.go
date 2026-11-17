package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"fluxo/internal/services/executiondomain"
)

func taskHelperRefreshFixture() executionActivation {
	p := activationPlanFixture()
	p.Format, p.Kind, p.TargetSchedulerSchema, p.TargetSystemTaskSchema = 3, activationTaskHelperRefresh, 5, 3
	f := prototypeFloorFixture()
	c, _ := activationContract(3)
	f.SchedulerSchema, f.SchedulerSupport, f.SystemTaskSupport, f.RoleContracts, f.Adapters = 5, c.SchedulerSupport, c.SystemTaskSupport, c.RoleContracts, c.Adapters
	p.SourceFloor, _ = json.Marshal(f)
	p.SourceFloorSHA256 = digest(p.SourceFloor)
	p.Files = p.Files[1:]
	return p
}

func TestTaskHelperRefreshRecoversNewIntentWhileSourceFloorIsStillCommitted(t *testing.T) {
	p := taskHelperRefreshFixture()
	var source executiondomain.ReaderFloor
	if err := json.Unmarshal(p.SourceFloor, &source); err != nil {
		t.Fatal(err)
	}
	if reuseCommittedActivation(&source, p.ID, "") {
		t.Fatal("new archived refresh was mistaken for completed source activation")
	}
	if err := validateActivationRecovery(p, p.ID, p.InstallationID, p.BinarySHA256, p.Version, &source, nil); err != nil {
		t.Fatal("exact source-phase recovery refused", err)
	}
	if !reuseCommittedActivation(&source, source.ActivationID, "") {
		t.Fatal("completed-operation recovery stopped being idempotent")
	}
	if reuseCommittedActivation(&source, "", strings.Repeat("a", 64)) {
		t.Fatal("explicit refresh skipped planning")
	}
}

func TestTaskHelperRefreshCannotBroadenAnExistingCurrentScope(t *testing.T) {
	if err := validateActivationPlan(taskHelperRefreshFixture()); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*executionActivation){
		"lost source":        func(p *executionActivation) { p.SourceFloor = nil },
		"source bytes":       func(p *executionActivation) { p.SourceFloor = append(p.SourceFloor, ' ') },
		"missing task core":  func(p *executionActivation) { p.Files = nil },
		"absent task core":   func(p *executionActivation) { p.Files[0].Before = "absent" },
		"Scheduler mutation": func(p *executionActivation) { p.Files = append(p.Files, activationPlanFixture().Files[0]) },
		"Scheduler helper": func(p *executionActivation) {
			p.Files = append(p.Files, executiondomain.ActivationFile{Path: "/var/lib/fluxo-scheduler/helpers/" + p.BinarySHA256, Image: true, Before: "absent", Mode: 0700})
		},
		"directory creation": func(p *executionActivation) {
			p.Files = append(p.Files, executiondomain.ActivationFile{Path: "/var/lib/fluxo-system-tasks/helpers", Before: "absent", Directory: true, Mode: 0700})
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := taskHelperRefreshFixture()
			mutate(&p)
			if validateActivationPlan(p) == nil {
				t.Fatal("unreviewed task-only authority accepted")
			}
		})
	}
	f := prototypeFloorFixture()
	if validateTaskHelperSourceFloor(f) == nil {
		t.Fatal("historical source granted current helper refresh")
	}
	p := taskHelperRefreshFixture()
	var current executiondomain.ReaderFloor
	_ = json.Unmarshal(p.SourceFloor, &current)
	for _, field := range []string{"phase", "roles", "support"} {
		changed := current
		switch field {
		case "phase":
			changed.Phase = "prepared"
		case "roles":
			changed.RoleContracts = map[string][]string{}
		case "support":
			changed.SchedulerSupport--
		}
		if validateTaskHelperSourceFloor(changed) == nil {
			t.Fatal("unreviewed source floor accepted", field)
		}
	}
	if !activationSourceFloorMatches(p, &current) {
		t.Fatal("exact source not recognized for recovery")
	}
	if err := validateActivationFloorTransitionWithRead("/var/lib/fluxo", p, nil, nil, func(string, int64) ([]byte, error) { return nil, nil }); err == nil {
		t.Fatal("lost source floor accepted")
	}
}

func TestTaskHelperRefreshFencesRetainedBinariesBeforeEveryWriteAndReplays(t *testing.T) {
	files := []executiondomain.ActivationFile{
		{Path: "/var/lib/fluxo-system-tasks/helpers/" + strings.Repeat("a", 64), Before: "absent", Image: true},
		{Path: "/var/lib/fluxo-system-tasks/state.json", Before: digest([]byte("old core")), Data: []byte("saved exact fresh nonce")},
		{Path: "/etc/cron.d/fluxo-cron-1", Before: digest([]byte("old trigger")), Data: []byte("saved helper and nonce"), Cron: true},
	}
	for boundary := 1; boundary <= len(files)+1; boundary++ {
		t.Run(fmt.Sprint(boundary), func(t *testing.T) {
			current := map[string]string{}
			for _, f := range files {
				current[f.Path] = f.Before
			}
			prepared, writes := false, 0
			crash := errors.New("caller died")
			observe := func() error {
				writes++
				if writes == boundary {
					return crash
				}
				return nil
			}
			write := func(f executiondomain.ActivationFile) error {
				if !prepared {
					t.Fatal("retained binary could run during task-core/helper replacement")
				}
				intended := activationIntended(f, "image")
				if current[f.Path] != f.Before && current[f.Path] != intended {
					return errors.New("outside exact intent")
				}
				current[f.Path] = intended
				return observe()
			}
			prepare := func() error { prepared = true; return observe() }
			if err := applyTaskHelperRefreshTransitions(context.Background(), files, write, prepare); !errors.Is(err, crash) {
				t.Fatal(err)
			}
			if err := applyTaskHelperRefreshTransitions(context.Background(), files, write, prepare); err != nil {
				t.Fatal(err)
			}
			for _, f := range files {
				if current[f.Path] != activationIntended(f, "image") {
					t.Fatal("exact recovery did not converge")
				}
			}
		})
	}
}
