package nginx

import (
	"context"
	"os"
	"testing"
	"time"

	"fluxo/internal/services/packages"
)

// Explicitly installs the real product-owned HTTP default on the disposable
// installed-panel fixture. No reload, existing vhost, or operator file changes.
// The next ordinary site operation can validate and reload it with its vhost.
func TestVMInstallManagedNginxHTTPDefaults(t *testing.T) {
	host, _ := os.Hostname()
	if host != "fluxo-bridge-prod-20261101" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || os.Getenv("FLUXO_NGINX_HTTP_DEFAULTS_VM_TEST") != "1" {
		t.Skip("explicit root opt-in on the dedicated installed-panel VM only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	release, err := packages.Lifecycle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	defaultServerMu.Lock()
	defer defaultServerMu.Unlock()
	if err := ensureHTTPDefaults(ctx, httpDefaultsEnvironment{
		path: httpDefaultsPath, trusted: trustedHTTPDefaultsPath,
		validate: validateConfig, loaded: loadedHTTPDefaults,
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(httpDefaultsPath)
	if err != nil || string(raw) != httpDefaultsConfig {
		t.Fatal("operator setting was preserved instead; explicit fixture review required", err)
	}
	t.Log("real managed HTTP default is validated and retained; no service reload was performed")
}
