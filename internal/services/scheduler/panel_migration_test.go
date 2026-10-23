package scheduler

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestPanelBoundaryRequiresCurrentBootAndExactBinary(t *testing.T) {
	boot, err := bootID()
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := fileDigest(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyPanelBoundary(journal{PanelProof: &panelProof{Boot: boot, Binary: hash}}); err != nil {
		t.Fatal(err)
	}
	for _, proof := range []*panelProof{nil, {Boot: "earlier-boot", Binary: hash}, {Boot: boot, Binary: strings.Repeat("0", 64)}} {
		err := verifyPanelBoundary(journal{PanelProof: proof})
		var diagnostic *diagnosticError
		if !errors.As(err, &diagnostic) || diagnostic.code != "panel_proof_required" || !strings.Contains(err.Error(), "scheduler-verify-panel") {
			t.Fatalf("invalid proof did not fail closed with recovery guidance: %v", err)
		}
	}
}
