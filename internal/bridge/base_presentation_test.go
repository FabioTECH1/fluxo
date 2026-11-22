package bridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/services/executiondomain/testfixture"
)

func TestBaseRefusalAdviceDoesNotRouteAdmittedBaseToBridge(t *testing.T) {
	for _, mode := range []string{"base", "base-recovery", "fresh", "", "unknown"} {
		if got := BaseInstallerRefusal(context.Background(), t.TempDir(), BaseAdmission{Mode: mode}); got != "review" {
			t.Fatal(mode, got)
		}
	}
}

func TestBaseRefusalPreservesPendingAndUnreadableEvidence(t *testing.T) {
	if !testfixture.InRoot(t, "") {
		return
	}
	data := "/var/lib/fluxo"
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	admission := BaseAdmission{Mode: "bridge"}
	if got := BaseInstallerRefusal(context.Background(), data, admission); got != "bridge" {
		t.Fatal(got)
	}
	v := installerTaskIntentFixture(installerHelperRefresh)
	path := filepath.Join(data, installerExecutionRecord)
	if err := writeActivationJSON(path, v); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := BaseInstallerRefusal(context.Background(), data, admission); got != "recovery" {
		t.Fatal(got)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("advice changed pending authority")
	}
	if err := os.WriteFile(path, []byte("invalid protected intent"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := BaseInstallerRefusal(context.Background(), data, admission); got != "review" {
		t.Fatal(got)
	}
}

func TestCompletedFreshRecordDoesNotInventPendingRecovery(t *testing.T) {
	if !testfixture.InRoot(t, "") {
		return
	}
	data := "/var/lib/fluxo"
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	intent := BaseFreshIntent{Format: 1, Version: "0.4.54-base-test.4", ImageSHA256: strings.Repeat("a", 64), Phase: "complete", InstallationID: strings.Repeat("b", 32), OperationID: "2177ef94-161b-4a2e-b37d-7dab78129c9d", PlanSHA256: strings.Repeat("c", 64)}
	path := filepath.Join(data, baseFreshRecord)
	if err := writeActivationJSON(path, intent); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"base", "base-recovery", ""} {
		if got := BaseInstallerRefusal(context.Background(), data, BaseAdmission{Mode: mode}); got != "review" {
			t.Fatal("completed fresh record invented recovery", mode, got)
		}
	}
	intent.Phase = "activating"
	if err := writeActivationJSON(path, intent); err != nil {
		t.Fatal(err)
	}
	if got := BaseInstallerRefusal(context.Background(), data, BaseAdmission{}); got != "recovery" {
		t.Fatal("unfinished initialization lost recovery advice", got)
	}
}
