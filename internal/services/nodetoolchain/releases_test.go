package nodetoolchain

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/syscmd"
)

func TestNodeUpdateSelectionStaysWithinSupportedMajorAndArchitecture(t *testing.T) {
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	index := []byte(`[{"version":"v26.2.0","lts":false,"npm":"11.0.0","files":["linux-x64"]},{"version":"v24.20.1","lts":"Krypton","npm":"11.18.0","files":["linux-x64"]},{"version":"v24.20.0","lts":"Krypton","npm":"11.17.0","files":["linux-x64","linux-arm64"]},{"version":"v22.24.0","lts":"Jod","files":["linux-x64"]},{"version":"v24.21.0-rc.1","lts":"Krypton","files":["linux-x64"]}]`)
	for _, tc := range []struct{ installed, arch, want string }{{"24.19.0", "x64", "24.20.1"}, {"24.19.0", "arm64", "24.20.0"}, {"24.20.1", "x64", ""}, {"22.23.0", "x64", "22.24.0"}} {
		got, _, err := SelectUpdate(index, tc.installed, tc.arch, now)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %s %v", tc, got, err)
		}
	}
	for _, version := range []string{"20.0.0", "23.0.0", "26.2.0", "invalid"} {
		if _, _, err := SelectUpdate(index, version, "x64", now); err == nil {
			t.Fatalf("unsupported %s accepted", version)
		}
	}
	if err := SupportedMajor("24.19.0", time.Date(2027, 5, 1, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("stale support policy accepted")
	}
}
func TestNodeManifestUsesRealSignatureAndExplicitTrust(t *testing.T) {
	signed, err := os.ReadFile("testdata/node-manifest.asc")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := os.ReadFile("testdata/node-test-keyring.kbx")
	if err != nil {
		t.Fatal(err)
	}
	sums, signer, err := verifyNodeManifest(context.Background(), signed, keys)
	if err != nil {
		t.Fatal(err)
	}
	if signer == "" || !strings.Contains(string(sums), "node-v24.1.0-linux-x64.tar.gz") {
		t.Fatal("verified manifest not returned")
	}
	for _, tc := range []struct {
		name         string
		message, key []byte
	}{{"unknown signer", signed, nodeReleaseKeys}, {"tampered", []byte(strings.Replace(string(signed), "24.1.0", "24.2.0", 1)), keys}, {"unsigned", sums, keys}, {"empty", nil, keys}} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := verifyNodeManifest(context.Background(), tc.message, tc.key); err == nil {
				t.Fatal("untrusted manifest accepted")
			}
		})
	}
}

