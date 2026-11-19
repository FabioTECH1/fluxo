package nginx

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func trafficFixture(t *testing.T) (trafficEnvironment, TrafficSite, *int, *int) {
	t.Helper()
	base := t.TempDir()
	if err := os.Chmod(base, 0700); err != nil {
		t.Fatal(err)
	}
	available, enabled, conf := filepath.Join(base, "available"), filepath.Join(base, "enabled"), filepath.Join(base, "conf.d")
	for _, dir := range []string{available, enabled, conf, filepath.Join(base, "logs")} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	site := TrafficSite{ID: 7, ConfigName: "site-original.test", Domain: "site.test", Generation: "0123456789abcdef", LogPath: filepath.Join(base, "logs", "generation.jsonl")}
	raw := "server {\n listen 80;\n server_name site.test alias.test;\n root /srv/site/public;\n access_log /var/log/nginx/site.access.log;\n location / { try_files $uri $uri/ /index.php?$query_string; }\n location = /favicon.ico { access_log off; }\n}\n"
	if err := os.WriteFile(filepath.Join(available, site.ConfigName), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(available, site.ConfigName), filepath.Join(enabled, site.ConfigName)); err != nil {
		t.Fatal(err)
	}
	validations, reloads := new(int), new(int)
	env := trafficEnvironment{available: available, enabled: enabled, httpPath: filepath.Join(conf, "fluxo-traffic.conf"), stateDir: filepath.Join(base, "state"), strict: false,
		validate: func(context.Context) error { *validations++; return nil }, reload: func(context.Context) error { *reloads++; return nil },
		dump: func(context.Context) (string, error) {
			return "# configuration file /etc/nginx/nginx.conf:\nhttp { access_log /var/log/nginx/access.log combined; }\n", nil
		},
		processes: func(context.Context) (trafficProcesses, error) {
			return trafficProcesses{Master: TrafficWorker{10, "123", "boot"}, Workers: []TrafficWorker{{11, "124", "boot"}}}, nil
		},
		probe: func(context.Context, string, string) error { return nil }, drained: func(context.Context, []TrafficWorker) (bool, error) { return false, nil }}
	return env, site, validations, reloads
}

func TestTrafficNeverEnabledIsStrictNoop(t *testing.T) {
	env, _, validations, reloads := trafficFixture(t)
	env.processes = func(context.Context) (trafficProcesses, error) {
		t.Fatal("OFF inspected workers")
		return trafficProcesses{}, nil
	}
	if _, err := applyTraffic(context.Background(), env, nil); err != nil {
		t.Fatal(err)
	}
	if *validations != 0 || *reloads != 0 {
		t.Fatal("OFF ran nginx")
	}
	if _, err := os.Lstat(env.stateDir); !os.IsNotExist(err) {
		t.Fatalf("OFF created control state: %v", err)
	}
}

