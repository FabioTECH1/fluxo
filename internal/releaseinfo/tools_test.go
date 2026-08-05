package releaseinfo

import "testing"

func TestInstallerToolVersions(t *testing.T) {
	originalComposer := ComposerVersion
	originalComposerSHA256 := ComposerSHA256
	originalWPCLI := WPCLIVersion
	t.Cleanup(func() {
		ComposerVersion = originalComposer
		ComposerSHA256 = originalComposerSHA256
		WPCLIVersion = originalWPCLI
	})

	tests := []struct {
		name     string
		composer string
		sha256   string
		wpCLI    string
		wantErr  bool
	}{
		{name: "valid", composer: "2.10.2", sha256: "5ee7125f8a30a34d246cefdc0bc85b8a783b28f2aec968994118512350d28027", wpCLI: "2.12.0"},
		{name: "trimmed", composer: " 2.10.2 ", sha256: " 5EE7125F8A30A34D246CEFDC0BC85B8A783B28F2AEC968994118512350D28027 ", wpCLI: " 2.12.0 "},
		{name: "missing composer", sha256: "5ee7125f8a30a34d246cefdc0bc85b8a783b28f2aec968994118512350d28027", wpCLI: "2.12.0", wantErr: true},
		{name: "missing composer hash", composer: "2.10.2", wpCLI: "2.12.0", wantErr: true},
		{name: "invalid composer hash", composer: "2.10.2", sha256: "not-a-hash", wpCLI: "2.12.0", wantErr: true},
		{name: "missing wp cli", composer: "2.10.2", sha256: "5ee7125f8a30a34d246cefdc0bc85b8a783b28f2aec968994118512350d28027", wantErr: true},
		{name: "prerelease", composer: "2.11.0-RC1", sha256: "5ee7125f8a30a34d246cefdc0bc85b8a783b28f2aec968994118512350d28027", wpCLI: "2.12.0", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ComposerVersion = test.composer
			ComposerSHA256 = test.sha256
			WPCLIVersion = test.wpCLI
			composer, composerSHA256, wpCLI, err := InstallerToolVersions()
			if test.wantErr {
				if err == nil {
					t.Fatal("InstallerToolVersions() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("InstallerToolVersions() error = %v", err)
			}
			if composer != "2.10.2" || composerSHA256 != "5ee7125f8a30a34d246cefdc0bc85b8a783b28f2aec968994118512350d28027" || wpCLI != "2.12.0" {
				t.Fatalf("InstallerToolVersions() = %q, %q, %q", composer, composerSHA256, wpCLI)
			}
		})
	}
}
