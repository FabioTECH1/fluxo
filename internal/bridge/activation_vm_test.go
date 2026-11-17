package bridge

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"fluxo/internal/services/executiondomain"
	"github.com/google/uuid"
)

// This is deliberately an explicit, retained-host format mutation. It never
// stops the panel, rewinds journals, deletes fixtures or fabricates receipts.
// The operator must first install the reviewed versioned image and stop panel
// ownership. Crash phase replay is covered separately by injectable unit tests.
func TestVMExecutionFormatActivationAndRecovery(t *testing.T) {
	host, _ := os.Hostname()
	if (host != "fluxo-php-patch-test" && host != "fluxo-bridge-prod-20261101" && host != "fluxo-bridge-noble-final-20261102" && host != "fluxo-bridge-jammy-20261101" && host != "fluxo-bridge-resolute-20261101") || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || os.Getenv("FLUXO_BRIDGE_FORMAT_ACTIVATION_TEST") != "1" {
		t.Skip("explicit guarded known bridge acceptance hosts only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := panelStopped(ctx); err != nil {
		t.Fatal("stop panel ownership explicitly before this acceptance test", err)
	}
	before, err := activationDatabaseHashes("/var/lib/fluxo")
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "/usr/local/bin/fluxo", args...)
		cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "FLUXO_ENV=prod", "FLUXO_DATA_DIR=/var/lib/fluxo"}
		return cmd.CombinedOutput()
	}
	if out, err := run("execution-compatibility", "recover", uuid.NewString()); err == nil {
		t.Fatal("unowned recovery operation was accepted", string(out))
	}
	action := "activate"
	sourceFloor, err := executiondomain.ReadFloor("/var/lib/fluxo")
	if err != nil {
		t.Fatal("source reader floor", err)
	}
	if executiondomain.CurrentSchedulerSchema == executiondomain.SchedulerLoaderSchema && sourceFloor != nil && sourceFloor.SchedulerSchema == 4 {
		action = "upgrade"
	}
	out, err := run("execution-compatibility", action)
	if err != nil {
		t.Fatalf("activation refused; preserve printed operation and all evidence: %v\n%s", err, out)
	}
	var report ExecutionActivationReport
	if err = json.Unmarshal(out, &report); err != nil || !report.FormatActive || report.BridgeQualified || report.Phase != "committed" {
		t.Fatalf("format completion overclaims qualification or has invalid readback: %v %s", err, out)
	}
	floor, err := executiondomain.ReadFloor("/var/lib/fluxo")
	if err != nil || floor == nil || floor.Phase != "committed" || floor.ActivationID != report.OperationID {
		t.Fatal("committed floor differs from activation report", floor, err)
	}
	if err = validateCommittedActivationArchive("/var/lib/fluxo", *floor); err != nil {
		t.Fatal("immutable image, original evidence or exact conversion plan missing", err)
	}
	if out, err = run("execution-compatibility", "inspect"); err != nil {
		t.Fatal("ordinary reader admission after commit", err, string(out))
	}
	if out, err = run("execution-compatibility", "recover", report.OperationID); err != nil {
		t.Fatal("exact completed operation recovery", err, string(out))
	}
	after, err := activationDatabaseHashes("/var/lib/fluxo")
	if err != nil || !sameHashes(before, after) {
		t.Fatal("format activation changed installation database bytes", err)
	}
	t.Logf("format activation %s committed; provider receipts, host profiles, dispatch and complete bridge qualification remain separate gates", report.OperationID)
}

// Real filesystem atomicity/refusal proof using only a unique private fixture.
// Does not change live journals or invoke an initializer against production state.
func TestVMActivationCreationFenceNoReplace(t *testing.T) {
	host, _ := os.Hostname()
	known := host == "fluxo-bridge-noble-final-20261102" || host == "fluxo-bridge-prod-20261101" || host == "fluxo-bridge-jammy-20261101" || host == "fluxo-bridge-resolute-20261101"
	if !known || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || os.Getenv("FLUXO_BRIDGE_FORMAT_ACTIVATION_TEST") != "1" {
		t.Skip("explicit known bridge acceptance hosts only")
	}
	directory, err := os.MkdirTemp("/var/lib/fluxo", "execution-creation-proof-")
	if err != nil {
		t.Fatal(err)
	}
	path := directory + "/initialized"
	marker := []byte("activation:" + uuid.NewString() + "\n")
	if err = writeActivationFence(path, marker); err != nil {
		t.Fatal(err)
	}
	if err = writeActivationFence(path, []byte("activation:other\n")); err == nil {
		t.Fatal("late creator replaced operation marker")
	}
	actual, err := readProtected(path, 4096)
	if err != nil || string(actual) != string(marker) {
		t.Fatal("late initializer altered preserved marker", err)
	}
	t.Log("atomic no-replace creation evidence retained at", directory)
}
