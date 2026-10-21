package phpupdate

import "testing"

func TestEffectiveServiceContractRejectsUnusedDistributionInterpreter(t *testing.T) {
	base := map[string]string{"Id": "php8.4-fpm.service", "Type": "notify", "ExecStart": "{ path=/usr/sbin/php-fpm8.4 ; argv[]=/usr/sbin/php-fpm8.4 --nodaemonize --fpm-config /etc/php/8.4/fpm/php-fpm.conf ; ignore_errors=no ; start_time=[n/a] }"}
	if err := validateUnitDefinition("8.4", nil, base); err != nil {
		t.Fatal(err)
	}
	for _, change := range []map[string]string{
		{"DropInPaths": "/etc/systemd/system/php8.4-fpm.service.d/custom.conf"},
		{"ExecStart": "{ path=/opt/php/sbin/php-fpm8.4 ; argv[]=/opt/php/sbin/php-fpm8.4 --nodaemonize --fpm-config /etc/php/8.4/fpm/php-fpm.conf ; ignore_errors=no }"},
		{"Environment": "PHPRC=/opt/php.ini"},
		{"ExecStartPre": "{ path=/opt/script ; argv[]=/opt/script ; ignore_errors=no }"},
		{"ExecStartPost": "{ path=/opt/script ; argv[]=/opt/script ; ignore_errors=no }"},
	} {
		p := map[string]string{}
		for k, v := range base {
			p[k] = v
		}
		for k, v := range change {
			p[k] = v
		}
		if validateUnitDefinition("8.4", nil, p) == nil {
			t.Fatalf("accepted unsupported service: %+v", change)
		}
	}
	c := Consumer{Kind: "laravel_queue", Command: "php8.4 artisan queue:work database --timeout=600", Directory: "/home/fluxo/app"}
	q := map[string]string{"Type": "simple", "User": "fluxo", "WorkingDirectory": c.Directory, "Restart": "always", "ExecStart": "{ path=/usr/bin/php8.4 ; argv[]=php8.4 artisan queue:work database --timeout=600 ; ignore_errors=no }"}
	if err := validateUnitDefinition("8.4", &c, q); err != nil {
		t.Fatal(err)
	}
	q["ExecStart"] = "{ path=/opt/php8.4 ; argv[]=php8.4 artisan queue:work database --timeout=600 ; ignore_errors=no }"
	if validateUnitDefinition("8.4", &c, q) == nil {
		t.Fatal("custom queue interpreter accepted")
	}
	if queueHasTerminationHandler("SigCgt:\t0000000000000000\n") || !queueHasTerminationHandler("SigCgt:\t0000000000004000\n") {
		t.Fatal("queue SIGTERM disposition check incorrect")
	}
}

func TestSupervisorRequiresKnownTerminalState(t *testing.T) {
	for _, state := range []string{"active", "activating", "deactivating", "reloading", "unknown", ""} {
		if supervisorTerminal(map[string]string{"ActiveState": state, "LoadState": "loaded", "MainPID": "0", "ControlPID": "0"}) {
			t.Fatalf("released nonterminal %s", state)
		}
	}
	for _, state := range []string{"inactive", "failed"} {
		if !supervisorTerminal(map[string]string{"ActiveState": state, "LoadState": "loaded", "MainPID": "0", "ControlPID": "0"}) {
			t.Fatalf("terminal state rejected %s", state)
		}
		if supervisorTerminal(map[string]string{"ActiveState": state, "LoadState": "loaded", "MainPID": "1", "ControlPID": "0"}) {
			t.Fatal("live main process ignored")
		}
	}
}

func TestHorizonCannotMasqueradeAsAnotherArtisanCommand(t *testing.T) {
	c := Consumer{Kind: "laravel_horizon", Command: "php8.4 artisan horizon", Directory: "/home/fluxo/app"}
	p := map[string]string{"Type": "simple", "User": "fluxo", "WorkingDirectory": c.Directory, "Restart": "always", "ExecStart": "{ path=/usr/bin/php8.4 ; argv[]=php8.4 artisan horizon ; ignore_errors=no }"}
	if err := validateUnitDefinition("8.4", &c, p); err != nil {
		t.Fatal(err)
	}
	c.Command = "php8.4 artisan migrate"
	p["ExecStart"] = "{ path=/usr/bin/php8.4 ; argv[]=php8.4 artisan migrate ; ignore_errors=no }"
	if validateUnitDefinition("8.4", &c, p) == nil {
		t.Fatal("unrelated artisan command accepted as a managed Horizon runtime")
	}
}