func TestNodeRetainedManifestReauthenticatesAcrossKeyrings(t *testing.T) {
	signed, err := os.ReadFile("testdata/node-manifest.asc")
	if err != nil {
		t.Fatal(err)
	}
	keysA, err := os.ReadFile("testdata/node-test-keyring.kbx")
	if err != nil {
		t.Fatal(err)
	}
	before, err := authenticateManifestRelease(context.Background(), "24.1.0", "11.0.0", "x64", signed, keysA)
	if err != nil {
		t.Fatal(err)
	}
	// A harmless keybox metadata reserialization changes its revision while
	// leaving the actual signer and signed archive unchanged.
	reserialized := append([]byte(nil), keysA...)
	reserialized[23] ^= 1 // keybox header's last-maintenance timestamp

	// Also exercise a genuine additive trust update using only public keys.
	dir := t.TempDir()
	base := filepath.Join(dir, "base.kbx")
	if err = os.WriteFile(base, nodeReleaseKeys, 0600); err != nil {
		t.Fatal(err)
	}
	public, err := syscmd.Run(context.Background(), 15*time.Second, "/usr/bin/gpg", "--homedir", dir, "--batch", "--no-options", "--no-autostart", "--no-default-keyring", "--keyring", base, "--export")
	if err != nil {
		t.Fatal(err)
	}
	pub := filepath.Join(dir, "public.gpg")
	additivePath := filepath.Join(dir, "additive.kbx")
	if err = os.WriteFile(pub, []byte(public), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(additivePath, keysA, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = syscmd.Run(context.Background(), 15*time.Second, "/usr/bin/gpg", "--homedir", dir, "--batch", "--no-options", "--no-autostart", "--no-default-keyring", "--keyring", additivePath, "--import", pub); err != nil {
		t.Fatal(err)
	}
	additive, err := os.ReadFile(additivePath)
	if err != nil {
		t.Fatal(err)
	}
	for name, keysB := range map[string][]byte{"reserialized": reserialized, "additive": additive} {
		t.Run(name, func(t *testing.T) {
			after, err := authenticateManifestRelease(context.Background(), "24.1.0", "11.0.0", "x64", signed, keysB)
			if err != nil {
				t.Fatal(err)
			}
			if after.TrustRevision == before.TrustRevision || after.Signer != before.Signer || after.SHA256 != before.SHA256 || after.ManifestSHA256 != before.ManifestSHA256 {
				t.Fatal("fresh signature proof lost the exact retained archive identity", before, after)
			}
		})
	}
	// Removing the fixture signer, unlike adding keys, MUST reject the proof.
	if _, err = authenticateManifestRelease(context.Background(), "24.1.0", "11.0.0", "x64", signed, nodeReleaseKeys); err == nil {
		t.Fatal("removed signer was silently reauthorized")
	}
}

func TestNodeRetainedManifestRejectsRevokedSigner(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		base := []string{"--homedir", dir, "--batch", "--no-options", "--pinentry-mode", "loopback", "--passphrase", ""}
		out, err := syscmd.Run(context.Background(), 15*time.Second, "/usr/bin/gpg", append(base, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	t.Cleanup(func() {
		_, _ = syscmd.Run(context.Background(), 5*time.Second, "/usr/bin/gpgconf", "--homedir", dir, "--kill", "gpg-agent")
	})
	run("--quick-generate-key", "Fluxo disposable revocation fixture <test@invalid>", "ed25519", "sign", "0")
	fingerprint := ""
	for _, line := range strings.Split(run("--with-colons", "--list-keys"), "\n") {
		f := strings.Split(line, ":")
		if len(f) > 9 && f[0] == "fpr" {
			fingerprint = f[9]
			break
		}
	}
	if fingerprint == "" {
		t.Fatal("fixture signer missing")
	}
	manifest := filepath.Join(dir, "manifest.txt")
	signature := filepath.Join(dir, "manifest.asc")
	if err := os.WriteFile(manifest, []byte(strings.Repeat("a", 64)+"  node-v24.1.0-linux-x64.tar.gz\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("--local-user", fingerprint, "--output", signature, "--clearsign", manifest)
	signed, err := os.ReadFile(signature)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := os.ReadFile(filepath.Join(dir, "pubring.kbx"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = authenticateManifestRelease(context.Background(), "24.1.0", "11.0.0", "x64", signed, keys); err != nil {
		t.Fatal("valid fixture rejected before revocation", err)
	}
	revocation, err := os.ReadFile(filepath.Join(dir, "openpgp-revocs.d", fingerprint+".rev"))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(revocation), ":-----BEGIN PGP PUBLIC KEY BLOCK-----")
	if start < 0 {
		t.Fatal("fixture revocation certificate missing")
	}
	revokedFile := filepath.Join(dir, "revocation.asc")
	if err = os.WriteFile(revokedFile, revocation[start+1:], 0600); err != nil {
		t.Fatal(err)
	}
	run("--import", revokedFile)
	keys, err = os.ReadFile(filepath.Join(dir, "pubring.kbx"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = authenticateManifestRelease(context.Background(), "24.1.0", "11.0.0", "x64", signed, keys); err == nil {
		t.Fatal("revoked signer reauthorized a retained archive")
	}
}
func TestNodeManifestChecksumRequiresUniqueExactAsset(t *testing.T) {
	name := "node-v24.1.0-linux-x64.tar.gz"
	line := strings.Repeat("a", 64) + "  " + name + "\n"
	for _, s := range []string{line + line, strings.Replace(line, "x64", "arm64", 1), "invalid  " + name} {
		if _, err := exactChecksum(s, name); err == nil {
			t.Fatal("ambiguous or invalid checksum accepted")
		}
	}
	if _, err := exactChecksum(line, name); err != nil {
		t.Fatal(err)
	}
}
func nodeArchive(t *testing.T, headers []*tar.Header) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "node.tar.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, h := range headers {
		if err = tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			if _, err = tw.Write([]byte(strings.Repeat("x", int(h.Size)))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestNodeArchiveRejectsEscapesAndLinksBeforeExtracting(t *testing.T) {
	root := "node-v24.1.0-linux-x64"
	base := []*tar.Header{{Name: root + "/bin/node", Mode: 0755, Size: 1}, {Name: root + "/lib/node_modules/npm/package.json", Mode: 0644, Size: 1}}
	if err := extractNodeArchive(nodeArchive(t, base), t.TempDir(), root); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []*tar.Header{{Name: "../escape", Mode: 0644, Size: 1}, {Name: "/absolute", Mode: 0644, Size: 1}, {Name: root + "/../escape", Mode: 0644, Size: 1}, {Name: root + "/bad", Typeflag: tar.TypeSymlink, Linkname: "../../escape"}, {Name: root + "/bad", Typeflag: tar.TypeLink, Linkname: root + "/bin/node"}, {Name: root + "/bad", Typeflag: tar.TypeChar}} {
		headers := append(append([]*tar.Header{}, base...), bad)
		if err := extractNodeArchive(nodeArchive(t, headers), t.TempDir(), root); err == nil {
			t.Fatalf("unsafe entry accepted: %+v", bad)
		}
	}
	headers := append(append([]*tar.Header{}, base...), &tar.Header{Name: root + "/alias", Typeflag: tar.TypeSymlink, Linkname: "bin"}, &tar.Header{Name: root + "/alias/injected", Mode: 0644, Size: 1})
	if err := extractNodeArchive(nodeArchive(t, headers), t.TempDir(), root); err == nil {
		t.Fatal("archive wrote through a symlink")
	}
}
func TestNodeTreeDigestBindsFilesLinksAndModes(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "node")
	if err := os.WriteFile(p, []byte("original"), 0755); err != nil {
		t.Fatal(err)
	}
	before, err := TreeDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(p, 0644); err != nil {
		t.Fatal(err)
	}
	after, _ := TreeDigest(root)
	if before == after {
		t.Fatal("mode changes were not bound")
	}
	if err = os.WriteFile(p, []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	last, _ := TreeDigest(root)
	if after == last {
		t.Fatal("file changes were not bound")
	}
}
