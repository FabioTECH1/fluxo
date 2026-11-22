package nginxupdate

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	mu    sync.Mutex
	value Saved
	fail  bool
}

func (s *memoryStore) Load() (Saved, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.value, nil }
func (s *memoryStore) Save(v Saved) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("storage failed")
	}
	s.value = v
	return nil
}
func setupManager(t *testing.T) (*Manager, *memoryStore, *int) {
	t.Helper()
	store := &memoryStore{}
	m := New(store)
	count := 0
	m.Now = func() time.Time { return time.Unix(100000, 0) }
	m.Admit = func(context.Context) (func(), error) { return func() {}, nil }
	m.Refresh = func(context.Context) error { count++; return nil }
	m.Inspect = func(context.Context) (Snapshot, error) {
		return Snapshot{State: "update_available", Package: "nginx-core", InstalledVersion: "1.24.0-2ubuntu7.15", CandidateVersion: "1.24.0-2ubuntu7.21", Identity: "candidate-a"}, nil
	}
	return m, store, &count
}
func TestNginxDiscoveryDoesNotRefreshOnRead(t *testing.T) {
	m, _, count := setupManager(t)
	for i := 0; i < 4; i++ {
		v, e := m.Status()
		if e != nil || !v.Stale || v.Notice {
			t.Fatal(v, e)
		}
	}
	if *count != 0 {
		t.Fatal("GET refreshed APT")
	}
}
func TestNginxDiscoveryDismissalIdentityAndExternalUpgrade(t *testing.T) {
	m, store, _ := setupManager(t)
	v, e := m.Check(context.Background())
	if e != nil || !v.Notice || v.Stale || v.RunningAdoption != "not_verified" {
		t.Fatal(v, e)
	}
	v, e = m.Dismiss("candidate-a")
	if e != nil || v.Notice {
		t.Fatal(v, e)
	}
	restarted := New(store)
	restarted.Now = m.Now
	v, _ = restarted.Status()
	if v.Notice {
		t.Fatal("lost dismissal on reopen")
	}
	v, _ = m.Check(context.Background())
	if v.Notice {
		t.Fatal("same result reappeared")
	}
	m.Inspect = func(context.Context) (Snapshot, error) {
		return Snapshot{State: "update_available", Identity: "new-version-or-source"}, nil
	}
	v, _ = m.Check(context.Background())
	if !v.Notice {
		t.Fatal("new candidate hidden")
	}
	if _, e = m.Dismiss("candidate-a"); !errors.Is(e, ErrChanged) {
		t.Fatal(e)
	}
	m.Inspect = func(context.Context) (Snapshot, error) {
		return Snapshot{State: "current", InstalledVersion: "1.24.0-2ubuntu7.21", Identity: "installed-new"}, nil
	}
	v, _ = m.Check(context.Background())
	if v.Notice || v.Snapshot.State != "current" {
		t.Fatal(v)
	}
}
func TestNginxDiscoveryFailurePreservesStaleResult(t *testing.T) {
	m, _, _ := setupManager(t)
	before, _ := m.Check(context.Background())
	m.Now = func() time.Time { return time.Unix(100100, 0) }
	inspected := false
	m.Inspect = func(context.Context) (Snapshot, error) { inspected = true; return Snapshot{}, nil }
	m.Refresh = func(context.Context) error { return errors.New("repository refresh failed") }
	v, e := m.Check(context.Background())
	if e != nil || !v.Stale || v.Notice || v.Error == "" || v.Snapshot.InstalledVersion != before.Snapshot.InstalledVersion || v.LastSuccessfulCheck != before.LastSuccessfulCheck || inspected {
		t.Fatal(v, e)
	}
	m.Refresh = func(context.Context) error { return nil }
	m.Inspect = func(context.Context) (Snapshot, error) { return Snapshot{}, errors.New("missing python-apt") }
	v, _ = m.Check(context.Background())
	if !v.Stale || v.Error == "" || v.LastSuccessfulCheck != before.LastSuccessfulCheck {
		t.Fatal(v)
	}
}
func TestNginxDiscoveryBusyCancellationAndAdmission(t *testing.T) {
	m, _, count := setupManager(t)
	entered, done := make(chan struct{}), make(chan struct{})
	m.Refresh = func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }
	ctx, cancel := context.WithCancel(context.Background())
	go func() { defer close(done); m.Check(ctx) }()
	<-entered
	if _, e := m.Check(context.Background()); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	if _, e := m.Dismiss("x"); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	cancel()
	<-done
	v, _ := m.Status()
	if v.Checking || !v.Stale {
		t.Fatal(v)
	}
	m.Admit = func(context.Context) (func(), error) { return nil, errors.New("maintenance busy") }
	m.Check(context.Background())
	if *count != 0 {
		t.Fatal("refreshed after failed admission")
	}
}
func TestNginxDiscoveryAgeAndUnsupportedNeverCurrent(t *testing.T) {
	m, _, _ := setupManager(t)
	m.Check(context.Background())
	m.Now = func() time.Time { return time.Unix(200000, 0) }
	v, _ := m.Status()
	if !v.Stale || v.Notice {
		t.Fatal(v)
	}
	for _, state := range []string{"unsupported", "unavailable", "held"} {
		m.Inspect = func(context.Context) (Snapshot, error) { return Snapshot{State: state}, nil }
		v, _ = m.Check(context.Background())
		if v.Notice || v.Snapshot.State == "current" {
			t.Fatal(v)
		}
	}
}
func TestNginxDiscoveryBinaryOwnerAndPayload(t *testing.T) {
	for _, pkg := range []string{"nginx", "nginx-core", "nginx-full:amd64", "nginx-extras"} {
		owner, e := owningPackage(pkg+": /usr/sbin/nginx", "/usr/sbin/nginx")
		if e != nil || owner != pkg {
			t.Fatal(owner, e)
		}
	}
	for _, raw := range []string{"manual: /usr/sbin/nginx", "nginx: /other/path", "nginx: /usr/sbin/nginx\nnginx-full: /usr/sbin/nginx"} {
		if _, e := owningPackage(raw, "/usr/sbin/nginx"); e == nil {
			t.Fatal(raw)
		}
	}
	path := filepath.Join(t.TempDir(), "nginx")
	data := []byte("fixture ELF bytes, never executed")
	os.WriteFile(path, data, 0700)
	manifest := fmt.Sprintf("%x  %s\n", md5.Sum(data), strings.TrimPrefix(path, "/"))
	if _, e := binaryDigest(path, manifest); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(path, []byte("manually replaced"), 0700)
	if _, e := binaryDigest(path, manifest); e == nil {
		t.Fatal("accepted manual replacement")
	}
}
func TestNginxDiscoveryRunnerNeverInstallsOrActivates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nginx")
	data := []byte("fixture, never executed")
	os.WriteFile(path, data, 0700)
	runner := func(_ context.Context, input, name string, args ...string) (string, error) {
		switch name {
		case "/usr/bin/dpkg-query":
			if args[0] == "--search" {
				return "nginx-core: " + path, nil
			}
			return fmt.Sprintf("%x  %s", md5.Sum(data), strings.TrimPrefix(path, "/")), nil
		case "/usr/bin/python3":
			if !strings.Contains(input, "nginx-core") {
				t.Fatal(input)
			}
			return `FLUXO_NGINX_JSON:{"state":"current","package":"nginx-core","architecture":"amd64","installed_version":"1.24.0-1","source_identity":"policy","sources":[]}`, nil
		default:
			t.Fatalf("unexpected executable %s", name)
			return "", nil
		}
	}
	v, e := inspectWith(context.Background(), runner, func(string) (string, error) { return path, nil })
	if e != nil || v.Identity == "" || v.Binary != path {
		t.Fatal(v, e)
	}
	v, e = inspectWith(context.Background(), runner, func(string) (string, error) { return "", errors.New("not found") })
	if e != nil || v.State != "unsupported" {
		t.Fatal(v, e)
	}
}

