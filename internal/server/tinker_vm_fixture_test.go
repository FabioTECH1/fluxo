package server

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"fluxo/internal/syscmd"
)

func requireTinkerVM(t *testing.T) {
	t.Helper()
	host, _ := os.Hostname()
	switch host {
	case "fluxo-php-patch-test", "fluxo-bridge-jammy-20261101", "fluxo-bridge-prod-20261101", "fluxo-bridge-resolute-20261101":
	default:
		t.Skip("guarded retained or dedicated bridge VM only")
	}
	if os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("requires root and explicit guarded VM opt-in")
	}
	if _, err := syscmd.TinkerOwner(); err != nil {
		t.Fatal(err)
	}
}

// Dedicated VM fixtures must be explicitly staged public applications, never a
// discovered installed site. The retained VM keeps its existing fixture.
func tinkerVMApp(t *testing.T) string {
	t.Helper()
	requireTinkerVM(t)
	host, _ := os.Hostname()
	if host == "fluxo-php-patch-test" {
		return "/home/fluxo/php-patch-acceptance"
	}
	path := os.Getenv("FLUXO_TINKER_VM_APP")
	if !regexp.MustCompile(`^/home/fluxo/bridge-tinker-acceptance-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(path) {
		t.Fatal("FLUXO_TINKER_VM_APP must identify an explicitly staged bridge-tinker-acceptance UUID fixture")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil || real != path {
		t.Fatalf("fixture must be an existing canonical directory: %q (%v)", path, err)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatalf("fixture is not a directory: %q (%v)", path, err)
	}
	return path
}
