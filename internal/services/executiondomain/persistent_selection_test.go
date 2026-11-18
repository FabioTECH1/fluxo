package executiondomain

import "testing"

func TestPersistentSelectionDoesNotExpandToUnknownHosts(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		if !persistentSelectionPlatform("ID=ubuntu\nVERSION_ID=\"22.04\"\n", arch) {
			t.Fatal("reviewed Jammy platform refused", arch)
		}
	}
	for _, tc := range []struct{ release, arch string }{
		{"ID=ubuntu\nVERSION_ID=\"22.04\"\n", "arm"},
		{"ID=ubuntu\nVERSION_ID=\"22.04\"\n", "386"},
		{"ID=ubuntu\nVERSION_ID=\"24.04\"\n", "amd64"},
		{"ID=ubuntu\nVERSION_ID=\"26.04\"\n", "amd64"},
		{"ID=ubuntu\nVERSION_ID=\"24.04\"\n", "arm64"},
		{"ID=ubuntu\nVERSION_ID=\"26.04\"\n", "arm64"},
		{"ID=debian\nVERSION_ID=\"22.04\"\n", "amd64"},
		{"ID=ubuntu\nID=debian\nVERSION_ID=\"22.04\"\n", "amd64"},
	} {
		if persistentSelectionPlatform(tc.release, tc.arch) {
			t.Fatal("unqualified249 host selected", tc)
		}
	}
}
