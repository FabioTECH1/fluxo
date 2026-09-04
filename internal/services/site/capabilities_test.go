package site

import "testing"

func TestApplicationCapabilities(t *testing.T) {
	tests := []struct {
		appType          string
		valid            bool
		usesPHP          bool
		supportsDatabase bool
		reverseProxy     bool
		zeroDowntime     bool
	}{
		{"laravel", true, true, true, false, true},
		{"php", true, true, true, false, true},
		{"wordpress", true, true, true, false, false},
		{"html", true, false, false, false, true},
		{"node", true, false, false, true, true},
		{"python", true, false, true, true, true},
		{"unknown", false, false, false, false, false},
	}

	for _, test := range tests {
		t.Run(test.appType, func(t *testing.T) {
			if got := IsValidAppType(test.appType); got != test.valid {
				t.Fatalf("IsValidAppType() = %v, want %v", got, test.valid)
			}
			if got := UsesPHP(test.appType); got != test.usesPHP {
				t.Fatalf("UsesPHP() = %v, want %v", got, test.usesPHP)
			}
			if got := SupportsDatabase(test.appType); got != test.supportsDatabase {
				t.Fatalf("SupportsDatabase() = %v, want %v", got, test.supportsDatabase)
			}
			if got := UsesReverseProxy(test.appType); got != test.reverseProxy {
				t.Fatalf("UsesReverseProxy() = %v, want %v", got, test.reverseProxy)
			}
			if got := SupportsZeroDowntime(test.appType); got != test.zeroDowntime {
				t.Fatalf("SupportsZeroDowntime() = %v, want %v", got, test.zeroDowntime)
			}
		})
	}
}
