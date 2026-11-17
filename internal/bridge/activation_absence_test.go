package bridge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"fluxo/internal/services/executiondomain"
)

func absenceFixture(schedulerAbsent, taskAbsent bool) executionActivation {
	p := activationPlanFixture()
	for _, base := range []string{"/var/lib/fluxo-scheduler", "/var/lib/fluxo-system-tasks"} {
		absent := schedulerAbsent
		if base == "/var/lib/fluxo-system-tasks" {
			absent = taskAbsent
		}
		if !absent {
			continue
		}
		for i := range p.Files {
			if p.Files[i].Path == base+"/state.json" {
				p.Files[i].Before = "absent"
			}
		}
		p.Auxiliary[base+".initialized"] = "absent"
		p.Files = append(p.Files, executiondomain.ActivationFile{Path: base + ".initialized", Before: "absent", Data: []byte("1\n"), Fence: []byte("activation:" + p.ID + "\n"), Mode: 0600}, executiondomain.ActivationFile{Path: base, Before: "absent", Directory: true, Mode: 0700})
		gate := base + ".lock"
		if base == "/var/lib/fluxo-scheduler" {
			gate = base + "/gate.lock"
			p.Files = append(p.Files, executiondomain.ActivationFile{Path: base + "/dispatcher", Before: "absent", Image: true, Mode: 0700})
		} else {
			p.Files = append(p.Files, executiondomain.ActivationFile{Path: base + "/helpers", Before: "absent", Directory: true, Mode: 0700})
		}
		p.Files = append(p.Files, executiondomain.ActivationFile{Path: gate, Before: "absent", Lock: true, Mode: 0600})
	}
	return p
}

// Model durable writes at each boundary, including crashes
// after every directory/gate/marker creation. The same production ordering is
// replayed. This is a state-machine proof, not fabricated native cron evidence.
func TestCleanAbsenceActivationCrashReplay(t *testing.T) {
	for _, which := range [][2]bool{{false, false}, {true, false}, {false, true}, {true, true}} {
		p := absenceFixture(which[0], which[1])
		if err := validateActivationPlan(p); err != nil {
			t.Fatal(err)
		}
		maxWrites := len(p.Files) + 1
		for _, f := range p.Files {
			if len(f.Fence) > 0 {
				maxWrites++
			}
		}
		for interrupt := 1; interrupt <= maxWrites; interrupt++ {
			t.Run(fmt.Sprintf("absent=%v/after=%d", which, interrupt), func(t *testing.T) {
				current := map[string]string{}
				for _, f := range p.Files {
					current[f.Path] = f.Before
				}
				writes := 0
				prepared := false
				crash := errors.New("interrupted after durable boundary")

				checkpoint := func() error {
					writes++
					if writes == interrupt {
						return crash
					}
					return nil
				}
				write := func(f executiondomain.ActivationFile) error {
					if f.PublishFence {
						if current[f.Path] == digest(f.Data) {
							return checkpoint()
						}
						current[f.Path] = digest(f.Fence)
					} else if f.Directory {
						current[f.Path] = "directory"
					} else {
						current[f.Path] = activationIntended(f, "image")
					}
					return checkpoint()
				}
				prepare := func() error {
					for _, f := range p.Files {
						if strings.HasSuffix(f.Path, "/state.json") && current[f.Path] != activationIntended(f, "image") {
							t.Fatal("floor preceded both durable current cores", f.Path)
						}
					}
					prepared = true
					return checkpoint()
				}
				err := applyExecutionTransitions(context.Background(), p.Files, write, prepare)
				if errors.Is(err, crash) {
					if err = applyExecutionTransitions(context.Background(), p.Files, write, prepare); err != nil {
						t.Fatal("exact replay failed", err)
					}
				} else {
					t.Fatal("durable interruption boundary was not exercised", err)
				}
				if !prepared {
					t.Fatal("current floor never reached")
				}
				for _, f := range p.Files {
					want := activationIntended(f, "image")
					if f.Directory {
						want = "directory"
					}
					if current[f.Path] != want {
						t.Fatal("replay omitted intended target", f.Path)
					}
				}
			})
		}
	}
}

func TestAbsencePlanCannotInventHistoryOrIgnoreMarkers(t *testing.T) {
	p := absenceFixture(true, true)
	for _, change := range []func(*executionActivation){func(p *executionActivation) {
		p.Auxiliary["/var/lib/fluxo-scheduler.initialized"] = strings.Repeat("a", 64)
	}, func(p *executionActivation) {
		for i := range p.Files {
			if len(p.Files[i].Fence) > 0 {
				p.Files[i].Fence = []byte("activation:other\n")
				return
			}
		}
	}, func(p *executionActivation) { p.Files = p.Files[:2] }} {
		copyPlan := p
		copyPlan.Files = append([]executiondomain.ActivationFile{}, p.Files...)
		copyPlan.Auxiliary = map[string]string{}
		for k, v := range p.Auxiliary {
			copyPlan.Auxiliary[k] = v
		}
		change(&copyPlan)
		if validateActivationPlan(copyPlan) == nil {
			t.Fatal("absence creation accepted missing/conflicting operation evidence")
		}
	}
}

// A historical initializer which already owned admission can finish before our
// newly acquired gate. Its real state must win over our stale absence intent:
// sealing refuses before any empty current core could overwrite that history.
func TestCompetingInitializerHistoryRefusesBeforeCurrentCorePublication(t *testing.T) {
	p := absenceFixture(true, true)
	competing := errors.New("older initializer published nonempty task history")
	prepared, currentCoreWrites := false, 0
	seal := func() error { return competing }
	err := applyExecutionTransitions(context.Background(), p.Files, func(f executiondomain.ActivationFile) error {
		if strings.HasSuffix(f.Path, "/state.json") {
			currentCoreWrites++
		}
		return nil
	}, func() error { prepared = true; return nil }, seal)
	if !errors.Is(err, competing) || prepared || currentCoreWrites != 0 {
		t.Fatal("stale absence plan overwrote late initializer state", err, currentCoreWrites, prepared)
	}
}
