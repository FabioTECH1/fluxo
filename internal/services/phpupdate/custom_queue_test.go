package phpupdate

import (
	"strings"
	"testing"
)

func TestCustomQueueProtocolRecognizesLegacyAndExplicitConnections(t *testing.T) {
	for _, command := range []string{
		"php8.4 artisan queue:work",
		"php8.4 artisan queue:work --sleep=3 --tries=3 --timeout=90",
		"/usr/bin/php8.4 artisan queue:work redis --queue=high,default --sleep=3 --tries=3 --timeout=60 --backoff=0 --memory=128 --max-time=3600 --force",
		"php8.4 artisan queue:work sqs-eu --queue=notifications:priority --tries=0 --max-time=0",
	} {
		t.Run(command, func(t *testing.T) {
			args, ok := customQueueArguments("8.4", command)
			if !ok || args[0] != "php8.4" || args[2] != "queue:work" {
				t.Fatalf("supported command rejected: %v %v", args, ok)
			}
		})
	}
}

func TestCustomQueueProtocolRejectsAmbiguousOrUnknownLaunchers(t *testing.T) {
	for _, command := range []string{
		"env php8.4 artisan queue:work", "sh -c 'php8.4 artisan queue:work'",
		"php artisan queue:work", "/opt/php8.4 artisan queue:work", "php8.3 artisan queue:work",
		"php8.4 -d auto_prepend_file=custom.php artisan queue:work",
		"php8.4 artisan migrate", "php8.4 artisan queue:listen", "php8.4 artisan queue:restart",
		"php8.4 artisan queue:work sync", "php8.4 artisan queue:work NULL",
		"php8.4 artisan queue:work redis other", "php8.4 artisan queue:work redis --unknown=value",
		"php8.4 artisan queue:work --sleep=3 --sleep=5", "php8.4 artisan queue:work --force=true",
		"php8.4 artisan queue:work --queue=default,", "php8.4 artisan queue:work --queue=default;id",
		"php8.4 artisan queue:work --queue=$(id)", "php8.4 artisan queue:work --queue=$QUEUE",
		"php8.4 artisan queue:work --timeout=0", "php8.4 artisan queue:work --timeout=999999999999999999999999",
		"php8.4 artisan queue:work --sleep=-1", "php8.4 artisan queue:work --tries=1e2",
		"php8.4 artisan queue:work\n/bin/true", "php8.4 artisan queue:work --queue='default'",
		"php8.4 artisan queue:work --queue=" + strings.Repeat("x", 65),
	} {
		if _, ok := customQueueArguments("8.4", command); ok {
			t.Fatalf("unsupported command accepted: %q", command)
		}
	}
}

func TestCustomQueueMustBeBoundToItsExistingSiteDirectory(t *testing.T) {
	base := Consumer{ID: 1, SiteID: 8, Directory: "/home/fluxo/app", Command: "php8.4 artisan queue:work"}
	site := Site{ID: 8, Path: base.Directory, AppType: "laravel", Strategy: "standard"}
	for _, tc := range []struct {
		name   string
		change func(*Consumer, *Site)
	}{
		{"orphan", func(c *Consumer, s *Site) { s.ID = 0 }},
		{"global", func(c *Consumer, s *Site) { c.SiteID = 0 }},
		{"other site", func(c *Consumer, s *Site) { s.ID = 9 }},
		{"unknown owner", func(c *Consumer, s *Site) { c.Kind = "custom" }},
		{"managed owner", func(c *Consumer, s *Site) { c.Kind = "laravel_queue" }},
		{"other directory", func(c *Consumer, s *Site) { c.Directory = "/home/fluxo/other" }},
		{"non PHP site", func(c *Consumer, s *Site) { s.AppType = "node" }},
		{"outside applications", func(c *Consumer, s *Site) { s.Path = "/tmp/app"; c.Directory = s.Path }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, s := base, site
			tc.change(&c, &s)
			if qualifyCustomQueue("8.4", c, s) {
				t.Fatal("unsupported binding qualified")
			}
		})
	}
	site.Strategy = "zero-downtime"
	if qualifyCustomQueue("8.4", base, site) {
		t.Fatal("release root accepted instead of active directory")
	}
	base.Directory += "/current"
	if !qualifyCustomQueue("8.4", base, site) {
		t.Fatal("active directory rejected")
	}
}

func TestCustomQueueRetainsFullEffectiveUnitContract(t *testing.T) {
	for _, prefix := range []string{"php8.4", "/usr/bin/php8.4"} {
		c := Consumer{SiteID: 8, Protocol: "laravel_queue", Command: prefix + " artisan queue:work --sleep=3 --tries=3 --timeout=90", Directory: "/home/fluxo/app", Instances: 1}
		base := map[string]string{"Type": "simple", "User": "fluxo", "WorkingDirectory": c.Directory, "Restart": "always", "ExecStart": "{ path=/usr/bin/php8.4 ; argv[]=" + c.Command + " ; ignore_errors=no }"}
		if err := validateUnitDefinition("8.4", &c, base); err != nil {
			t.Fatal(err)
		}
		for _, change := range []map[string]string{
			{"User": "www-data"}, {"Environment": "PHPRC=/opt/php.ini"}, {"ExecStop": "{ path=/bin/kill ; argv[]=/bin/kill ; ignore_errors=no }"},
			{"DropInPaths": "/etc/systemd/system/worker.service.d/custom.conf"}, {"WorkingDirectory": "/home/fluxo/other"},
			{"ExecStart": "{ path=/opt/php8.4 ; argv[]=" + c.Command + " ; ignore_errors=no }"},
			{"ExecStart": "{ path=/usr/bin/php8.4 ; argv[]=php8.4 artisan queue:work redis ; ignore_errors=no }"},
		} {
			p := map[string]string{}
			for k, v := range base {
				p[k] = v
			}
			for k, v := range change {
				p[k] = v
			}
			if validateUnitDefinition("8.4", &c, p) == nil {
				t.Fatalf("custom unit contract relaxed: %+v", change)
			}
		}
	}
}

func TestRecoveryRejectsChangedCustomQueueMeaning(t *testing.T) {
	c := Consumer{ID: 1, SiteID: 8, Protocol: "laravel_queue", Command: "php8.4 artisan queue:work redis --queue=high", Directory: "/home/fluxo/app", Instances: 1}
	before := Impact{Workers: []Consumer{c}, UnitHash: "verified unit"}
	for _, change := range []func(*Consumer){func(c *Consumer) { c.Protocol = "" }, func(c *Consumer) { c.Kind = "laravel_queue"; c.Protocol = "" }, func(c *Consumer) { c.Command = "php8.4 artisan queue:work redis --queue=default" }, func(c *Consumer) { c.Directory = "/home/fluxo/other" }, func(c *Consumer) { c.Instances = 2 }} {
		changed := c
		change(&changed)
		if recoveryCompatible(before, Impact{Workers: []Consumer{changed}, UnitHash: before.UnitHash}) == nil {
			t.Fatal("changed custom consumer accepted by old recovery approval")
		}
	}
}
