package bootstrap

import "testing"

func TestManagedSiteOwnershipTarget(t *testing.T) {
	tests := []struct {
		name       string
		domain     string
		storedPath string
		want       string
		ok         bool
	}{
		{name: "managed site", domain: "example.com", storedPath: "/home/fluxo/example.com", want: "/home/fluxo/example.com", ok: true},
		{name: "outside managed root", domain: "example.com", storedPath: "/srv/example.com", ok: false},
		{name: "parent traversal", domain: "example.com", storedPath: "/home/fluxo/example.com/../other", ok: false},
		{name: "invalid domain", domain: "../example.com", storedPath: "/home/fluxo/example.com", ok: false},
		{name: "home root", domain: "example.com", storedPath: "/home/fluxo", ok: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := managedSiteOwnershipTarget(test.domain, test.storedPath)
			if got != test.want || ok != test.ok {
				t.Fatalf("managedSiteOwnershipTarget() = (%q, %t), want (%q, %t)", got, ok, test.want, test.ok)
			}
		})
	}
}
