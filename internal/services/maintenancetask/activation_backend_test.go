package maintenancetask

import (
	"context"
	"encoding/json"
	"fluxo/internal/services/executiondomain/testfixture"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/services/scheduler"
	"github.com/google/uuid"
)

func activationBackendFixture() scheduler.CronBackendIdentity {
	return scheduler.CronBackendIdentity{Backend: "cronie-noble-1", Boot: uuid.NewString(), PID: 40, Start: "400", Group: "/system.slice/cronie.service", GroupInode: 1, Configuration: strings.Repeat("a", 64)}
}

func TestFirstTaskConversionAllowsQualifiedPriorBootWithoutWideningRefresh(t *testing.T) {
	saved := activationBackendFixture()
	current := saved
	current.Boot, current.PID, current.Start, current.GroupInode = uuid.NewString(), 50, "500", 2
	for _, schema := range []int{1, 2} {
		if err := validateActivationBackendContinuity(schema, saved, current); err != nil {
			t.Fatal("first conversion refused unchanged contract on a new boot", schema, err)
		}
	}
	if err := validateActivationBackendContinuity(3, saved, current); err == nil {
		t.Fatal("current helper refresh acquired backend renewal authority")
	}
	for _, schema := range []int{1, 2, 3} {
		if err := validateActivationBackendContinuity(schema, saved, saved); err != nil {
			t.Fatal("unchanged authentic identity refused", schema, err)
		}
	}
}

func TestFirstTaskConversionRejectsChangedProviderOrConfiguration(t *testing.T) {
	for _, name := range []string{"same-boot-restart", "configuration", "provider", "group", "invalid-saved", "invalid-current", "unknown-source"} {
		t.Run(name, func(t *testing.T) {
			saved := activationBackendFixture()
			current := saved
			current.Boot, current.PID, current.Start, current.GroupInode = uuid.NewString(), 50, "500", 2
			schema := 1
			switch name {
			case "same-boot-restart":
				current.Boot = saved.Boot
			case "configuration":
				current.Configuration = strings.Repeat("b", 64)
			case "provider":
				current.Backend, current.Group = "ubuntu-jammy-cron-v1", "/system.slice/cron.service"
			case "group":
				current.Group = "/system.slice/cron.service"
			case "invalid-saved":
				saved.PID = 0
			case "invalid-current":
				current.PID = 0
			case "unknown-source":
				schema = 4
			}
			if err := validateActivationBackendContinuity(schema, saved, current); err == nil {
				t.Fatal("changed authority admitted by first conversion")
			}
		})
	}
}

func TestInstallerPlansEnabledLegacyTaskWithoutRetirementWitness(t *testing.T) {
	if !testfixture.InRoot(t, "testdata") {
		return
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	source, err := os.ReadFile("/inputs/v040-cronie-schema1.json")
	must(err)
	var j journal
	must(json.Unmarshal(source, &j))
	r := j.Entries[9]
	hostBoot, err := bootID()
	must(err)
	helper := []byte("retained fixture helper")
	r.Helper = filepath.Join(stateRoot, "helpers", digest(helper))
	must(os.MkdirAll(filepath.Dir(r.Helper), 0700))
	must(atomicFile(r.Helper, helper, 0700))
	must(atomicFile(stateRoot+".initialized", []byte("1\n"), 0600))
	must(atomicFile(stateRoot+".lock", []byte(readyGate), 0600))
	for _, priorBoot := range []bool{false, true} {
		r.Boot = hostBoot
		if priorBoot {
			r.Boot = uuid.NewString()
		}
		r.Backend.Boot = r.Boot
		j.Entries[9] = r
		must(save(j))
		must(atomicFile(cronPath(9), []byte(cronContent(r)), 0644))
		var proofs []scheduler.CronBackendRetirement
		current := r.Backend
		current.Boot = hostBoot
		if priorBoot {
			current.PID++
			current.GroupInode++
		}
		emptyChecks := 0
		image := strings.Repeat("c", 64)
		files, err := planTaskActivationWithBackend(t.Context(), image, uuid.NewString(), &proofs,
			func(_ context.Context, saved scheduler.CronBackendIdentity) (scheduler.CronBackendIdentity, error) {
				if saved != r.Backend {
					t.Fatal("lost saved provider")
				}
				return current, nil
			},
			func(_ context.Context, got scheduler.CronBackendIdentity) error {
				if got != current {
					t.Fatal("checked wrong provider")
				}
				emptyChecks++
				return nil
			})
		must(err)
		if len(proofs) != 0 || (emptyChecks == 1) != priorBoot {
			t.Fatal("incorrect retirement or drainage requirement", priorBoot, proofs, emptyChecks)
		}
		before, err := readRegular(filepath.Join(stateRoot, "state.json"), 8<<20)
		must(err)
		must(ValidateInstallerHistoricalActivationPlan(before, files[0].Data, image, files, proofs))
		var target journal
		must(json.Unmarshal(files[0].Data, &target))
		entry := target.Entries[9]
		if target.Schema != 3 || entry.Received || entry.Nonce == r.Nonce || entry.Backend != current {
			t.Fatal("invalid planned task3 registration")
		}
	}
}
