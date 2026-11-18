package scheduler

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestVMARM64LoaderClosure(t *testing.T) {
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || runtime.GOARCH != "arm64" || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || (host != "fluxo-arm64-2204-acceptance" && host != "fluxo-arm64-2404-acceptance" && host != "fluxo-arm64-2604-acceptance") {
		t.Skip("dedicated native ARM fixture only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	provider, err := inspectLoaderProvider(ctx, nil)
	if err != nil {
		t.Fatal("native provider closure", err)
	}
	if !loaderDigestValid(provider.PolicyHash) || !loaderDigestValid(provider.RuntimeHash) {
		t.Fatal("provider lacks policy/runtime binding")
	}
	if err = VerifyCronBackendCurrent(ctx, provider.Identity); err != nil {
		t.Fatal("provider changed during inspection", err)
	}
	t.Log("native loader/provider closure qualified", provider.Profile, provider.PolicyHash)
}
