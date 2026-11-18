package maintenancetask

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestVMARM64MaintenanceProfiles(t *testing.T) {
	if os.Geteuid() != 0 || !arm64NativeVMHost() {
		t.Skip("dedicated native ARM fixture only")
	}
	for _, kind := range []Kind{ComposerUpdate, CertificateRenewal, PackageCleanup} {
		t.Run(string(kind), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			p, err := qualifyProfile(ctx, kind)
			if err != nil {
				t.Fatal("native fixed-task profile", err)
			}
			if len(p.Files) == 0 {
				t.Fatal("missing bound profile inputs")
			}
		})
	}
}

// Acquisition-method sentinels run as _apt, so caller-specific tests retain
// their own UID checks in addition to this exact host/architecture/opt-in gate.
func arm64NativeVMHost() bool {
	host, _ := os.Hostname()
	return runtime.GOARCH == "arm64" && os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") == "1" && (host == "fluxo-arm64-2204-acceptance" || host == "fluxo-arm64-2404-acceptance" || host == "fluxo-arm64-2604-acceptance")
}