func TestTrafficApplyEnableDisableAndWorkerDrain(t *testing.T) {
	env, site, validations, reloads := trafficFixture(t)
	path := filepath.Join(env.available, site.ConfigName)
	baseline, _ := os.ReadFile(path)
	result, err := applyTraffic(context.Background(), env, []TrafficSite{site})
	if err != nil {
		t.Fatal(err)
	}
	if result.AppliedAt.IsZero() || len(result.RetiringWorkers) != 1 || *validations != 1 || *reloads != 1 {
		t.Fatalf("bad activation: %+v", result)
	}
	active, _ := os.ReadFile(path)
	if !bytes.Contains(active, []byte(site.LogPath)) {
		t.Fatal("producer absent")
	}
	if !bytes.Contains(active, []byte("access_log /var/log/nginx/site.access.log;")) {
		t.Fatal("operational logging changed")
	}
	off, err := applyTraffic(context.Background(), env, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(off.RetiringWorkers) != 1 {
		t.Fatal("OFF claimed drained while old worker may still write")
	}
	restored, _ := os.ReadFile(path)
	if !bytes.Equal(restored, baseline) {
		t.Fatalf("OFF changed vhost bytes:\n%s", restored)
	}
	global, _ := os.ReadFile(env.httpPath)
	if bytes.Contains(global, []byte("log_format")) || bytes.Contains(global, []byte("map ")) {
		t.Fatal("OFF retained analytics producer")
	}
	env.drained = func(context.Context, []TrafficWorker) (bool, error) { return true, nil }
	result, err = applyTraffic(context.Background(), env, nil)
	if err != nil || len(result.RetiringWorkers) != 0 {
		t.Fatalf("drained status: %+v %v", result, err)
	}
}

func TestTrafficApplyRollsBackValidationReloadAndProbeFailures(t *testing.T) {
	for _, failure := range []string{"validation", "reload", "activation"} {
		t.Run(failure, func(t *testing.T) {
			env, site, _, reloads := trafficFixture(t)
			path := filepath.Join(env.available, site.ConfigName)
			baseline, _ := os.ReadFile(path)
			fail := errors.New("injected " + failure)
			switch failure {
			case "validation":
				env.validate = func(context.Context) error { return fail }
			case "reload":
				env.reload = func(context.Context) error {
					*reloads++
					if *reloads == 1 {
						return fail
					}
					return nil
				}
			case "activation":
				env.probe = func(context.Context, string, string) error { return fail }
			}
			result, err := applyTraffic(context.Background(), env, []TrafficSite{site})
			if err == nil || !result.AppliedAt.IsZero() {
				t.Fatalf("toggle succeeded on %s: %+v %v", failure, result, err)
			}
			restored, _ := os.ReadFile(path)
			if !bytes.Equal(restored, baseline) {
				t.Fatalf("rollback did not restore exact vhost: %s", restored)
			}
			if _, err := os.Lstat(env.httpPath); !os.IsNotExist(err) {
				t.Fatalf("new global config survived rollback: %v", err)
			}
		})
	}
}

func TestTrafficOffFailurePreservesPreviouslyEnabledProducer(t *testing.T) {
	env, site, _, _ := trafficFixture(t)
	if _, err := applyTraffic(context.Background(), env, []TrafficSite{site}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(env.available, site.ConfigName))
	env.validate = func(context.Context) error { return errors.New("invalid unrelated config") }
	if _, err := applyTraffic(context.Background(), env, nil); err == nil {
		t.Fatal("OFF falsely succeeded")
	}
	after, _ := os.ReadFile(filepath.Join(env.available, site.ConfigName))
	if !bytes.Equal(before, after) {
		t.Fatal("failed OFF changed previous producer")
	}
	state, err := readTrafficState(env)
	if err != nil || len(state.Sites) != 1 {
		t.Fatalf("failed OFF changed state: %+v %v", state, err)
	}
}

func TestTrafficConfigPreservesGeneratedAppTypesRedirectsAndCustomizations(t *testing.T) {
	_, site, _, _ := trafficFixture(t)
	for _, app := range []string{"html", "php", "wordpress", "node", "python", "python-django"} {
		t.Run(app, func(t *testing.T) {
			raw := renderHostGroupsWithPool("site.test", "/srv/custom-root/public", "8.4", app, 3010, "stable-pool", "/fallback/cert", "/fallback/key", []hostGroup{{certPath: "/real/cert", keyPath: "/real/key", domains: []string{"site.test", "alias.test"}}, {domains: []string{"www.site.test"}, redirectTo: "site.test"}})
			raw += "# a retained operator comment\n"
			active, err := injectTrafficConfig(raw, site, []string{"access_log /var/log/nginx/access.log combined;"})
			if err != nil {
				t.Fatal(err)
			}
			restored, err := stripTrafficConfig(active)
			if err != nil || restored != raw {
				t.Fatalf("vhost changed beyond managed additions: %v", err)
			}
			if strings.Count(active, "access_log "+site.LogPath) != strings.Count(raw, "server {")+strings.Count(raw, "access_log off;") {
				t.Fatalf("not every hostname/server block is covered:\n%s", active)
			}
			if !strings.Contains(active, "access_log /var/log/nginx/access.log combined;") {
				t.Fatal("redirect operational inheritance was lost")
			}
		})
	}
	raw := "server { server_name site.test; access_log /tmp/operational; location /custom { access_log /tmp/custom custom; return 204; } }"
	active, err := injectTrafficConfig(raw, site, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(active, "access_log "+site.LogPath) != 2 {
		t.Fatal("location-specific logs silently override analytics")
	}
}

func TestTrafficCustomAndMalformedConfigurationsRefuseWithoutMutation(t *testing.T) {
	_, site, _, _ := trafficFixture(t)
	for _, raw := range []string{"server { server_name site.test; include custom.conf; }", "server { server_name site.test; access_log off; access_log /tmp/custom; }", "upstream app { server localhost; }", "server { server_name site.test; location / {", "server { server_name site.test; }"} {
		if _, err := injectTrafficConfig(raw, site, nil); err == nil {
			t.Fatalf("accepted unsupported config: %s", raw)
		}
	}
	if _, err := stripTrafficConfig("server {\n" + trafficMarker + "\n root /elsewhere;\n" + trafficEndMarker + "\n}"); err == nil {
		t.Fatal("removed modified managed block")
	}
}

func TestTrafficRegenerationAndRestartReapplyPersistedConfiguration(t *testing.T) {
	env, site, _, reloads := trafficFixture(t)
	if _, err := applyTraffic(context.Background(), env, []TrafficSite{site}); err != nil {
		t.Fatal(err)
	}
	generated := "server { server_name promoted.test alias.test; root /new/public; access_log /tmp/operational; }\n"
	updated, err := preserveTrafficConfig(env, site.ConfigName, generated)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated, site.LogPath) || !strings.Contains(updated, "/new/public") {
		t.Fatal("regeneration removed producer or site update")
	}
	if err := os.WriteFile(filepath.Join(env.available, site.ConfigName), []byte(updated), 0644); err != nil {
		t.Fatal(err)
	}
	// A new controller must reload and prove activation even if disk already
	// equals desired. Old state is not proof of workers loaded after a restart.
	probes := 0
	env.probe = func(context.Context, string, string) error { probes++; return nil }
	if _, err := applyTraffic(context.Background(), env, []TrafficSite{site}); err != nil {
		t.Fatal(err)
	}
	if *reloads != 2 || probes != 1 {
		t.Fatal("restart trusted disk without worker confirmation")
	}
}

func TestTrafficRecoversPartialPublicationBeforeNextApply(t *testing.T) {
	env, site, _, _ := trafficFixture(t)
	if err := trafficEnsurePrivateDirectory(env.stateDir, false); err != nil {
		t.Fatal(err)
	}
	operation := "abcdef0123456789"
	after := trafficState{Operation: operation, Sites: []TrafficSite{site}, InheritedLogs: []string{"access_log /tmp/op;"}}
	changes, err := planTrafficChanges(env, trafficState{}, after)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTrafficJSON(env, "intent.json", trafficIntent{Version: 1, Operation: operation, Changes: changes}); err != nil {
		t.Fatal(err)
	}
	if err := applyTrafficChange(env, changes[0], false); err != nil {
		t.Fatal(err)
	}
	if _, err := applyTraffic(context.Background(), env, nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.available, site.ConfigName))
	if strings.Contains(string(raw), trafficMarker) {
		t.Fatal("recovery retained uncommitted producer")
	}
}

