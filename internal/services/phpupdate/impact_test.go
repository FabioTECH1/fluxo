package phpupdate

import (
	"context"
	"strings"
	"testing"

	"fluxo/internal/database"
)

func TestPHPReviewMessagesIdentifyDomainsAndIndependentProcessNames(t *testing.T) {
	testStore(t)
	if _, err := database.DB.Exec(`INSERT INTO sites(id,domain,path,php_version,app_type) VALUES
		(8,'hotel.example.test','/home/fluxo/hotel','8.4','laravel'),
		(9,'shop.example.test','/home/fluxo/shop','8.4','laravel')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`INSERT INTO daemons(id,site_id,name,managed_kind,command,directory,user,instances) VALUES
		(1,8,'Queue Worker','','/home/fluxo/wrapper.sh','/home/fluxo/hotel','fluxo',1),
		(2,9,'Queue Worker','','/home/fluxo/wrapper.sh','/home/fluxo/shop','fluxo',1),
		(3,8,'Independent queue','','php8.4 artisan queue:work','/home/fluxo/hotel','fluxo',1),
		(4,8,'Report generator','','php8.4 artisan reports:generate','/home/fluxo/hotel','fluxo',1),
		(5,8,'Nightwatch','laravel_nightwatch','php8.4 artisan nightwatch:agent --listen-on=127.0.0.1:2407','/home/fluxo/hotel','fluxo',1),
		(6,8,'Custom Nightwatch','laravel_nightwatch','php8.4 artisan nightwatch:agent --custom','/home/fluxo/hotel','fluxo',1)`); err != nil {
		t.Fatal(err)
	}
	// Metadata messages are produced before host PHP/package inspection. This
	// exercises the real review path without requiring or changing host services.
	impact, _ := inspectImpact(context.Background(), "8.4")
	blockers := strings.Join(impact.BlockedReasons, "\n")
	notes := strings.Join(impact.Notes, "\n")
	for _, want := range []string{`"Queue Worker" on hotel.example.test`, `"Queue Worker" on shop.example.test`, `"Custom Nightwatch" on hotel.example.test`} {
		if !strings.Contains(blockers, want) {
			t.Fatalf("missing readable blocker %q: %s", want, blockers)
		}
	}
	for _, want := range []string{`"Independent queue" on hotel.example.test`, `"Report generator" on hotel.example.test`, `"Nightwatch" on hotel.example.test`} {
		if !strings.Contains(notes, want) {
			t.Fatalf("missing readable note %q: %s", want, notes)
		}
	}
	for _, internalLabel := range []string{"site #", "daemon #", "cron #", "fluxo-daemon-"} {
		if strings.Contains(blockers+notes, internalLabel) {
			t.Fatalf("internal identity exposed in review: %s\n%s", blockers, notes)
		}
	}
}

func TestNodePHPBlockersIdentifyDomainsWithoutHidingOrphans(t *testing.T) {
	testStore(t)
	if _, err := database.DB.Exec(`INSERT INTO sites(id,domain,path,php_version,app_type) VALUES(8,'bridge.example.test','/home/fluxo/bridge','8.4','laravel')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`INSERT INTO daemons(id,site_id,name,command,directory) VALUES
		(1,8,'Bridge','/home/fluxo/wrapper.sh','/home/fluxo/bridge'),
		(2,999,'Orphaned bridge','/home/fluxo/wrapper.sh','/home/fluxo/missing')`); err != nil {
		t.Fatal(err)
	}
	for _, recovery := range []bool{false, true} {
		_, reasons, err := InspectMaintenanceConsumers(context.Background(), nil, MaintenanceInspection{Recovery: recovery})
		if err != nil || len(reasons) != 2 {
			t.Fatal("unsupported consumers no longer blocked", reasons, err)
		}
		if !strings.Contains(reasons[0], `"Bridge" on bridge.example.test`) || !strings.Contains(reasons[1], `"Orphaned bridge" (no site domain available)`) {
			t.Fatal("blockers do not identify the affected process/domain", reasons)
		}
	}
}

func TestWorkerOwnershipCannotBeInferredFromLabelOrCommand(t *testing.T) {
	for _, name := range []string{"Queue Worker", "Laravel Queue Worker", "Nightwatch"} {
		c := Consumer{ID: 1, SiteID: 8, Name: name, Command: "php8.4 artisan queue:work --sleep=3 --tries=3 --timeout=90", Instances: 1}
		if issue := workerMetadataIssue("8.4", c, "fluxo"); !strings.Contains(issue, "no verified") {
			t.Fatalf("custom entry %q inferred as managed: %q", name, issue)
		}
		// A second, explicitly managed entry does not adopt or replace #1.
		managed := c
		managed.ID, managed.Kind = 4, "laravel_queue"
		if issue := workerMetadataIssue("8.4", managed, "fluxo"); issue != "" {
			t.Fatal(issue)
		}
		if c.Kind != "" || workerMetadataIssue("8.4", c, "fluxo") == "" {
			t.Fatal("managed duplicate changed the original ownership")
		}
		site := Site{ID: 8, Path: "/home/fluxo/hotel", AppType: "laravel", Strategy: "standard"}
		c.Directory = site.Path
		if !qualifyCustomQueue("8.4", c, site) {
			t.Fatal("supported custom queue did not qualify independently")
		}
		c.Protocol = "laravel_queue"
		if c.Kind != "" || workerMetadataIssue("8.4", c, "fluxo") != "" {
			t.Fatal("maintenance protocol changed ownership or rejected a supported queue")
		}
	}
}

func TestWorkerMetadataExplainsIndependentContractFailures(t *testing.T) {
	base := Consumer{Kind: "laravel_queue", Command: "php8.4 artisan queue:work", Instances: 1}
	for _, tc := range []struct {
		name, user, want string
		mutate           func(*Consumer)
	}{
		{"wrong service user", "www-data", "fluxo service user", func(*Consumer) {}},
		{"generic interpreter", "fluxo", "explicit php8.4", func(c *Consumer) { c.Command = "php artisan queue:work" }},
		{"other branch", "fluxo", "explicit php8.4", func(c *Consumer) { c.Command = "php8.3 artisan queue:work" }},
		{"zero instances", "fluxo", "1 and 64", func(c *Consumer) { c.Instances = 0 }},
		{"too many instances", "fluxo", "1 and 64", func(c *Consumer) { c.Instances = 65 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			tc.mutate(&c)
			if issue := workerMetadataIssue("8.4", c, tc.user); !strings.Contains(issue, tc.want) {
				t.Fatalf("got %q, want %q", issue, tc.want)
			}
		})
	}
}
