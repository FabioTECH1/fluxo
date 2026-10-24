package phpupdate

import (
	"errors"
	"fluxo/internal/services/php"
	"strings"
	"testing"
)

func TestPHPNewCheckDistinguishesMissingAndIncompleteRuntime(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items map[string]php.InstalledFPM
		want  error
	}{
		{"removed", map[string]php.InstalledFPM{}, ErrNotInstalled},
		{"missing executable", map[string]php.InstalledFPM{"8.5": {}}, ErrUnsupported},
		{"usable stopped or running", map[string]php.InstalledFPM{"8.5": {Available: true}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := requireFPMInventory("8.5", tc.items)
			if !errors.Is(err, tc.want) {
				t.Fatalf("wrong check eligibility: %v", err)
			}
			if err != nil && !strings.Contains(err.Error(), "PHP 8.5") {
				t.Fatal("missing branch context", err)
			}
		})
	}
}