func TestTrafficSymlinkHardlinkAndWritablePathsRejected(t *testing.T) {
	for _, kind := range []string{"vhost-symlink", "vhost-hardlink", "log-symlink", "writable-parent", "custom-enabled"} {
		t.Run(kind, func(t *testing.T) {
			env, site, _, _ := trafficFixture(t)
			path := filepath.Join(env.available, site.ConfigName)
			switch kind {
			case "vhost-symlink":
				raw, _ := os.ReadFile(path)
				other := path + ".held"
				os.WriteFile(other, raw, 0644)
				os.Remove(path)
				os.Symlink(other, path)
			case "vhost-hardlink":
				os.Link(path, path+".other")
			case "log-symlink":
				os.Symlink(path, site.LogPath)
			case "writable-parent":
				os.Chmod(filepath.Dir(site.LogPath), 0777)
			case "custom-enabled":
				os.Remove(filepath.Join(env.enabled, site.ConfigName))
				os.Symlink("/tmp/custom-nginx", filepath.Join(env.enabled, site.ConfigName))
			}
			if _, err := applyTraffic(context.Background(), env, []TrafficSite{site}); err == nil {
				t.Fatalf("accepted %s", kind)
			}
		})
	}
}

func TestTrafficInheritedPolicyExpandsHTTPIncludesAndRejectsParameterOverrides(t *testing.T) {
	dump := "# configuration file /etc/nginx/nginx.conf:\nhttp { include /etc/nginx/conf.d/*.conf; }\n# configuration file /etc/nginx/conf.d/logs.conf:\naccess_log /tmp/op1 combined; access_log /tmp/op2 combined if=$condition;\n# configuration file /etc/nginx/conf.d/site.conf:\nserver { server_name example.test; location / { include fastcgi_params; } }\n# configuration file /etc/nginx/fastcgi_params:\nfastcgi_param QUERY_STRING $query_string;\n"
	logs, err := trafficInheritedLogs(dump)
	if err != nil || len(logs) != 2 {
		t.Fatalf("inheritance: %v %v", logs, err)
	}
	if _, err := trafficInheritedLogs(dump + "access_log /tmp/unexpected;\n"); err == nil {
		t.Fatal("allowed hidden logging override in fastcgi_params")
	}
}

func TestTrafficWorkerRetirementIsBoundToStartAndBoot(t *testing.T) {
	raw, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Skip(err)
	}
	boot := strings.TrimSpace(string(raw))
	worker, err := trafficProcessIdentity(os.Getpid(), boot)
	if err != nil {
		t.Fatal(err)
	}
	if drained, err := TrafficWorkersDrained(context.Background(), []TrafficWorker{worker}); err != nil || drained {
		t.Fatalf("live identity was retired: %v %v", drained, err)
	}
	reused := worker
	reused.StartTime += "0"
	if drained, err := TrafficWorkersDrained(context.Background(), []TrafficWorker{reused}); err != nil || !drained {
		t.Fatalf("reused PID was treated as old worker: %v %v", drained, err)
	}
	rebooted := worker
	rebooted.BootID = "a-prior-boot"
	if drained, err := TrafficWorkersDrained(context.Background(), []TrafficWorker{rebooted}); err != nil || !drained {
		t.Fatalf("reboot reused old PID: %v %v", drained, err)
	}
	if _, err := TrafficWorkersDrained(context.Background(), []TrafficWorker{{PID: worker.PID}}); err == nil {
		t.Fatal("incomplete identity was treated as retired")
	}
}

