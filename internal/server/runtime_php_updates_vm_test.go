package server

import (
	"context"
	"os"
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
