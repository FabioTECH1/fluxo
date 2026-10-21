package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/syscmd"
)

func TestVMOctaneEnablePreflightAndFailedReadinessKeepRouting(t *testing.T) {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("explicit isolated Ubuntu VM only")
	}
	const app = "/home/fluxo/php-recovery-acceptance"
	writeFixture := func(path, data string) {
		old, err := os.ReadFile(path)
		existed := err == nil
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if existed {
				_ = os.WriteFile(path, old, 0644)
			} else {
				_ = os.Remove(path)
			}
		})
		if err = os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		credential, err := syscmd.ResolveCredential("fluxo")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.Chown(path, int(credential.Uid), int(credential.Gid)); err != nil {
			t.Fatal(err)
		}
	}
	cache := filepath.Join(app, "bootstrap/cache/config.php")
	writeFixture(cache, "<?php return [];")
	if err := os.Remove(cache); err != nil {
		t.Fatal(err)
	}
	writeFixture(filepath.Join(app, ".rr.yaml"), "")
	writeFixture(filepath.Join(app, "config/octane.php"), "<?php return ['server'=>'swoole','swoole'=>['command'=>'unsupported-custom-launcher']];")
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer database.DB.Close()
	const id = 9108000
	if _, err := database.DB.Exec("INSERT INTO sites(id,domain,path,php_version,app_type,deployment_strategy,app_port) VALUES (?,'octane-enablement-acceptance.test',?,'8.4','laravel','standard',18890)", id, app); err != nil {
		t.Fatal(err)
	}
	// Reserve the disposable high ID range so service generation cannot touch a
	// pre-existing unit belonging to the VM's original application fixtures.
	if _, err := database.DB.Exec("INSERT INTO daemons(id,site_id,name,command,directory) VALUES (?,?,'sequence reservation','unused',?)", id, id, app); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("DELETE FROM daemons WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	unit := filepath.Join("/etc/systemd/system", "fluxo-daemon-9108001.service")
	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatal("enablement acceptance unit already exists")
	}
	nginxPath := "/etc/nginx/sites-available/octane-enablement-acceptance.test"
	if _, err := os.Stat(nginxPath); !os.IsNotExist(err) {
		t.Fatal("enablement acceptance Nginx config already exists")
	}
	s := NewServer(nil, t.TempDir(), false)
	invoke := func(ctx context.Context) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/sites/"+strconv.Itoa(id)+"/features/octane/enable", nil)
		r = r.WithContext(ctx)
		r.SetPathValue("id", strconv.Itoa(id))
		w := httptest.NewRecorder()
		s.handleEnableOctane()(w, r)
		return w
	}
	expectedPort := 18890
	assertPreserved := func(t *testing.T) {
		t.Helper()
		var port, count int
		if err := database.DB.QueryRow("SELECT app_port FROM sites WHERE id=?", id).Scan(&port); err != nil || port != expectedPort {
			t.Fatalf("enablement failure changed previous port: %d %v", port, err)
		}
		if err := database.DB.QueryRow("SELECT count(*) FROM daemons").Scan(&count); err != nil || count != 0 {
			t.Fatalf("failed enablement retained a daemon: %d %v", count, err)
		}
		if _, err := os.Stat(nginxPath); !os.IsNotExist(err) {
			t.Fatal("failed enablement changed Nginx routing")
		}
		if _, err := os.Stat(unit); !os.IsNotExist(err) {
			t.Fatal("failed enablement left its service definition")
		}
	}
	w := invoke(context.Background())
	if w.Code != 422 || !strings.Contains(w.Body.String(), "compatibility preflight failed") {
		t.Fatalf("unsupported launcher was admitted: %d %s", w.Code, w.Body.String())
	}
	assertPreserved(t)
	if err := os.WriteFile(filepath.Join(app, "config/octane.php"), []byte("<?php return ['server'=>'roadrunner'];"), 0644); err != nil {
		t.Fatal(err)
	}
	writeFixture(filepath.Join(app, "routes/web.php"), "<?php \\Illuminate\\Support\\Facades\\Route::get('/',fn()=>response('Deliberate readiness failure',500));")
	w = invoke(context.Background())
	if w.Code != 422 || !strings.Contains(w.Body.String(), "routing was not changed") {
		t.Fatalf("unhealthy startup was reported successful: %d %s", w.Code, w.Body.String())
	}
	assertPreserved(t)

	t.Run("cancelled request cleans up the generated service", func(t *testing.T) {
		unit = "/etc/systemd/system/fluxo-daemon-9108002.service"
		if _, err := os.Lstat(unit); !os.IsNotExist(err) {
			t.Fatal("cancellation acceptance unit already exists")
		}
		link := "/etc/systemd/system/multi-user.target.wants/fluxo-daemon-9108002.service"
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			t.Fatal("cancellation acceptance enable link already exists")
		}
		// Force reservation of a new port, so cleanup must restore zero rather
		// than merely leave an unchanged, pre-existing port behind.
		if _, err := database.DB.Exec("UPDATE sites SET app_port=0 WHERE id=?", id); err != nil {
			t.Fatal(err)
		}
		expectedPort = 0
		started := filepath.Join(app, "storage/fluxo-cancel-acceptance.started")
		finished := filepath.Join(app, "storage/fluxo-cancel-acceptance.finished")
		for _, marker := range []string{started, finished} {
			if _, err := os.Lstat(marker); !os.IsNotExist(err) {
				t.Fatalf("acceptance marker already exists: %s", marker)
			}
		}
		t.Cleanup(func() { _ = os.Remove(started); _ = os.Remove(finished) })
		if err := os.WriteFile(filepath.Join(app, "routes/web.php"), []byte("<?php \\Illuminate\\Support\\Facades\\Route::get('/',fn()=>response('Deliberate readiness failure',500)); \\Illuminate\\Support\\Facades\\Route::get('/fluxo-cancel-acceptance',function(){ file_put_contents('"+started+"','started'); sleep(8); file_put_contents('"+finished+"','finished'); return response('completed',200); });"), 0644); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		observed := make(chan bool, 1)
		stopWatching := make(chan struct{})
		requestDone := make(chan error, 1)
		go func() {
			client := &http.Client{Timeout: 20 * time.Second}
			for {
				select {
				case <-ctx.Done():
					requestDone <- ctx.Err()
					return
				case <-stopWatching:
					requestDone <- fmt.Errorf("enablement ended before request")
					return
				default:
				}
				var port int
				if err := database.DB.QueryRow("SELECT app_port FROM sites WHERE id=?", id).Scan(&port); err == nil && port > 0 {
					response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/fluxo-cancel-acceptance", port))
					if err == nil {
						body, readErr := io.ReadAll(response.Body)
						response.Body.Close()
						if readErr != nil {
							requestDone <- readErr
						} else if response.StatusCode != 200 || string(body) != "completed" {
							requestDone <- fmt.Errorf("long request: %d %q", response.StatusCode, body)
						} else {
							requestDone <- nil
						}
						return
					}
				}
				time.Sleep(25 * time.Millisecond)
			}
		}()
		go func() {
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for {
				if _, err := os.Stat(started); err == nil {
					cancel()
					observed <- true
					return
				}
				select {
				case <-ticker.C:
				case <-ctx.Done():
					observed <- false
					return
				case <-stopWatching:
					observed <- false
					return
				}
			}
		}()
		w := invoke(ctx)
		close(stopWatching)
		requestErr := <-requestDone
		if !<-observed {
			t.Fatalf("request ended before cancellation during the live request: %d %s", w.Code, w.Body.String())
		}
		if ctx.Err() != context.Canceled || (w.Code != 422 && w.Code != 500) || strings.Contains(w.Body.String(), "failed to clean up") {
			t.Fatalf("cancelled enablement did not clean up independently: %v, %d %s", ctx.Err(), w.Code, w.Body.String())
		}
		if requestErr != nil {
			t.Fatalf("cleanup interrupted the in-flight request: %v", requestErr)
		}
		if _, err := os.Stat(finished); err != nil {
			t.Fatalf("in-flight request did not finish naturally: %v", err)
		}
		assertPreserved(t)
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			t.Fatal("cancelled enablement retained its systemd enable link")
		}
		state, err := syscmd.Run(context.Background(), 5*time.Second, "systemctl", "show", filepath.Base(unit), "--property=LoadState,ActiveState,MainPID")
		if err != nil || !strings.Contains(state, "LoadState=not-found") || !strings.Contains(state, "MainPID=0") || strings.Contains(state, "ActiveState=active") {
			t.Fatalf("cancelled service is still loaded or running: %s %v", state, err)
		}
	})

	t.Run("cleanup failure retains failed record and reserved port", func(t *testing.T) {
		const daemonID = 9108099
		path := "/etc/systemd/system/fluxo-daemon-9108099.service"
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("cleanup-failure acceptance path already exists")
		}
		// A nonempty disposable directory makes removal fail without changing
		// system-wide permissions or interfering with any real service.
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
		blocker := filepath.Join(path, "acceptance-blocker")
		t.Cleanup(func() {
			_ = os.Remove(blocker)
			_ = os.Remove(path)
			var count int
			if err := database.DB.QueryRow("SELECT COUNT(*) FROM daemons WHERE id=?", daemonID).Scan(&count); err != nil {
				t.Error(err)
			} else if count > 0 {
				if err := cleanupFailedOctane(id, daemonID, 0); err != nil {
					t.Errorf("restore disposable cleanup fixture: %v", err)
				}
			}
		})
		if err := os.WriteFile(blocker, []byte("acceptance fixture\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := database.DB.Exec("INSERT INTO daemons(id,site_id,name,managed_kind,status,command,directory) VALUES (?,?,'Laravel Octane','laravel_octane','enable_cleanup_pending','unused',?)", daemonID, id, app); err != nil {
			t.Fatal(err)
		}
		if _, err := database.DB.Exec("UPDATE sites SET app_port=18891 WHERE id=?", id); err != nil {
			t.Fatal(err)
		}
		if err := cleanupFailedOctane(id, daemonID, 0); err == nil {
			t.Fatal("cleanup unexpectedly removed the deliberately blocked service path")
		}
		var status string
		var port int
		if err := database.DB.QueryRow("SELECT status FROM daemons WHERE id=?", daemonID).Scan(&status); err != nil || status != "enable_cleanup_pending" {
			t.Fatalf("cleanup failure did not retain failed daemon: %q %v", status, err)
		}
		if err := database.DB.QueryRow("SELECT app_port FROM sites WHERE id=?", id).Scan(&port); err != nil || port != 18891 {
			t.Fatalf("cleanup failure released the reserved port: %d %v", port, err)
		}
		if isOctaneEnabled(id) {
			t.Fatal("pending enablement is advertised as enabled")
		}
		managed, err := renderManagedNginxForSite(id)
		if err != nil || strings.Contains(managed.Content, "proxy_pass http://127.0.0.1:18891") {
			t.Fatalf("pending enablement changed generated routing: %v %s", err, managed.Content)
		}
		w := invoke(context.Background())
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "cleanup remains pending") {
			t.Fatalf("failed cleanup retry bypassed preflight: %d %s", w.Code, w.Body.String())
		}
		if _, err := os.Stat(nginxPath); !os.IsNotExist(err) {
			t.Fatal("cleanup failure changed Nginx routing")
		}
		// Once protected cleanup can succeed, retry must run a fresh preflight
		// instead of treating the retained record as an enabled runtime.
		if err := os.Remove(blocker); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(app, "config/octane.php"), []byte("<?php return ['server'=>'swoole','swoole'=>['command'=>'unsupported-custom-launcher']];"), 0644); err != nil {
			t.Fatal(err)
		}
		w = invoke(context.Background())
		if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "compatibility preflight failed") {
			t.Fatalf("retry did not re-run compatibility preflight after cleanup: %d %s", w.Code, w.Body.String())
		}
		var count int
		if err := database.DB.QueryRow("SELECT COUNT(*) FROM daemons WHERE site_id=?", id).Scan(&count); err != nil || count != 0 {
			t.Fatalf("retry retained or duplicated pending record: %d %v", count, err)
		}
	})
}

