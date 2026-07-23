package safeinput

import "testing"

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
