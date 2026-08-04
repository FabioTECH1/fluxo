package safeinput

import (
	"strings"
	"testing"
)

func TestValidateAdminUsername(t *testing.T) {
	valid := []string{"admin", "site owner", "admin@example.com", "管理者"}
	for _, username := range valid {
		if !ValidateAdminUsername(username) {
			t.Errorf("ValidateAdminUsername(%q) = false, want true", username)
		}
	}

	invalid := []string{"", "__bootstrap__", " admin", "admin ", "admin\nowner", "admin\towner", strings.Repeat("a", 65)}
	for _, username := range invalid {
		if ValidateAdminUsername(username) {
			t.Errorf("ValidateAdminUsername(%q) = true, want false", username)
		}
	}
}

func TestNormalizeManagedSitePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		ok   bool
	}{
		{name: "managed path", path: "/home/fluxo/example.com", ok: true},
		{name: "stable path after promotion", path: "/home/fluxo/old.example.com", ok: true},
		{name: "nested path", path: "/home/fluxo/example.com/current"},
		{name: "traversal", path: "/home/fluxo/example.com/../other.example.com"},
		{name: "outside root", path: "/srv/example.com"},
		{name: "invalid basename", path: "/home/fluxo/example"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeManagedSitePath(test.path)
			if (err == nil) != test.ok {
				t.Fatalf("NormalizeManagedSitePath(%q) = %q, %v", test.path, got, err)
			}
			if test.ok && got != test.path {
				t.Fatalf("NormalizeManagedSitePath(%q) = %q", test.path, got)
			}
		})
	}
}
