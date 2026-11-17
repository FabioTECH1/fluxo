package executiondomain

import "testing"

func TestPersistentSelectionDoesNotExpandToUnknownHosts(t *testing.T) {
	if !persistentSelectionPlatform("ID=ubuntu\nVERSION_ID=\"22.04\"\n", "amd64") {
		t.Fatal("reviewed Jammy platform refused")
	}
	for _, tc := range []struct{ release, arch string }{
		{"ID=ubuntu\nVERSION_ID=\"22.04\"\n", "arm64"},
		{"ID=ubuntu\nVERSION_ID=\"24.04\"\n", "amd64"},
		{"ID=ubuntu\nVERSION_ID=\"26.04\"\n", "amd64"},
		{"ID=debian\nVERSION_ID=\"22.04\"\n", "amd64"},
		{"ID=ubuntu\nID=debian\nVERSION_ID=\"22.04\"\n", "amd64"},
	} {
		if persistentSelectionPlatform(tc.release, tc.arch) {
			t.Fatal("unqualified249 host selected", tc)
		}
	}
}
