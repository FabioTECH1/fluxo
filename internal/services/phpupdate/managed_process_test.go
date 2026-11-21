package phpupdate

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestManagedProcessPolicyIsGenericAndLegacyReviewsStayNatural(t *testing.T) {
	site := Site{ID: 8, Path: "/home/fluxo/app", AppType: "laravel"}
	for _, command := range []string{"php8.4 artisan reverb:start --port=8080", "php8.4 artisan custom:worker tenant-1", "/usr/bin/php8.4 worker.php --queue=custom"} {
		current := Consumer{ID: 9, SiteID: 8, Directory: site.Path, Command: command, Instances: 1, StopSeconds: 42}
		customProcessProtocol("8.4", &current, site, ManagedProcessPolicy, "SIGINT")
		if !isManagedProcess(&current) || current.StopSignal != "SIGINT" || current.StopSeconds != 42 || current.Kind != "" || current.Command != command {
			t.Fatal("managed shutdown changed ownership/command or ignored recorded settings", current)
		}
		if issue := workerMetadataIssue("8.4", current, "fluxo"); issue != "" {
			t.Fatal(issue)
		}
	}
	old := Consumer{SiteID: 8, Directory: site.Path, Command: "php8.4 artisan reverb:start", Instances: 1, StopSeconds: 15}
	customProcessProtocol("8.4", &old, site, 0, "SIGINT")
	if old.Protocol != "artisan_natural" || old.StopSignal != "" {
		t.Fatal("old review acquired signal authority", old)
	}
	if policy, err := managedStopPolicy(&old); err != nil || policy != nil {
		t.Fatal(policy, err)
	}
}

func TestManagedProcessRejectsAmbiguousLaunchAndForcedKill(t *testing.T) {
	for _, command := range []string{"sh -c php8.4 artisan reverb:start", "php artisan worker", "php8.3 worker.php", "php8.4 -n worker.php", "php8.4 -d auto_prepend_file=/tmp/custom.php worker.php", "php8.4 -i", "php8.4 artisan worker | cat", "php8.4 artisan worker --tenant=$TENANT", "php8.4 artisan worker %i", "php8.4 artisan worker\nwhoami"} {
		if _, ok := managedPHPArguments("8.4", command); ok {
			t.Fatal("ambiguous launch admitted", command)
		}
	}
	for _, policy := range []*ManagedStopPolicy{{Signal: "SIGKILL", Seconds: 15}, {Signal: "SIGTERM\nExecStop=bad", Seconds: 15}, {Signal: "SIGTERM", Seconds: 0}, {Signal: "SIGTERM", Seconds: 86401}} {
		if policy.Validate() == nil {
			t.Fatal("invalid shutdown policy accepted", policy)
		}
	}
}

func TestManagedStopAuthorityIsBoundToBothRetainedSnapshots(t *testing.T) {
	c := Consumer{ID: 1, SiteID: 8, Protocol: managedProcessProtocol, Command: "php8.4 artisan reverb:start", Instances: 1, StopSignal: "SIGTERM", StopSeconds: 15}
	p := Plan{Version: "8.4", Impact: Impact{MaintenancePolicy: ManagedProcessPolicy, Workers: []Consumer{c}}}
	u := unitSnapshot{Name: unitName(1, 1), Natural: true, ManagedStop: &ManagedStopPolicy{Signal: "SIGTERM", Seconds: 15}}
	runtime := runtimeSnapshot{Units: []unitSnapshot{{Name: "php8.4-fpm.service"}, u}}
	g := recoveryGeneration{Version: p.Version, Original: runtime, Attempt: runtime}
	if err := validateConsumerProtocols(p, g); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*unitSnapshot){func(u *unitSnapshot) { u.ManagedStop = nil }, func(u *unitSnapshot) { u.Natural = false }, func(u *unitSnapshot) { u.ManagedStop = &ManagedStopPolicy{Signal: "SIGINT", Seconds: 15} }, func(u *unitSnapshot) { u.ManagedStop = &ManagedStopPolicy{Signal: "SIGTERM", Seconds: 16} }} {
		var altered recoveryGeneration
		raw, _ := json.Marshal(g)
		if err := json.Unmarshal(raw, &altered); err != nil {
			t.Fatal(err)
		}
		change(&altered.Attempt.Units[1])
		if validateConsumerProtocols(p, altered) == nil {
			t.Fatal("altered shutdown authority admitted", altered.Attempt)
		}
	}
	p.Impact.MaintenancePolicy = 0
	if validateConsumerProtocols(p, g) == nil {
		t.Fatal("new shutdown authority accepted under an unmarked old plan")
	}
}

