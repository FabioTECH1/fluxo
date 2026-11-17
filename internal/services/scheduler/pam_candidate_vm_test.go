package scheduler

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Read-only host qualification. The sole write is a unique root-owned public
// evidence artifact; no PAM call, cron trigger, user/group mutation or service
// operation is performed. Actual root/nonroot cron execution tests are separate.
func TestVMDisabledCronPAMNSSClosure(t *testing.T) {
	host, _ := os.Hostname()
	id := ""
	switch host {
	case "fluxo-bridge-jammy-20261101":
		id = jammyCronCandidateID
	case "fluxo-bridge-resolute-20261101":
		id = resoluteCronCandidateID
	}
	if id == "" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("guarded known Ubuntu22/26 root acceptance hosts only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	h, err := inspectDistributionCronCandidate(ctx, id)
	if err != nil {
		t.Fatal("existing disabled daemon/config candidate prerequisite", err)
	}
	report, inspectionErr := inspectPAMCandidate(id, h.PID)
	after, err := inspectDistributionCronCandidate(ctx, id)
	if err != nil || h != after {
		report.Qualified = false
		report.Failure = "cron daemon/config changed around PAM/NSS closure inspection"
		if inspectionErr == nil {
			inspectionErr = err
		}
		if inspectionErr == nil {
			inspectionErr = context.Canceled
		}
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := "/var/lib/fluxo-cron-pam-candidate-" + uuid.NewString() + ".json"
	evidence, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = evidence.Write(append(raw, '\n')); err == nil {
		err = evidence.Sync()
	}
	closeErr := evidence.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.Open("/var/lib")
	if err == nil {
		err = directory.Sync()
		directory.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Log("public disabled-candidate evidence retained at", path)
	if inspectionErr != nil {
		t.Fatal("PAM/NSS candidate remains unqualified", inspectionErr)
	}
	t.Log("disabled PAM/NSS configuration/code closure passed; production selector and journal formats remain unchanged")
}
