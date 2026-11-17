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

// Every durable write can succeed and then lose its caller. Replay must retain
// the reader fence and converge using only original/intended target states.
func TestExecutionActivationCrashReplay(t *testing.T) {
	files := []executiondomain.ActivationFile{
		{Path: "/var/lib/fluxo-system-tasks/helpers/current", Before: "absent", Image: true},
		{Path: "/var/lib/fluxo-system-tasks/state.json", Before: digest([]byte("task1")), Data: []byte("task3")},
		{Path: "/etc/cron.d/fluxo-cron-1", Before: digest([]byte("old trigger")), Data: []byte("fresh nonce")},
		{Path: "/var/lib/fluxo-scheduler/dispatcher", Before: digest([]byte("old image")), Image: true},
		{Path: "/var/lib/fluxo-scheduler/state.json", Before: digest([]byte("scheduler2")), Data: []byte("scheduler4")},
	}
	for interruption := 1; interruption <= len(files)+1; interruption++ {
		t.Run(fmt.Sprintf("after-write=%d", interruption), func(t *testing.T) {
			current := map[string]string{}
			for _, f := range files {
				current[f.Path] = f.Before
			}
			writes, prepared := 0, false
			crash := errors.New("caller died after durable write")
			observe := func() error {
				if prepared && (current["/var/lib/fluxo-scheduler/state.json"] != digest([]byte("scheduler4")) || current["/var/lib/fluxo-system-tasks/state.json"] != digest([]byte("task3"))) {
					t.Fatal("prepared floor preceded BOTH current core fences")
				}
				writes++
				if writes == interruption {
					return crash
				}
				return nil
			}
			write := func(f executiondomain.ActivationFile) error {
				intended := activationIntended(f, "new image")
				if current[f.Path] != f.Before && current[f.Path] != intended {
					return errors.New("target changed outside operation")
				}
				current[f.Path] = intended
				return observe()
			}
			prepare := func() error { prepared = true; return observe() }
			if err := applyExecutionTransitions(context.Background(), files, write, prepare); !errors.Is(err, crash) {
				t.Fatal("interruption boundary not exercised", err)
			}
			if err := applyExecutionTransitions(context.Background(), files, write, prepare); err != nil {
				t.Fatal("exact replay", err)
			}
			for _, f := range files {
				if current[f.Path] != activationIntended(f, "new image") {
					t.Fatal("replay omitted target")
				}
			}
		})
	}
}

