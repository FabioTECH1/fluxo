package phpupdate

import "testing"

func TestRecoveryCannotLoseOrChangeAnApprovedNaturalConsumer(t *testing.T) {
	p := Plan{Version: "8.4", Impact: Impact{Workers: []Consumer{{ID: 1, Instances: 1, Protocol: "artisan_natural"}, {ID: 2, Instances: 1, Kind: "laravel_queue"}}}}
	r := runtimeSnapshot{Units: []unitSnapshot{{Name: "php8.4-fpm.service"}, {Name: unitName(1, 1), Natural: true}, {Name: unitName(2, 1)}}}
	g := recoveryGeneration{Version: p.Version, Original: r, Attempt: r}
	if err := validateConsumerProtocols(p, g); err != nil {
		t.Fatal(err)
	}
	for _, units := range [][]unitSnapshot{
		{{Name: "php8.4-fpm.service"}, {Name: unitName(1, 1)}, {Name: unitName(2, 1)}},
		{{Name: "php8.4-fpm.service"}, {Name: unitName(2, 1)}},
		{{Name: "php8.4-fpm.service"}, {Name: unitName(1, 1), Natural: true}, {Name: unitName(1, 1), Natural: true}},
	} {
		changed := g
		changed.Attempt.Units = units
		if validateConsumerProtocols(p, changed) == nil {
			t.Fatal("changed drain mode or incomplete consumer inventory accepted", units)
		}
	}
}

func TestNaturalCompletionRequiresBoundDurableExecutionEvidence(t *testing.T) {
	const boot = "c69076c5-6522-4956-a055-da79eded9d51"
	for _, r := range []naturalResume{
		{}, {Boot: "unknown", After: 1}, {Boot: boot},
		{Boot: boot, After: 10, Completed: true},
		{Boot: boot, After: 10, Invocation: boot + ":9:123", Completed: true},
		{Boot: boot, After: 10, Invocation: boot + ":11:0", Completed: true},
		{Boot: boot, After: 10, Invocation: "another-boot:11:123", Completed: true},
	} {
		if validateNaturalResume(r) == nil {
			t.Fatal("invalid or incomplete completion evidence accepted", r)
		}
	}
	if err := validateNaturalResume(naturalResume{Boot: boot, After: 10, Invocation: boot + ":11:123", Completed: true}); err != nil {
		t.Fatal(err)
	}
}

func TestOtherArtisanCommandsUseLiteralNaturalDrainProtocol(t *testing.T) {
	for _, command := range []string{
		"php8.4 artisan reports:generate --format=json --all",
		"/usr/bin/php8.4 artisan reverb:start --host=127.0.0.1 --port=8080",
		"php8.4 artisan queue:listen redis --queue=other",
		"php8.4 artisan custom:worker account-123 -- --literal",
		"php8.4 artisan migrate --force",
	} {
		c := Consumer{SiteID: 8, Protocol: "artisan_natural", Command: command, Directory: "/home/fluxo/app", Instances: 1}
		args, ok := customArtisanArguments("8.4", command)
		if !ok || args[0] != "php8.4" || workerMetadataIssue("8.4", c, "fluxo") != "" {
			t.Fatal("literal Artisan command rejected", command)
		}
		p := map[string]string{"Type": "simple", "User": "fluxo", "WorkingDirectory": c.Directory, "Restart": "always", "ExecStart": "{ path=/usr/bin/php8.4 ; argv[]=" + command + " ; ignore_errors=no }"}
		if err := validateUnitDefinition("8.4", &c, p); err != nil {
			t.Fatal(err)
		}
		p["ExecStop"] = "{ path=/usr/bin/custom-stop ; argv[]=/usr/bin/custom-stop ; ignore_errors=no }"
		if validateUnitDefinition("8.4", &c, p) == nil {
			t.Fatal("natural drainage relaxed custom lifecycle hook rejection")
		}
	}
	for _, command := range []string{
		"php artisan reports:generate", "php8.3 artisan reports:generate",
		"env php8.4 artisan reports:generate", "php8.4 -d auto_prepend_file=x artisan reports:generate",
		"php8.4 artisan --version", "php8.4 other.php reports:generate",
		"php8.4 artisan reports:generate --name='hello world'", "php8.4 artisan reports:generate --name=$NAME",
		"php8.4 artisan reports:generate --name=%i", "php8.4 artisan reports:generate;id",
		"php8.4 artisan reports:generate --name=foo\n--all", "php8.4 artisan reports:generate | cat",
	} {
		if _, ok := customArtisanArguments("8.4", command); ok {
			t.Fatal("ambiguous launcher or expansion accepted", command)
		}
	}
}

func TestNaturalProtocolCannotOverrideManagedOwnershipOrOtherProtocols(t *testing.T) {
	c := Consumer{Kind: "laravel_queue", Protocol: "artisan_natural", Command: "php8.4 artisan reports:generate", Instances: 1}
	if workerMetadataIssue("8.4", c, "fluxo") == "" {
		t.Fatal("custom protocol replaced managed ownership")
	}
	c.Kind, c.Protocol = "", "unrecognized"
	if workerMetadataIssue("8.4", c, "fluxo") == "" {
		t.Fatal("unknown protocol accepted")
	}
	before := Impact{Workers: []Consumer{{ID: 7, SiteID: 8, Protocol: "artisan_natural", Command: "php8.4 artisan reports:generate", Instances: 1}}, UnitHash: "unit"}
	after := before
	after.Workers = []Consumer{before.Workers[0]}
	after.Workers[0].Protocol = "laravel_queue"
	if recoveryCompatible(before, after) == nil {
		t.Fatal("recovery changed its approved shutdown protocol")
	}
}
