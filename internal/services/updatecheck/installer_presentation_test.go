package updatecheck

import (
	"os"
	"strings"
	"testing"
)

func installerPresentationFixture(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(raw), "# BEGIN TERMINAL PRESENTATION\n")
	functions, _, end := strings.Cut(tail, "# END TERMINAL PRESENTATION")
	if !ok || !end {
		t.Fatal("missing installer presentation helpers")
	}
	return functions
}
