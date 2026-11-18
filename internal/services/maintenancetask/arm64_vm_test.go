package maintenancetask

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestVMARM64MaintenanceProfiles(t *testing.T) {
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || runtime.GOARCH != "arm64" || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || (host != "fluxo-arm64-2204-acceptance" && host != "fluxo-arm64-2404-acceptance" && host != "fluxo-arm64-2604-acceptance") {
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