func TestNginxDiscoveryRefreshAllowsOnlyMetadataAndDetectsPolicyChange(t *testing.T) {
	for _, changed := range []bool{false, true} {
		calls := 0
		runner := func(_ context.Context, input, name string, args ...string) (string, error) {
			calls++
			if name == "/usr/bin/python3" {
				hash := strings.Repeat("a", 64)
				if changed && calls == 3 {
					hash = strings.Repeat("b", 64)
				}
				return `FLUXO_NGINX_JSON:{"policy":"` + hash + `"}`, nil
			}
			if name != "/usr/bin/apt-get" || args[len(args)-1] != "update" || input != "" {
				t.Fatal("not metadata-only", name, args)
			}
			return "", nil
		}
		e := refreshWith(context.Background(), runner)
		if (e != nil) != changed || calls != 3 {
			t.Fatal(changed, calls, e)
		}
	}
}
func TestNginxDiscoveryPersistenceFailureStalesPreviousResult(t *testing.T) {
	m, store, _ := setupManager(t)
	m.Check(context.Background())
	store.fail = true
	if _, e := m.Check(context.Background()); e == nil {
		t.Fatal("ignored persistence failure")
	}
	v, _ := m.Status()
	if !v.Stale || v.Notice || v.Error == "" {
		t.Fatal("claimed current after unconfirmed save", v)
	}
}
