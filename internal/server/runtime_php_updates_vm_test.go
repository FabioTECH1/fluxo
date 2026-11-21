package server

import (
	"context"
	"fluxo/internal/database"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fluxo/internal/services/packages"
)

func TestVMDeferredRuntimeMutationRetainsLifecycleLease(t *testing.T) {
	hostname, _ := os.Hostname()
	if os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || hostname != "fluxo-php-patch-test" || os.Geteuid() != 0 {
		t.Skip("disposable root acceptance VM only")
	}
	s := &Server{migrateLegacyCredentials: true}
	finish := make(chan struct{})
	started := make(chan struct{})
	s.runRuntimeMutationAsync(func() { close(started); <-finish })
	defer func() {
		select {
		case <-finish:
		default:
			close(finish)
		}
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	unlock, err := packages.Lifecycle(ctx, false)
	cancel()
	if err == nil {
		unlock()
		t.Fatal("maintenance overtook an accepted deferred mutation")
	}
	close(finish)
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	unlock, err = packages.Lifecycle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}

// This exercises the real kernel lease and actual middleware, not a mocked lock
// or Scheduler receipt. It does not create application jobs or run services.
func TestVMRuntimeFeatureGateReopensAfterLeaseRelease(t *testing.T) {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("guarded reusable VM only")
	}
	old := database.DB
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer func() { database.DB.Close(); database.DB = old }()
	if pending, err := database.PHPMaintenancePending(); err != nil || pending {
		t.Fatalf("fixture has existing maintenance; preserve it: %v %v", pending, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	release, err := packages.Lifecycle(ctx, false)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { release() }()
	called := 0
	s := &Server{migrateLegacyCredentials: true}
	h := s.phpMaintenanceGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++; w.WriteHeader(204) }))
	for _, feature := range []string{"scheduler", "nightwatch", "queue-worker", "horizon", "octane", "maintenance"} {
		r := httptest.NewRequest("POST", "/api/v1/sites/1/features/"+feature+"/enable", nil)
		wait, cancel := context.WithTimeout(r.Context(), 80*time.Millisecond)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r.WithContext(wait))
		cancel()
		if w.Code != 409 || called != 0 {
			t.Fatalf("%s bypassed actual exclusive lease: %d %s", feature, w.Code, w.Body.String())
		}
	}
	release()
	release = func() {}
	for _, feature := range []string{"scheduler", "nightwatch", "queue-worker", "horizon", "octane", "maintenance"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/sites/1/features/"+feature+"/enable", nil))
		if w.Code != 204 {
			t.Fatalf("%s blocked after lock release: %d %s", feature, w.Code, w.Body.String())
		}
	}
	if called != 6 {
		t.Fatal("not every feature admitted", called)
	}
}
