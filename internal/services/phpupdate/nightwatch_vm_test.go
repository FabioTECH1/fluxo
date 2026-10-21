package phpupdate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"fluxo/internal/syscmd"
)

func TestVMOfficialNightwatchStopsRestartsAndPreservesInactiveAndOtherBranch(t *testing.T) {
	requireRecoveryVM(t)
	m := testStore(t)
	const app = "/home/fluxo/php-nightwatch-acceptance"
	if _, err := os.Stat(filepath.Join(app, "vendor/laravel/nightwatch/agent/build/agent.phar")); err != nil {
		t.Fatal("prepare the separate pinned Nightwatch 1.30.4 acceptance application first:", err)
	}
	// The official agent talks only to this local failing authentication stub.
	// No real Nightwatch account, token, ingestion or remote traffic is used.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "acceptance unavailable", 503) }))
	defer stub.Close()
	envPath := filepath.Join(app, ".env")
	old, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(string(old), "\n") {
		if !strings.HasPrefix(line, "NIGHTWATCH_") && !strings.HasPrefix(line, "LOG_CHANNEL=") && !strings.HasPrefix(line, "LOG_STACK=") {
			lines = append(lines, line)
		}
	}
	lines = append(lines, "NIGHTWATCH_TOKEN=acceptance-placeholder", "NIGHTWATCH_BASE_URL="+stub.URL, "LOG_CHANNEL=single")
	vmApplicationFile(t, envPath, strings.Join(lines, "\n")+"\n")
	cache := filepath.Join(app, "bootstrap/cache/config.php")
	vmApplicationFile(t, cache, "<?php return [];\n")
	if err = os.Remove(cache); err != nil {
		t.Fatal(err)
	}
	active := Consumer{ID: 9109201, Name: "Nightwatch", Kind: "laravel_nightwatch", Command: "php8.4 artisan nightwatch:agent --listen-on=127.0.0.1:41921", Directory: app, Instances: 1, Port: 41921}
	stopped := active
	stopped.ID = 9109202
	stopped.Port = 41922
	stopped.Command = strings.Replace(active.Command, "41921", "41922", 1)
	other := active
	other.ID = 9109203
	other.Port = 41923
	other.Command = "php8.3 artisan nightwatch:agent --listen-on=127.0.0.1:41923"
	// The Laravel fixture requires PHP 8.4.1. Exercise the other-branch agent
	// using the same official self-contained PHAR, without that application's
	// Composer platform check. This tests branch isolation, not Laravel on 8.3.
	other.Directory = "/home/fluxo/php-nightwatch-other-acceptance"
	if err := os.MkdirAll(other.Directory, 0755); err != nil {
		t.Fatal(err)
	}
	vmApplicationFile(t, filepath.Join(other.Directory, "artisan"), "<?php $refreshToken='acceptance-placeholder';$baseUrl="+strconv.Quote(stub.URL)+";$listenOn='127.0.0.1:41923';require '/home/fluxo/php-nightwatch-acceptance/vendor/laravel/nightwatch/agent/build/agent.phar';\n")
	for _, c := range []Consumer{active, stopped, other} {
		vmUnit(t, unitName(c.ID, 1), "/usr/bin/"+c.Command, c.Directory)
	}
	vmRun(t, "/usr/bin/systemctl", "start", unitName(active.ID, 1), unitName(other.ID, 1))
	waitAgent := func(branch string, c Consumer) unitSnapshot {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for {
			u, e := unitState(context.Background(), unitName(c.ID, 1))
			if e == nil && u.Active && validateUnitProcesses(context.Background(), branch, &c, u) == nil && verifyListenerOwnership(context.Background(), u, c.Port) == nil {
				return u
			}
			if time.Now().After(deadline) {
				t.Fatalf("Nightwatch agent/listener did not qualify (port %s): %v", strconv.Itoa(c.Port), e)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	a := waitAgent("8.4", active)
	unaffected := waitAgent("8.3", other)
	if verifyListenerOwnership(context.Background(), a, other.Port) == nil {
		t.Fatal("another branch's listener accepted as this agent")
	}
	s, err := unitState(context.Background(), unitName(stopped.ID, 1))
	if err != nil {
		t.Fatal(err)
	}
	out := vmRun(t, "/usr/bin/systemctl", "show", a.Name, "--property="+inspectedUnitProperties)
	if err := validateUnitDefinition("8.4", &active, unitProperties(out)); err != nil {
		t.Fatal(err)
	}
	g := vmGeneration(t, m, []unitSnapshot{a, s})
	if err := m.drainConsumers(context.Background(), &g); err != nil {
		t.Fatal(err)
	}
	for _, u := range g.Original.Units {
		current, err := observeUnit(context.Background(), u.Name, u.CGroup)
		if err != nil {
			t.Fatal(err)
		}
		if drained, err := fullyDrained(u, current, processKey); err != nil || !drained {
			t.Fatalf("agent not fully stopped: %v", err)
		}
		vmRun(t, "/usr/bin/systemctl", "start", u.Name)
		current, err = unitState(context.Background(), u.Name)
		if err != nil || current.Active || len(current.PIDs) > 0 {
			t.Fatal("manual start bypassed persistent hold", err)
		}
	}
	still, err := unitState(context.Background(), unaffected.Name)
	if err != nil || !sameOriginalMain(unaffected, still) {
		t.Fatal("other PHP branch was changed", err)
	}
	if err := m.startConsumers(context.Background(), &g); err != nil {
		t.Fatal(err)
	}
	restarted := waitAgent("8.4", active)
	if sameOriginalMain(a, restarted) {
		t.Fatal("original Nightwatch process survived restart")
	}
	if err := processCurrent(restarted.MainPID); err != nil {
		t.Fatal(err)
	}
	if err := m.releaseBarriers(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	still, err = unitState(context.Background(), s.Name)
	if err != nil || still.Active || len(still.PIDs) > 0 {
		t.Fatal("stopped agent started", err)
	}
	g.State, g.Pending = "succeeded", false
	if err := saveGeneration(g); err != nil {
		t.Fatal(err)
	}
	if err := finish(g.Head, "succeeded", "Nightwatch acceptance completed"); err != nil {
		t.Fatal(err)
	}
}

// A fault-injected PHP telemetry fixture delays TERM completion. The official
// Nightwatch test above covers the real package; this verifies interrupted
// drainage and the same resumable barrier path used by Repair.
func TestVMNightwatchDrainInterruptionKeepsHoldAndResumesWithoutForceKill(t *testing.T) {
	requireRecoveryVM(t)
	m := testStore(t)
	dir, err := os.MkdirTemp("/home/fluxo", "nightwatch-drain-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	cred, err := syscmd.ResolveCredential("fluxo")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(dir, int(cred.Uid), int(cred.Gid)); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "delay")
	if err := os.WriteFile(marker, []byte("delay\n"), 0644); err != nil {
		t.Fatal(err)
	}
	vmApplicationFile(t, filepath.Join(dir, "artisan"), `<?php
pcntl_async_signals(true);
pcntl_signal(SIGTERM,function(){file_put_contents(__DIR__.'/signals', 'TERM\n', FILE_APPEND);if(file_exists(__DIR__.'/delay')){pcntl_alarm(3);}else{exit(0);}});
pcntl_signal(SIGALRM,function(){file_put_contents(__DIR__.'/normal-exit','deadline');exit(0);});
while(true){usleep(50000);}
`)
	c := Consumer{ID: 9109211, Kind: "laravel_nightwatch", Command: "php8.4 artisan nightwatch:agent --listen-on=127.0.0.1:41931", Directory: dir, Instances: 1, Port: 41931}
	active, stopped := unitName(c.ID, 1), unitName(c.ID+1, 1)
	vmUnit(t, active, "/usr/bin/"+c.Command, dir)
	vmUnit(t, stopped, "/usr/bin/"+c.Command, dir)
	vmRun(t, "/usr/bin/systemctl", "start", active)
	time.Sleep(200 * time.Millisecond)
	a, err := unitState(context.Background(), active)
	if err != nil || !a.Active {
		t.Fatal("fixture not running", err)
	}
	s, err := unitState(context.Background(), stopped)
	if err != nil {
		t.Fatal(err)
	}
	g := vmGeneration(t, m, []unitSnapshot{a, s})
	for _, u := range g.Attempt.Units {
		if err := m.prepareBarrier(context.Background(), g, u); err != nil {
			t.Fatal(err)
		}
	}
	before := vmRun(t, "/usr/bin/dpkg-query", "-W", "-f=${Version}", "php8.4-cli")
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	err = m.drainConsumers(ctx, &g)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("delayed shutdown did not interrupt: %v", err)
	}
	for _, u := range g.Attempt.Units {
		b, err := getBarrier(u.Name)
		if err != nil || verifyHeldBarrier(context.Background(), b) != nil {
			t.Fatal("interruption lost hold", err)
		}
	}
	if after := vmRun(t, "/usr/bin/dpkg-query", "-W", "-f=${Version}", "php8.4-cli"); after != before {
		t.Fatal("drain interruption changed PHP packages")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	// Resume observes the existing systemd stop job; repeating TERM here would
	// shorten this fixture's original three-second shutdown deadline.
	if err := m.drainConsumers(context.Background(), &g); err != nil {
		t.Fatal(err)
	}
	signals, err := os.ReadFile(filepath.Join(dir, "signals"))
	if err != nil || strings.Count(string(signals), "TERM") != 1 {
		t.Fatalf("TERM repeated during resume: %q %v", signals, err)
	}
	exited, err := os.ReadFile(filepath.Join(dir, "normal-exit"))
	if err != nil || string(exited) != "deadline" {
		t.Fatalf("original shutdown did not finish by its own deadline: %q %v", exited, err)
	}
	if err := m.startConsumers(context.Background(), &g); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, err := unitState(context.Background(), active)
		if err == nil && current.Active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("resume did not restore originally active agent", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	current, err := unitState(context.Background(), stopped)
	if err != nil || current.Active {
		t.Fatal("resume started originally stopped agent", err)
	}
	if err := m.releaseBarriers(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	g.State, g.Pending = "succeeded", false
	if err := saveGeneration(g); err != nil {
		t.Fatal(err)
	}
}