func TestExecutionActivationFailureNeverPublishesPreparedFloorEarly(t *testing.T) {
	for _, failPath := range []string{"helper", "/var/lib/fluxo-scheduler/state.json", "/var/lib/fluxo-system-tasks/state.json"} {
		prepared := false
		err := applyExecutionTransitions(context.Background(), []executiondomain.ActivationFile{
			{Path: "helper", Image: true}, {Path: "/var/lib/fluxo-scheduler/state.json"}, {Path: "/var/lib/fluxo-system-tasks/state.json"},
		}, func(f executiondomain.ActivationFile) error {
			if f.Path == failPath {
				return errors.New("disk failure")
			}
			return nil
		}, func() error { prepared = true; return nil })
		if err == nil || prepared {
			t.Fatal("floor preceded compatible helpers and durable old-reader fence")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := applyExecutionTransitions(ctx, nil, nil, func() error { t.Fatal("cancelled activation wrote floor"); return nil }); err == nil {
		t.Fatal("cancelled activation accepted")
	}
}

func activationPlanFixture() executionActivation {
	p := executionActivation{Format: 2, ID: "1af605c7-82ca-4245-9fc5-1240cae7d8ab", InstallationID: strings.Repeat("a", 32), BinarySHA256: strings.Repeat("b", 64), Version: "0.0.0-review", Database: map[string]string{"/var/lib/fluxo/fluxo.db": strings.Repeat("c", 64), "/var/lib/fluxo/fluxo.db-wal": "absent"}, Auxiliary: map[string]string{}}
	for _, path := range []string{"/var/lib/fluxo-scheduler.initialized", "/var/lib/fluxo-system-tasks.initialized", "/var/lib/fluxo-scheduler/backend-proofs.json", "/var/lib/fluxo-scheduler/backend-proofs.initialized", "/var/lib/fluxo-scheduler/backend-proofs.initializing"} {
		p.Auxiliary[path] = "absent"
	}
	p.Files = []executiondomain.ActivationFile{
		{Path: "/var/lib/fluxo-scheduler/state.json", Before: strings.Repeat("d", 64), Data: []byte(`{"schema":4,"entries":{},"invocations":{}}`), Mode: 0600},
		{Path: "/var/lib/fluxo-system-tasks/state.json", Before: strings.Repeat("e", 64), Data: []byte(`{"schema":3,"entries":{},"invocations":{}}`), Mode: 0600},
	}
	p.Auxiliary["/var/lib/fluxo-scheduler.initialized"] = digest([]byte("1\n"))
	p.Auxiliary["/var/lib/fluxo-system-tasks.initialized"] = digest([]byte("1\n"))
	return p
}

func TestExecutionActivationPlanRefusesUnownedTargetsAndInvalidIdentity(t *testing.T) {
	if err := validateActivationPlan(activationPlanFixture()); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []func(*executionActivation){
		func(p *executionActivation) { p.ID = "../other" },
		func(p *executionActivation) { p.BinarySHA256 = strings.Repeat("Z", 64) },
		func(p *executionActivation) { p.InstallationID = strings.Repeat("A", 32) },
		func(p *executionActivation) {
			p.Files = []executiondomain.ActivationFile{{Path: "/etc/shadow", Before: "absent", Mode: 0600}}
		},
		func(p *executionActivation) {
			p.Files = []executiondomain.ActivationFile{{Path: "/var/lib/fluxo-scheduler/state.json", Before: "absent", Mode: 0600}}
		},
		func(p *executionActivation) {
			p.Files = []executiondomain.ActivationFile{{Path: "/var/lib/fluxo-system-tasks/helpers/" + strings.Repeat("c", 64), Before: "absent", Image: true, Mode: 0700}}
		},
	} {
		p := activationPlanFixture()
		mutation(&p)
		if validateActivationPlan(p) == nil {
			t.Fatal("accepted changed activation identity or unowned transition")
		}
	}
}

func TestExecutionActivationRecoveryBindsExactOperationImageAndPlan(t *testing.T) {
	p := activationPlanFixture()
	raw, _ := json.Marshal(p)
	f := executiondomain.ReaderFloor{Phase: "prepared", ActivationID: p.ID, InstallationID: p.InstallationID, BinarySHA256: p.BinarySHA256, PlanSHA256: digest(raw)}
	check := func(p executionActivation, resume, installation, image, version string, floor *executiondomain.ReaderFloor, raw []byte) error {
		return validateActivationRecovery(p, resume, installation, image, version, floor, raw)
	}
	if err := check(p, p.ID, p.InstallationID, p.BinarySHA256, p.Version, &f, raw); err != nil {
		t.Fatal(err)
	}
	if err := check(p, p.ID, p.InstallationID, p.BinarySHA256, p.Version, nil, raw); err != nil {
		t.Fatal("exact pre-floor first-core recovery must remain available", err)
	}
	for _, args := range [][4]string{
		{"other-operation", p.InstallationID, p.BinarySHA256, p.Version},
		{p.ID, strings.Repeat("d", 32), p.BinarySHA256, p.Version},
		{p.ID, p.InstallationID, strings.Repeat("e", 64), p.Version},
		{p.ID, p.InstallationID, p.BinarySHA256, "0.0.1-review"},
	} {
		if check(p, args[0], args[1], args[2], args[3], &f, raw) == nil {
			t.Fatal("different operation/installation/image/version acquired recovery authority")
		}
	}
	changed := append(append([]byte{}, raw...), ' ')
	if check(p, p.ID, p.InstallationID, p.BinarySHA256, p.Version, &f, changed) == nil {
		t.Fatal("stale intent acquired prepared-floor authority")
	}
	for _, field := range []string{"phase", "operation", "image", "installation", "plan"} {
		next := f
		switch field {
		case "phase":
			next.Phase = "committed"
		case "operation":
			next.ActivationID = "other-operation"
		case "image":
			next.BinarySHA256 = strings.Repeat("f", 64)
		case "installation":
			next.InstallationID = strings.Repeat("f", 32)
		case "plan":
			next.PlanSHA256 = strings.Repeat("f", 64)
		}
		if check(p, p.ID, p.InstallationID, p.BinarySHA256, p.Version, &next, raw) == nil {
			t.Fatalf("different floor %s accepted", field)
		}
	}
}