func TestManagedBarrierUsesApprovedSignalWithoutForcedTermination(t *testing.T) {
	marker := "/etc/systemd/system/fluxo-daemon-1.service.d/hold"
	legacy, err := barrierContent(marker)
	if err != nil {
		t.Fatal(err)
	}
	current, err := barrierContentFor(marker, &ManagedStopPolicy{Signal: "SIGINT", Seconds: 42})
	if err != nil {
		t.Fatal(err)
	}
	if current != strings.Replace(strings.Replace(legacy, "KillSignal=SIGTERM", "KillSignal=SIGINT", 1), "KillMode=mixed", "KillMode=control-group", 1) {
		t.Fatal("barrier changed unrelated legacy safeguards", current)
	}
	for _, retained := range []string{"Restart=no", "KillMode=control-group", "SendSIGKILL=no", "TimeoutStopSec=infinity"} {
		if !strings.Contains(current, retained) {
			t.Fatal("lost safeguard", retained)
		}
	}
	if _, err = barrierContentFor(marker, &ManagedStopPolicy{Signal: "SIGKILL", Seconds: 15}); err == nil {
		t.Fatal("forced kill barrier admitted")
	}
}

func TestManagedDeadlineIsBoundedAndIdentifiesTheActualProcess(t *testing.T) {
	u := unitSnapshot{Natural: true, ManagedStop: &ManagedStopPolicy{Signal: "SIGINT", Seconds: 42}}
	if err := MaintenanceStopExpired(u, 41*time.Second, "Reverb on app.test"); err != nil {
		t.Fatal(err)
	}
	err := MaintenanceStopExpired(u, 42*time.Second, "Reverb on app.test")
	if err == nil || !strings.Contains(err.Error(), "Reverb on app.test") || !strings.Contains(err.Error(), "SIGINT") {
		t.Fatal(err)
	}
	if err := MaintenanceStopExpired(unitSnapshot{Natural: true}, 24*time.Hour, "legacy"); err != nil {
		t.Fatal("legacy observation changed", err)
	}
	u.ManagedStop.Seconds = 600
	if MaintenanceDrainWindow([]MaintenanceRuntime{u}) != 10*time.Minute {
		t.Fatal("configured longer grace was truncated")
	}
}

func TestManagedSystemdTimeoutMustMatchTheRecordedSetting(t *testing.T) {
	for _, tc := range []struct {
		value   string
		seconds int
		want    bool
	}{{"15s", 15, true}, {"1min 30s", 90, true}, {"1d", 86400, true}, {"infinity", 15, false}, {"1min", 15, false}, {"999999999d", 15, false}} {
		if got := managedTimeoutMatches(tc.value, tc.seconds); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}

func TestDebuggerProcessIsPartOfTheActualBranchRuntime(t *testing.T) {
	for _, path := range []string{"/usr/bin/phpdbg8.5", "/usr/bin/phpdbg8.5 (deleted)", "/usr/bin/php8.5", "/usr/sbin/php-fpm8.5"} {
		if !branchRuntimeProcess(path, "8.5") {
			t.Fatal(path)
		}
	}
	for _, path := range []string{"/usr/bin/phpdbg8.4", "/usr/bin/php8.4", "/usr/bin/node"} {
		if branchRuntimeProcess(path, "8.5") {
			t.Fatal(path)
		}
	}
}

func TestManagedServiceBindsActualSignalTimeoutAndLaunch(t *testing.T) {
	c := Consumer{Protocol: managedProcessProtocol, Command: "php8.4 artisan reverb:start --port=8080", Directory: "/home/fluxo/app", StopSignal: "SIGINT", StopSeconds: 42}
	base := map[string]string{"Type": "simple", "User": "fluxo", "WorkingDirectory": c.Directory, "Restart": "always", "ExecStart": "{ path=/usr/bin/php8.4 ; argv[]=php8.4 artisan reverb:start --port=8080 ; ignore_errors=no }", "KillSignal": "2", "TimeoutStopUSec": "42s", "KillMode": "control-group"}
	if err := validateUnitDefinition("8.4", &c, base); err != nil {
		t.Fatal(err)
	}
	for _, change := range []map[string]string{{"KillSignal": "15"}, {"TimeoutStopUSec": "15s"}, {"KillMode": "process"}, {"ExecStop": "/custom/shutdown"}, {"ExecStart": "{ path=/usr/bin/php8.4 ; argv[]=php8.4 artisan another:command ; ignore_errors=no }"}} {
		p := map[string]string{}
		for key, value := range base {
			p[key] = value
		}
		for key, value := range change {
			p[key] = value
		}
		if validateUnitDefinition("8.4", &c, p) == nil {
			t.Fatal("actual shutdown/launch drift accepted", change)
		}
	}
}