// Enablement state is distinct from runtime health: an already enabled daemon
// that later fails must retain routing, while an unfinished enable must not.
func TestOctanePendingEnablementDoesNotAdvertiseOrAllowActions(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer database.DB.Close()
	if _, err := database.DB.Exec("INSERT INTO sites(id,domain,path,php_version,app_type,deployment_strategy,app_port) VALUES (1,'pending-octane.test',?,'8.4','laravel','standard',18891)", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("INSERT INTO daemons(id,site_id,name,managed_kind,command,directory,status) VALUES (1,1,'Laravel Octane','laravel_octane','unused','/unused','enable_pending')"); err != nil {
		t.Fatal(err)
	}
	s := NewServer(nil, t.TempDir(), false)
	for _, status := range []string{"enable_pending", "enable_publish_pending", "enable_fallback_pending", "enable_cleanup_pending", "active", "failed", "stopped"} {
		t.Run(status, func(t *testing.T) {
			if _, err := database.DB.Exec("UPDATE daemons SET status=? WHERE id=1", status); err != nil {
				t.Fatal(err)
			}
			pending := status == "enable_pending" || status == "enable_publish_pending" || status == "enable_fallback_pending" || status == "enable_cleanup_pending"
			if isOctaneEnabled(1) == pending {
				t.Fatalf("incorrect enabled state for %s", status)
			}
			if pending {
				for _, handler := range []http.HandlerFunc{s.handleUpdateSite(), s.handleDeleteSite()} {
					r := httptest.NewRequest(http.MethodPut, "/api/v1/sites/1", strings.NewReader(`{}`))
					r.SetPathValue("id", "1")
					w := httptest.NewRecorder()
					handler(w, r)
					if w.Code != http.StatusConflict {
						t.Fatalf("pending site mutation admitted: %d %s", w.Code, w.Body.String())
					}
				}
				for _, handler := range []http.HandlerFunc{s.handleStartDaemon(), s.handleStopDaemon(), s.handleRestartDaemon()} {
					r := httptest.NewRequest(http.MethodPost, "/api/v1/daemons/1/action", nil)
					r.SetPathValue("daemon_id", "1")
					w := httptest.NewRecorder()
					handler(w, r)
					if w.Code != http.StatusConflict {
						t.Fatalf("pending runtime action admitted: %d %s", w.Code, w.Body.String())
					}
				}
			}
		})
	}
}