func TestTrafficRecoveryAdmissionAndOperatorEditsRemainPending(t *testing.T) {
	env, site, _, _ := trafficFixture(t)
	if err := trafficRecoveryReady(env); err != nil {
		t.Fatal(err)
	}
	if err := trafficEnsurePrivateDirectory(env.stateDir, false); err != nil {
		t.Fatal(err)
	}
	after := trafficState{Operation: "abcdef0123456789", Sites: []TrafficSite{site}, InheritedLogs: []string{"access_log /tmp/op;"}}
	changes, err := planTrafficChanges(env, trafficState{}, after)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTrafficJSON(env, "intent.json", trafficIntent{Version: 1, Operation: after.Operation, Changes: changes}); err != nil {
		t.Fatal(err)
	}
	if err := trafficRecoveryReady(env); err == nil {
		t.Fatal("qualification ignored pending publication")
	}
	replacement := []byte("server { server_name operator.test; return 418; }\n")
	if err := os.WriteFile(changes[0].Path, replacement, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := applyTraffic(context.Background(), env, nil); err == nil {
		t.Fatal("recovery overwrote an unrelated operator replacement")
	}
	current, _ := os.ReadFile(changes[0].Path)
	if !bytes.Equal(current, replacement) {
		t.Fatal("operator replacement was lost")
	}
	if _, err := os.Lstat(filepath.Join(env.stateDir, "intent.json")); err != nil {
		t.Fatal("recovery evidence was lost")
	}
}

func TestTrafficDisableCleansAlreadyDisabledVhost(t *testing.T) {
	env, site, _, _ := trafficFixture(t)
	if _, err := applyTraffic(context.Background(), env, []TrafficSite{site}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(env.enabled, site.ConfigName)); err != nil {
		t.Fatal(err)
	}
	if _, err := applyTraffic(context.Background(), env, nil); err != nil {
		t.Fatal(err)
	}
	current, _ := os.ReadFile(filepath.Join(env.available, site.ConfigName))
	if bytes.Contains(current, []byte(trafficMarker)) {
		t.Fatal("disabled vhost retained producer")
	}
}

func TestTrafficInheritedOffDoesNotEnableOperationalLogs(t *testing.T) {
	_, site, _, _ := trafficFixture(t)
	raw := "server { server_name redirect.test; return 301 https://site.test$request_uri; }\n"
	active, err := injectTrafficConfig(raw, site, []string{"access_log /var/log/nginx/disabled-operational.log;", "access_log off;"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(active, "disabled-operational") || strings.Contains(active, "access_log off;") {
		t.Fatal("adding analytics changed disabled operational logging")
	}
	if !strings.Contains(active, site.LogPath) {
		t.Fatal("inherited operational OFF incorrectly disabled independent analytics")
	}
	restored, err := stripTrafficConfig(active)
	if err != nil || restored != raw {
		t.Fatal("OFF did not restore original inheritance")
	}
}

func TestTrafficShadowsOnlyExactlyOffAndRestoresBytes(t *testing.T) {
	_, site, _, _ := trafficFixture(t)
	for _, raw := range []string{
		"server {server_name site.test;access_log off;location / {return 204;}}",
		"server { server_name site.test; access_log /tmp/operational; location / {access_log\n # retain comment\n off ; return 204;} }\n",
		"server { server_name site.test; access_log off; location / {access_log off; return 204;} }\n",
	} {
		active, err := injectTrafficConfig(raw, site, nil)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(active, "access_log off;") {
			t.Fatal("operational off canceled independent analytics")
		}
		restored, err := stripTrafficConfig(active)
		if err != nil || restored != raw {
			t.Fatalf("off shadow was not exactly reversible: %v\n%s", err, restored)
		}
		repeated, err := injectTrafficConfig(active, site, nil)
		if err != nil || repeated != active {
			t.Fatal("reapplying duplicated or changed shadow markers")
		}
	}
	for _, original := range []string{"root /different;", "access_log /tmp/private;", "access_log off; root /different;"} {
		raw := "server {\n" + trafficSavedOffMarker + base64.RawStdEncoding.EncodeToString([]byte(original)) + "\n}"
		if _, err := stripTrafficConfig(raw); err == nil {
			t.Fatalf("restored arbitrary content from saved off marker: %s", original)
		}
	}
}
