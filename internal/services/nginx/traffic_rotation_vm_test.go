package nginx

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This requires the dedicated acceptance VM because it intentionally exercises
// a real root master with a different, unprivileged Nginx worker identity.
func TestTrafficPrivateSourceRotationRequiresReloadVM(t *testing.T) {
	hostname, _ := os.Hostname()
	if os.Geteuid() != 0 || hostname != "fluxo-php-patch-test" || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("requires root, fluxo-php-patch-test and FLUXO_PHP_UPDATE_VM_TEST=1")
	}
	binary := trafficNativeBinary(t)
	worker, err := user.Lookup("www-data")
	if err != nil {
		t.Fatal(err)
	}
	workerUID, err := strconv.Atoi(worker.Uid)
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp("", "fluxo-rotation-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	if err := os.Chmod(base, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"available", "enabled", "conf.d", "sources"} {
		if err := os.Mkdir(filepath.Join(base, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	port := freeTCPPorts(t, 1)[0]
	site := TrafficSite{ID: 9, ConfigName: "rotation.test", Domain: "rotation.test", Generation: "abcdef0123456789", LogPath: filepath.Join(base, "sources", "source.jsonl")}
	vhost := fmt.Sprintf("server { listen 127.0.0.1:%d; server_name rotation.test; access_log off; location / { return 200 'ok'; } }\n", port)
	if err := os.WriteFile(filepath.Join(base, "available", site.ConfigName), []byte(vhost), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "available", site.ConfigName), filepath.Join(base, "enabled", site.ConfigName)); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(base, "nginx.conf")
	pidPath := filepath.Join(base, "nginx.pid")
	main := fmt.Sprintf("user www-data; worker_processes 1; pid %s; error_log %s notice; events {} http { access_log off; include %s/*.conf; include %s/*; }\n", pidPath, filepath.Join(base, "error.log"), filepath.Join(base, "conf.d"), filepath.Join(base, "enabled"))
	if err := os.WriteFile(config, []byte(main), 0644); err != nil {
		t.Fatal(err)
	}
	run := func(ctx context.Context, args ...string) (string, error) {
		all := append([]string{"-p", base + "/", "-c", config}, args...)
		raw, err := exec.CommandContext(ctx, binary, all...).CombinedOutput()
		if err != nil {
			return string(raw), fmt.Errorf("isolated Nginx %v: %w: %s", args, err, raw)
		}
		return string(raw), nil
	}
	if _, err := run(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := run(ctx, "-s", "quit"); err != nil {
			t.Log(err)
		}
	})
	waitForTCP(t, port)
	env := trafficEnvironment{available: filepath.Join(base, "available"), enabled: filepath.Join(base, "enabled"), httpPath: filepath.Join(base, "conf.d", "fluxo-traffic.conf"), stateDir: filepath.Join(base, "state"), strict: false,
		validate: func(ctx context.Context) error { _, err := run(ctx, "-t"); return err }, reload: func(ctx context.Context) error { _, err := run(ctx, "-s", "reload"); return err }, dump: func(ctx context.Context) (string, error) { return run(ctx, "-T") }, probe: probeTrafficActivation, drained: TrafficWorkersDrained, awaitShutdown: awaitTrafficWorkerShutdown,
		processes: func(context.Context) (trafficProcesses, error) { return trafficNativeProcesses(pidPath) }}
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	request := func(path string) {
		t.Helper()
		request, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil)
		request.Host = site.Domain
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("request returned %d", response.StatusCode)
		}
	}
	if _, err := applyTraffic(context.Background(), env, []TrafficSite{site}); err != nil {
		t.Fatal(err)
	}
	request("/before-rotation")
	readTrafficNativeRecords(t, site.LogPath, 1)
	rotated := site.LogPath + ".1"
	if err := os.Rename(site.LogPath, rotated); err != nil {
		t.Fatal(err)
	}
	if err := prepareTrafficLog(site.LogPath, false); err != nil {
		t.Fatal(err)
	}
	if _, err := run(context.Background(), "-s", "reopen"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		info, err := os.Stat(site.LogPath)
		if err != nil {
			t.Fatal(err)
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Uid == uint32(workerUID) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Nginx master did not acknowledge USR1 by reopening/chowning current file")
		}
		time.Sleep(10 * time.Millisecond)
	}
	request("/after-usr1")
	old := readTrafficNativeRecords(t, rotated, 2)
	if old[1]["path"] != "/after-usr1" {
		t.Fatalf("unexpected old-file record: %+v", old)
	}
	current, err := os.ReadFile(site.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 0 {
		t.Fatal("expected root-private worker traversal to prevent USR1-only rotation")
	}
	if retired, err := trafficLogRetired(context.Background(), rotated, false, binary); err != nil || retired {
		t.Fatalf("USR1 falsely retired old producer inode: %v %v", retired, err)
	}
	// A validated same-generation reload opens the current file in the root
	// master, then new workers inherit that FD without traversing private dirs.
	if _, err := applyTraffic(context.Background(), env, []TrafficSite{site}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		retired, err := trafficLogRetired(context.Background(), rotated, false, binary)
		if err != nil {
			t.Fatal(err)
		}
		if retired {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("old source remained open after validated reload")
		}
		time.Sleep(10 * time.Millisecond)
	}
	request("/after-reload")
	records := readTrafficNativeRecords(t, site.LogPath, 1)
	if records[0]["path"] != "/after-reload" {
		t.Fatalf("reload did not move production to current file: %+v", records)
	}
	if records := readTrafficNativeRecords(t, rotated, 2); len(records) != 2 {
		t.Fatal("retired source continued growing")
	}
}

func trafficNativeProcesses(pidPath string) (trafficProcesses, error) {
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
}
