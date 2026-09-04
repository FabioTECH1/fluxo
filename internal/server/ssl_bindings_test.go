package server

import (
	"path/filepath"
	"testing"

	"fluxo/internal/database"
)

func TestGetSiteApplicationWebRootMatchesRuntimeLayout(t *testing.T) {
	sitePath := "/home/fluxo/example.com"
	tests := []struct {
		name            string
		webRoot         string
		strategy        string
		appType         string
		nodePreset      string
		nodeMode        string
		staticOutputDir string
		pythonPreset    string
		appDirectory    string
		want            string
	}{
		{name: "standard PHP", webRoot: "/public", appType: "php", want: filepath.Join(sitePath, "public")},
		{name: "zero-downtime static Node", webRoot: "/", strategy: "zero-downtime", appType: "node", nodePreset: "generic", nodeMode: "static", staticOutputDir: "dist", want: filepath.Join(sitePath, "current", "dist")},
		{name: "standard Django subdirectory", webRoot: "/", appType: "python", pythonPreset: "django", appDirectory: "backend", want: filepath.Join(sitePath, "backend")},
		{name: "zero-downtime Django subdirectory", webRoot: "/", strategy: "zero-downtime", appType: "python", pythonPreset: "django", appDirectory: "backend", want: filepath.Join(sitePath, "current", "backend")},
		{name: "zero-downtime FastAPI", webRoot: "/", strategy: "zero-downtime", appType: "python", pythonPreset: "fastapi", appDirectory: "backend", want: filepath.Join(sitePath, "current")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := getSiteApplicationWebRoot(sitePath, test.webRoot, test.strategy, test.appType, test.nodePreset, test.nodeMode, test.staticOutputDir, test.pythonPreset, test.appDirectory)
			if err != nil {
				t.Fatalf("resolve application webroot: %v", err)
			}
			if got != test.want {
				t.Fatalf("webroot = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCertificateBindingActionFor(t *testing.T) {
	tests := []struct {
		name      string
		binding   *database.CertificateDomainBinding
		oldCovers bool
		newCovers bool
		expected  certificateBindingAction
	}{
		{
			name:      "preserve working alias when replacement does not cover it",
			oldCovers: true,
			expected:  certificateBindingPreserve,
		},
		{
			name:      "release automatic binding when replacement covers alias",
			binding:   &database.CertificateDomainBinding{Origin: database.CertificateBindingOriginPreserved},
			newCovers: true,
			expected:  certificateBindingRelease,
		},
		{
			name:     "retain automatic binding while replacement does not cover alias",
			binding:  &database.CertificateDomainBinding{Origin: database.CertificateBindingOriginPreserved},
			expected: certificateBindingKeep,
		},
		{
			name:      "never release manual override",
			binding:   &database.CertificateDomainBinding{Origin: database.CertificateBindingOriginManual},
			newCovers: true,
			expected:  certificateBindingKeep,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := certificateBindingActionFor(test.binding, test.oldCovers, test.newCovers); actual != test.expected {
				t.Fatalf("expected action %d, got %d", test.expected, actual)
			}
		})
	}
}

func TestCertificateBindingMutations(t *testing.T) {
	preserved := database.CertificateDomainBinding{
		CertificateID: 7,
		Origin:        database.CertificateBindingOriginPreserved,
	}
	changes := []certificateBindingChange{
		{domain: "release.example.com", previous: &preserved},
		{
			domain:        "preserve.example.com",
			certificateID: 7,
			origin:        database.CertificateBindingOriginPreserved,
		},
	}

	forward := certificateBindingMutations(changes, false)
	if forward[0].CertificateID != 0 || forward[1].CertificateID != 7 {
		t.Fatalf("unexpected forward mutations: %#v", forward)
	}
	rollback := certificateBindingMutations(changes, true)
	if rollback[0].CertificateID != 7 || rollback[0].Origin != database.CertificateBindingOriginPreserved {
		t.Fatalf("expected released binding to be restored: %#v", rollback[0])
	}
	if rollback[1].CertificateID != 0 {
		t.Fatalf("expected preserved binding to be removed: %#v", rollback[1])
	}
}
