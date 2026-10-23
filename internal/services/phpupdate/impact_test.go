package phpupdate

import (
	"strings"
	"testing"
)

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
