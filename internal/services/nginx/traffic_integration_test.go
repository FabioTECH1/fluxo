package nginx

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func trafficNativeBinary(t *testing.T) string {
	t.Helper()
	binary := os.Getenv("FLUXO_NGINX_TEST_BINARY")
	if binary == "" {
		var err error
		binary, err = exec.LookPath("nginx")
		if err != nil {
			t.Skip("nginx is not installed (set FLUXO_NGINX_TEST_BINARY for isolated native tests)")
		}
	}
	return binary
}

func TestTrafficNativePrivacyActivationAndDelayedWorkerRetirement(t *testing.T) {
	binary := trafficNativeBinary(t)
	// Keep the Unix control path below Linux's sockaddr_un length limit.
	base, err := os.MkdirTemp("", "fluxo-traffic-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	for _, name := range []string{"available", "enabled", "conf.d", "logs", "public"} {
		if err := os.Mkdir(filepath.Join(base, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	public := filepath.Join(base, "public")
	if err := os.WriteFile(filepath.Join(public, "index.html"), []byte("traffic test"), 0644); err != nil {
		t.Fatal(err)
	}
	held, release := make(chan struct{}), make(chan struct{})
	var heldOnce, releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		heldOnce.Do(func() { close(held) })
		<-release
		w.Write([]byte("held response"))
	}))
	defer upstream.Close()
	port := freeTCPPorts(t, 1)[0]
	site := TrafficSite{ID: 8, ConfigName: "original-name.test", Domain: "site.test", Generation: "0123456789abcdef", LogPath: filepath.Join(base, "logs", "source.jsonl")}
	vhost := fmt.Sprintf("server { listen 127.0.0.1:%d; server_name site.test alias.test; root %s; access_log %s; location / { try_files $uri /index.html; } location = /hold { proxy_pass %s; } location = /favicon.ico { access_log off; } }\n", port, public, filepath.Join(base, "logs", "operational.jsonl"), upstream.URL)
	available, enabled := filepath.Join(base, "available"), filepath.Join(base, "enabled")
	if err := os.WriteFile(filepath.Join(available, site.ConfigName), []byte(vhost), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(available, site.ConfigName), filepath.Join(enabled, site.ConfigName)); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(base, "nginx.conf")
	pidPath := filepath.Join(base, "nginx.pid")
	workerUser := ""
	if os.Geteuid() == 0 {
		workerUser = "user root; "
	}
	main := workerUser + fmt.Sprintf("pid %s; error_log %s notice; worker_processes 1; events {} http { access_log %s; include %s/*.conf; include %s/*; }\n", pidPath, filepath.Join(base, "error.log"), filepath.Join(base, "logs", "inherited.log"), filepath.Join(base, "conf.d"), enabled)
	if err := os.WriteFile(conf, []byte(main), 0644); err != nil {
		t.Fatal(err)
	}
	run := func(ctx context.Context, args ...string) (string, error) {
		all := append([]string{"-p", base + "/", "-c", conf}, args...)
		raw, err := exec.CommandContext(ctx, binary, all...).CombinedOutput()
		if err != nil {
			return string(raw), fmt.Errorf("nginx %v: %w: %s", args, err, raw)
		}
		return string(raw), nil
	}
	if _, err := run(context.Background(), "-t"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := run(ctx, "-s", "quit"); err != nil {
			t.Log(err)
		}
	})
	waitForTCP(t, port)
	env := trafficEnvironment{available: available, enabled: enabled, httpPath: filepath.Join(base, "conf.d", "fluxo-traffic.conf"), stateDir: filepath.Join(base, "state"), strict: false,
		validate: func(ctx context.Context) error { _, err := run(ctx, "-t"); return err }, reload: func(ctx context.Context) error { _, err := run(ctx, "-s", "reload"); return err }, dump: func(ctx context.Context) (string, error) { return run(ctx, "-T") }, probe: probeTrafficActivation, drained: TrafficWorkersDrained, awaitShutdown: awaitTrafficWorkerShutdown,
		processes: func(context.Context) (trafficProcesses, error) {
			raw, err := os.ReadFile(pidPath)
			if err != nil {
				return trafficProcesses{}, err
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil {
				return trafficProcesses{}, err
			}
			boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
			if err != nil {
				return trafficProcesses{}, err
			}
			bootID := strings.TrimSpace(string(boot))
			master, err := trafficProcessIdentity(pid, bootID)
			if err != nil {
				return trafficProcesses{}, err
			}
			snapshot := trafficProcesses{Master: master}
			children, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/children", pid, pid))
			if err != nil {
				return snapshot, err
			}
			for _, child := range strings.Fields(string(children)) {
				cmd, err := os.ReadFile("/proc/" + child + "/cmdline")
				if err != nil {
					continue
				}
				if !strings.HasPrefix(string(cmd), "nginx: worker process") {
					continue
				}
				id, _ := strconv.Atoi(child)
				worker, err := trafficProcessIdentity(id, bootID)
				if err == nil {
					snapshot.Workers = append(snapshot.Workers, worker)
				}
			}
			return snapshot, nil
		}}
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 10 * time.Second}
	defer client.CloseIdleConnections()
	request := func(path, referer, agent string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil)
		req.Host = "site.test"
		req.Header.Set("Accept", "text/html")
		req.Header.Set("Referer", referer)
		req.Header.Set("User-Agent", agent)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("request returned %d", response.StatusCode)
		}
	}
	request("/baseline?secret=virgin", "", "human-private-agent")
	if _, err := os.Lstat(site.LogPath); !os.IsNotExist(err) {
		t.Fatal("disabled baseline produced an analytics file")
	}
	if _, err := applyTraffic(context.Background(), env, []TrafficSite{site}); err != nil {
		t.Fatal(err)
	}
	request("/original-path?secret=never-store", "https://Ref.Example/private/path?secret=never-store", "human-private-agent")
	request("/bot?token=hidden", "https://192.0.2.8/private", "ExampleSpider private-agent")
	records := readTrafficNativeRecords(t, site.LogPath, 2)
	if records[0]["path"] != "/original-path" || records[0]["referrer"] != "Ref.Example" || records[0]["html"] != float64(1) || records[1]["bot"] != float64(1) || records[1]["referrer"] != "" {
		t.Fatalf("privacy/classification fields: %+v", records)
	}
	raw, _ := os.ReadFile(site.LogPath)
	for _, secret := range []string{"never-store", "hidden", "192.0.2.8", "private-agent", "?secret="} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("producer serialized private data %q: %s", secret, raw)
		}
	}
	// Assets in an ordinary access_log off location still contribute to
	// analytics, without enabling their operational log destination.
	operationalBefore, _ := os.ReadFile(filepath.Join(base, "logs", "operational.jsonl"))
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/favicon.ico", port), nil)
	req.Host = "site.test"
	assetResponse, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, assetResponse.Body)
	assetResponse.Body.Close()
	records = readTrafficNativeRecords(t, site.LogPath, 3)
	if records[2]["path"] != "/favicon.ico" {
		t.Fatalf("operationally quiet asset was omitted from analytics: %+v", records)
	}
	operationalAfter, _ := os.ReadFile(filepath.Join(base, "logs", "operational.jsonl"))
	if string(operationalBefore) != string(operationalAfter) {
		t.Fatal("analytics enabled ordinary access logging for an off location")
	}
	completed := make(chan error, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/hold", port), nil)
		req.Host = "alias.test"
		response, err := client.Do(req)
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
		completed <- err
	}()
	select {
	case <-held:
	case <-time.After(5 * time.Second):
		t.Fatal("held request did not reach upstream")
	}
	result, err := applyTraffic(context.Background(), env, nil)
	if err != nil {
		t.Fatal(err)
	}
	drained, err := TrafficWorkersDrained(context.Background(), result.RetiringWorkers)
	if err != nil || drained {
		t.Fatalf("OFF ignored old request's producer: %+v %v %v", result, drained, err)
	}
	retired, err := trafficLogRetired(context.Background(), site.LogPath, false, binary)
	if err != nil || retired {
		t.Fatalf("in-flight worker source was called retired: %v %v", retired, err)
	}
	request("/after-off?secret=disabled", "", "human")
	if records := readTrafficNativeRecords(t, site.LogPath, 3); len(records) != 3 {
		t.Fatal("new workers logged after OFF")
	}
	releaseOnce.Do(func() { close(release) })
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	records = readTrafficNativeRecords(t, site.LogPath, 4)
	if records[3]["path"] != "/hold" {
		t.Fatalf("old worker's completion was not accounted for: %+v", records)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		drained, err = TrafficWorkersDrained(context.Background(), result.RetiringWorkers)
		if err != nil {
			t.Fatal(err)
		}
		if drained {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("old workers did not retire")
		}
		time.Sleep(20 * time.Millisecond)
	}
	retired, err = trafficLogRetired(context.Background(), site.LogPath, false, binary)
	if err != nil || !retired {
		t.Fatalf("drained source was not retired: %v %v", retired, err)
	}
	stable, _ := os.ReadFile(site.LogPath)
	request("/disabled-again", "", "human")
	time.Sleep(30 * time.Millisecond)
	after, _ := os.ReadFile(site.LogPath)
	if string(stable) != string(after) {
		t.Fatal("producer continued after confirmed OFF/drain")
	}
	restored, _ := os.ReadFile(filepath.Join(available, site.ConfigName))
	if string(restored) != vhost {
		t.Fatal("native transitions changed original vhost")
	}
	// Re-enable uses a new generation and produces only that generation in the
	// fresh source, preventing ingestion of requests from the disabled interval.
	site.Generation = "abcdef0123456789"
	site.LogPath = filepath.Join(base, "logs", "next.jsonl")
	if _, err := applyTraffic(context.Background(), env, []TrafficSite{site}); err != nil {
		t.Fatal(err)
	}
	request("/re-enabled", "", "human")
	next := readTrafficNativeRecords(t, site.LogPath, 1)
	if next[0]["generation"] != site.Generation {
		t.Fatalf("wrong activation generation: %+v", next)
	}
	// A real master stop/start must retain producer configuration and source
	// generation without asking the running daemon to regenerate its vhost.
	beforeRestart, err := env.processes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run(context.Background(), "-s", "quit"); err != nil {
		t.Fatal(err)
	}
	stopDeadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Lstat(pidPath); os.IsNotExist(err) {
			break
		}
		if time.Now().After(stopDeadline) {
			t.Fatal("isolated Nginx did not stop for restart test")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := run(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForTCP(t, port)
	request("/after-master-restart", "", "human")
	next = readTrafficNativeRecords(t, site.LogPath, 2)
	if next[1]["generation"] != site.Generation || next[1]["path"] != "/after-master-restart" {
		t.Fatalf("master restart lost durable producer state: %+v", next)
	}
	if done, err := TrafficWorkersDrained(context.Background(), beforeRestart.Workers); err != nil || !done {
		t.Fatalf("restarted master retained old worker identity: %v %v", done, err)
	}
	if _, err := applyTraffic(context.Background(), env, []TrafficSite{site}); err != nil {
		t.Fatalf("controller reconciliation after master restart: %v", err)
	}
}

func readTrafficNativeRecords(t *testing.T, path string, minimum int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var records []map[string]any
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			var record map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				file.Close()
				t.Fatalf("invalid producer JSON: %s: %v", scanner.Bytes(), err)
			}
			records = append(records, record)
		}
		err = scanner.Err()
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(records) >= minimum {
			return records
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d records, expected %d", len(records), minimum)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
