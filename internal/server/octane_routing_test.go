package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
)

func TestPendingOctanePublicationRestoresRoutingBeforeCleanup(t *testing.T) {
	oldDB := database.DB
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer func() { database.DB.Close(); database.DB = oldDB }()
	if _, err := database.DB.Exec("INSERT INTO sites(id,domain,path,php_version,app_type,app_port,deploy_script,deploy_script_mode) VALUES (1,'publication.test','/home/fluxo/publication.test','8.4','laravel',18891,?,'legacy')", octaneReloadLine); err != nil {
		t.Fatal(err)
	}
	oldSync := syncOctaneRoutingConfig
	oldReload := reloadExistingOctaneRouting
	oldRead, oldRender, oldProxy, oldInstall, oldCleanup := readOctaneRoutingConfig, renderRegeneratedSiteVhost, renderOctaneEnableVhost, installRegeneratedSiteVhost, cleanupOctaneRuntime
	defer func() {
		syncOctaneRoutingConfig = oldSync
		reloadExistingOctaneRouting = oldReload
		readOctaneRoutingConfig = oldRead
		renderRegeneratedSiteVhost = oldRender
		renderOctaneEnableVhost = oldProxy
		installRegeneratedSiteVhost = oldInstall
		cleanupOctaneRuntime = oldCleanup
	}()
	renderRegeneratedSiteVhost = func(int) (renderedSiteVhost, error) {
		return renderedSiteVhost{"publication.test", "FPM fallback"}, nil
	}
	renderOctaneEnableVhost = func(int) (renderedSiteVhost, error) {
		return renderedSiteVhost{"publication.test", "Octane proxy"}, nil
	}
	for _, initial := range []string{"before_publication", "after_publication", "fallback_on_disk", "interrupted_fallback", "fsync_failure", "changed_config"} {
		t.Run(initial, func(t *testing.T) {
			phase, actual := "enable_pending", "FPM fallback"
			if initial != "before_publication" {
				phase = "enable_publish_pending"
			}
			if initial == "changed_config" {
				actual = "administrator replacement"
			}
			if _, err := database.DB.Exec("INSERT INTO daemons(id,site_id,name,managed_kind,status,command,directory) VALUES (1,1,'Laravel Octane','laravel_octane',?,'unused','/home/fluxo/publication.test')", phase); err != nil {
				t.Fatal(err)
			}
			defer database.DB.Exec("DELETE FROM daemons WHERE id=1")
			database.DB.Exec("UPDATE sites SET app_port=18891,deploy_script=? WHERE id=1", octaneReloadLine)
			installs, cleanups, syncs := 0, 0, 0
			failSync := initial == "fsync_failure"
			syncOctaneRoutingConfig = func(name, content string, absent bool) error {
				if failSync {
					return errors.New("injected fsync failure")
				}
				if actual != content || absent || installs == 0 {
					return errors.New("durable confirmation preceded verified fallback")
				}
				syncs++
				return nil
			}
			readOctaneRoutingConfig = func(string) (string, error) { return actual, nil }
			failReload := initial == "interrupted_fallback"
			installRegeneratedSiteVhost = func(_ context.Context, _ string, content string) error {
				if content == "FPM fallback" {
					var state string
					if err := database.DB.QueryRow("SELECT status FROM daemons WHERE id=1").Scan(&state); err != nil {
						return err
					}
					if state != "enable_fallback_pending" {
						return errors.New("fallback reload preceded durable intent")
					}
				}
				actual = content
				installs++
				if failReload && content == "FPM fallback" {
					return errors.New("interrupted after fallback write before reload")
				}
				return nil
			}
			cleanupOctaneRuntime = func(_ context.Context, _ string, _ int, _ string, _ int) error {
				cleanups++
				if initial != "before_publication" {
					var script string
					if err := database.DB.QueryRow("SELECT deploy_script FROM sites WHERE id=1").Scan(&script); err != nil {
						return err
					}
					if actual != "FPM fallback" || installs == 0 || syncs == 0 || strings.Contains(script, octaneReloadLine) {
						return errors.New("backend cleanup preceded fallback and hook restoration")
					}
				}
				return nil
			}
			if initial == "after_publication" || initial == "interrupted_fallback" || initial == "fsync_failure" {
				// Production publication path succeeded, but its caller crashed before
				// replacing the durable enable_publish_pending status with active.
				if err := regenerateNginxForOctaneEnable(1); err != nil {
					t.Fatal(err)
				}
				if actual != "Octane proxy" {
					t.Fatal("proxy was not published")
				}
				installs = 0
			}
			err := cleanupFailedOctane(1, 1, 0)
			if initial == "changed_config" {
				if err == nil || cleanups != 0 || installs != 0 {
					t.Fatalf("changed routing was overwritten or backend stopped: %v %d %d", err, cleanups, installs)
				}
				return
			}
			if initial == "interrupted_fallback" || initial == "fsync_failure" {
				if err == nil || cleanups != 0 {
					t.Fatalf("backend removed before successful fallback reload: %v %d", err, cleanups)
				}
				var status string
				database.DB.QueryRow("SELECT status FROM daemons WHERE id=1").Scan(&status)
				if status != "enable_fallback_pending" {
					t.Fatalf("lost fallback intent: %s", status)
				}
				failReload = false
				failSync = false
				err = cleanupFailedOctane(1, 1, 0)
				if installs != 2 {
					t.Fatalf("retry did not reload the already written fallback: %d", installs)
				}
			}
			if err != nil || cleanups != 1 {
				t.Fatalf("cleanup did not finish: %v %d", err, cleanups)
			}
			if initial == "before_publication" && installs != 0 {
				t.Fatalf("pre-publication failure changed Nginx: %d", installs)
			}
			var count, port int
			database.DB.QueryRow("SELECT COUNT(*) FROM daemons").Scan(&count)
			database.DB.QueryRow("SELECT app_port FROM sites WHERE id=1").Scan(&port)
			if count != 0 || port != 0 {
				t.Fatalf("cleanup did not release record/port: %d %d", count, port)
			}
		})
	}
	// A missing file also requires validation/reload: live workers may still
	// retain a proxy after a lost reload reply and a subsequent unlink.
	readOctaneRoutingConfig = func(string) (string, error) { return "", os.ErrNotExist }
	installRegeneratedSiteVhost = func(context.Context, string, string) error {
		t.Fatal("absence restoration created a vhost")
		return nil
	}
	syncs := 0
	failAbsentSync := true
	syncOctaneRoutingConfig = func(_ string, _ string, absent bool) error {
		if !absent {
			t.Fatal("absence confirmation tried to persist a new config")
		}
		if failAbsentSync {
			return errors.New("injected directory fsync failure")
		}
		syncs++
		return nil
	}
	reloads := 0
	reloadExistingOctaneRouting = func(context.Context) error { reloads++; return errors.New("reload failed") }
	if err := restorePendingOctaneRouting(1, 1, "enable_publish_pending"); err == nil {
		t.Fatal("missing vhost allowed cleanup despite failed reload")
	}
	reloadExistingOctaneRouting = func(context.Context) error { reloads++; return nil }
	if err := restorePendingOctaneRouting(1, 1, "enable_publish_pending"); err == nil {
		t.Fatal("absence allowed cleanup before directory fsync")
	}
	failAbsentSync = false
	if err := restorePendingOctaneRouting(1, 1, "enable_publish_pending"); err != nil {
		t.Fatal(err)
	}
	if reloads != 3 || syncs != 1 {
		t.Fatalf("absence did not retry live routing confirmation: %d", reloads)
	}

}

