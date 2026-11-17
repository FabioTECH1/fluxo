package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Read-only profile inspection on the retained Noble VM. It neither activates
// a format nor installs/replaces a panel/helper or changes a cron/service.
func TestVMNobleStrictLoaderClosure(t *testing.T) {
	hostname, _ := os.Hostname()
	if os.Geteuid() != 0 || hostname != "fluxo-bridge-prod-20261101" || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("retained Noble VM root read-only opt-in only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	floorBefore, floorErr := os.ReadFile("/var/lib/fluxo/execution-readers.json")
	host, err := QualifyCronBackend(ctx)
	if err != nil || host.Backend != "" {
		t.Fatal("existing authentic stock Noble cron identity prerequisite", err)
	}
	report, inspectErr := inspectPAMCandidate(nobleLoaderPAMProfileID, host.PID)
	provider, providerErr := qualifyLoaderProvider(ctx, nil)
	evidence := map[string]any{"profile": nobleLoaderPAMProfileID, "closure": report, "provider": provider, "production_enabled": false, "format_activated": false}
	if providerErr != nil {
		evidence["provider_failure"] = providerErr.Error()
	}
	raw, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := "/var/lib/fluxo-noble-loader-closure-" + uuid.NewString() + ".json"
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(append(raw, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	floorAfter, afterErr := os.ReadFile("/var/lib/fluxo/execution-readers.json")
	if (floorErr == nil) != (afterErr == nil) || !bytes.Equal(floorBefore, floorAfter) {
		t.Fatal("read-only inspector changed/observed changing format floor")
	}
	t.Log("read-only Noble profile evidence", path)
	if inspectErr != nil {
		t.Fatal("Noble closure does not qualify", inspectErr)
	}
	if providerErr != nil {
		t.Fatal("Noble loader provider does not qualify", providerErr)
	}
}
