package bridge

import (
	"context"
	"os"
	"path/filepath"
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