func TestSiteEditAndDeleteWaitForOctaneMutationGate(t *testing.T) {
	oldDB := database.DB
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer func() { database.DB.Close(); database.DB = oldDB }()
	database.DB.Exec("INSERT INTO sites(id,domain,path) VALUES (1,'gate.test','/home/fluxo/gate.test')")
	database.DB.Exec("INSERT INTO daemons(site_id,name,managed_kind,status,command,directory) VALUES (1,'Laravel Octane','laravel_octane','enable_pending','unused','/unused')")
	s := NewServer(nil, t.TempDir(), false)
	for _, handler := range []http.HandlerFunc{s.handleUpdateSite(), s.handleDeleteSite()} {
		octaneMutationMu.Lock()
		r := httptest.NewRequest(http.MethodPut, "/api/v1/sites/1", strings.NewReader(`{}`))
		r.SetPathValue("id", "1")
		w := httptest.NewRecorder()
		done := make(chan struct{})
		go func() { handler(w, r); close(done) }()
		select {
		case <-done:
			octaneMutationMu.Unlock()
			t.Fatal("site mutation bypassed enablement gate")
		case <-time.After(20 * time.Millisecond):
		}
		octaneMutationMu.Unlock()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("site mutation did not resume")
		}
		if w.Code != http.StatusConflict {
			t.Fatalf("pending state changed after gate release: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestSiteEditRetainsOctaneGateThroughDeferredRuntime(t *testing.T) {
	oldDB := database.DB
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer func() { database.DB.Close(); database.DB = oldDB }()
	if _, err := database.DB.Exec("INSERT INTO sites(id,domain,path,app_type,deployment_strategy,repository,branch,node_mode) VALUES (1,'deferred.test','/home/fluxo/deferred.test','php','standard','','main','server')"); err != nil {
		t.Fatal(err)
	}
	oldRender, oldInstall := renderRegeneratedSiteVhost, installRegeneratedSiteVhost
	defer func() { renderRegeneratedSiteVhost = oldRender; installRegeneratedSiteVhost = oldInstall }()
	entered, release := make(chan struct{}), make(chan struct{})
	renderRegeneratedSiteVhost = func(int) (renderedSiteVhost, error) { return renderedSiteVhost{"deferred.test", "fixture"}, nil }
	installRegeneratedSiteVhost = func(context.Context, string, string) error { close(entered); <-release; return nil }
	s := NewServer(nil, t.TempDir(), false)
	r := httptest.NewRequest(http.MethodPut, "/api/v1/sites/1", strings.NewReader(`{"web_root":"/public2"}`))
	r.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	s.handleUpdateSite()(w, r)
	if w.Code != http.StatusOK {
		close(release)
		t.Fatalf("update failed: %d %s", w.Code, w.Body.String())
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("runtime job was not handed off")
	}
	acquired := make(chan struct{})
	go func() { octaneMutationMu.Lock(); octaneMutationMu.Unlock(); close(acquired) }()
	select {
	case <-acquired:
		close(release)
		t.Fatal("enablement could overtake deferred site reconciliation")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("deferred reconciliation did not release gate")
	}
}
