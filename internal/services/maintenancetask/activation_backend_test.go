package maintenancetask

import (
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
