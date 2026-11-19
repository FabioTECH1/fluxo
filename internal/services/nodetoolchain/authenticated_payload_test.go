package nodetoolchain

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type payloadTestTransport func(*http.Request) (*http.Response, error)

func (f payloadTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func payloadTestTar(t *testing.T, files map[string]string, extra ...*tar.Header) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: "package/" + name, Mode: 0644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, data); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range extra {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func payloadTestWrite(t *testing.T, root, name, data string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestAuthenticatedManagersCompareOfficialBytesAndPreserveHistory(t *testing.T) {
	// All filesystem paths and network responses are isolated. No package manager
	// is executed, and all pins are restored after this test.
	oldCV, oldCI, oldPV, oldPI, oldYV, oldYI := PinnedCorepackVersion, PinnedCorepackIntegrity, PinnedPNPMVersion, PinnedPNPMIntegrity, PinnedYarnVersion, PinnedYarnIntegrity
	oldTransport := http.DefaultTransport
	t.Cleanup(func() {
		PinnedCorepackVersion, PinnedCorepackIntegrity, PinnedPNPMVersion, PinnedPNPMIntegrity, PinnedYarnVersion, PinnedYarnIntegrity = oldCV, oldCI, oldPV, oldPI, oldYV, oldYI
		http.DefaultTransport = oldTransport
	})
	toolchain, cache := t.TempDir(), t.TempDir()
	artifacts := make(map[string][]byte)
	for _, component := range []struct {
		name, pkg, version string
		bins               map[string]string
		versionPin, sriPin *string
	}{
		{"corepack", "corepack", "0.35.0", map[string]string{"corepack": "./bin/corepack.js"}, &PinnedCorepackVersion, &PinnedCorepackIntegrity},
		{"pnpm", "pnpm", "11.20.0", map[string]string{"pnpm": "bin/pnpm.mjs", "pnpx": "bin/pnpx.mjs", "pn": "bin/pnpm.mjs", "pnx": "bin/pnpx.mjs"}, &PinnedPNPMVersion, &PinnedPNPMIntegrity},
		{"yarn", "@yarnpkg/cli-dist", "4.18.0", map[string]string{"yarn": "./bin/yarn.js", "yarnpkg": "./bin/yarn.js"}, &PinnedYarnVersion, &PinnedYarnIntegrity},
	} {
		manifest, err := json.Marshal(map[string]any{"name": component.pkg, "version": component.version, "bin": component.bins})
		if err != nil {
			t.Fatal(err)
		}
		files := map[string]string{"package.json": string(manifest), "LICENSE": "official license"}
		for _, bin := range component.bins {
			files[strings.TrimPrefix(bin, "./")] = "official " + component.name + " payload"
		}
		archive := payloadTestTar(t, files)
		digest := sha512.Sum512(archive)
		*component.versionPin, *component.sriPin = component.version, "sha512-"+base64.StdEncoding.EncodeToString(digest[:])
		artifacts["https://registry.npmjs.org/"+component.pkg+"/-/"+filepath.Base(component.pkg)+"-"+component.version+".tgz"] = archive
		root := filepath.Join(cache, "v1", component.name, component.version)
		var bin any = component.bins
		if component.name == "pnpm" {
			bin = map[string]string{"pnpm": "./bin/pnpm.mjs", "pnpx": "./bin/pnpx.mjs"}
		}
		if component.name == "corepack" {
			root = filepath.Join(toolchain, "lib/node_modules/corepack")
		} else if component.name == "yarn" {
			files = map[string]string{"yarn.js": files["bin/yarn.js"]}
			bin = []string{"yarn", "yarnpkg"}
			digest = sha512.Sum512([]byte(files["yarn.js"]))
		}
		for name, data := range files {
			payloadTestWrite(t, root, name, data)
		}
		if component.name != "corepack" {
			meta, err := json.Marshal(map[string]any{"locator": map[string]string{"name": component.name, "reference": component.version}, "bin": bin, "hash": "sha512." + hex.EncodeToString(digest[:])})
			if err != nil {
				t.Fatal(err)
			}
			payloadTestWrite(t, root, ".corepack", string(meta))
		}
	}
	payloadTestWrite(t, cache, "lastKnownGood.json", `{"pnpm":"11.20.0","yarn":"4.18.0"}`)
	history := filepath.Join(cache, "v1/pnpm/9.0.0/project-payload")
	payloadTestWrite(t, cache, "v1/pnpm/9.0.0/project-payload", "historical project version")
	http.DefaultTransport = payloadTestTransport(func(r *http.Request) (*http.Response, error) {
		data, ok := artifacts[r.URL.String()]
		if !ok {
			t.Fatalf("unexpected artifact request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
	})
	assess := func() error { return authenticateManagerPayloadsAt(context.Background(), nil, toolchain, cache) }
	if err := assess(); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(history); err != nil || string(b) != "historical project version" {
		t.Fatal("historical cache changed", err)
	}
	// Same-size tampering must fail despite unchanged package.json/.corepack.
	yarn := filepath.Join(cache, "v1/yarn/4.18.0/yarn.js")
	original, _ := os.ReadFile(yarn)
	tampered := bytes.Repeat([]byte("x"), len(original))
	if err := os.WriteFile(yarn, tampered, 0644); err != nil {
		t.Fatal(err)
	}
	if err := assess(); err == nil || !strings.Contains(err.Error(), "Yarn") || !strings.Contains(err.Error(), "bytes differ") {
		t.Fatal("tampered Yarn was accepted", err)
	}
	if b, _ := os.ReadFile(yarn); !bytes.Equal(b, tampered) {
		t.Fatal("assessment repaired the modified payload")
	}
	payloadTestWrite(t, cache, "v1/yarn/4.18.0/yarn.js", string(original))
	payloadTestWrite(t, cache, "lastKnownGood.json", `{"pnpm":"9.0.0","yarn":"4.18.0"}`)
	if err := assess(); err == nil || !strings.Contains(err.Error(), "active pnpm") {
		t.Fatal("unauthenticated active default was accepted", err)
	}
}

func TestPayloadRejectsUntrustedArchiveBeforeParsing(t *testing.T) {
	digest := sha512.Sum512([]byte("official archive"))
	client := &http.Client{Transport: payloadTestTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("untrusted malformed archive"))}, nil
	})}
	err := withAuthenticatedPayload(context.Background(), client, "https://registry.npmjs.org/pnpm/-/pnpm-11.20.0.tgz", digest[:], sha512.New(), func(*os.File) error {
		t.Fatal("parsed archive before verifying release pin")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatal(err)
	}
}

func TestPayloadRejectsArchivePathsAndLinks(t *testing.T) {
	files := map[string]string{"package.json": `{"name":"pnpm","version":"11.20.0","bin":{"pnpm":"bin/pnpm.js"}}`, "bin/pnpm.js": "payload"}
	for _, header := range []*tar.Header{
		{Name: "package/../escape", Typeflag: tar.TypeReg},
		{Name: "/absolute", Typeflag: tar.TypeReg},
		{Name: "package/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
		{Name: "package/hardlink", Typeflag: tar.TypeLink, Linkname: "package/bin/pnpm.js"},
		{Name: "package/bin/pnpm.js", Typeflag: tar.TypeReg},
	} {
		if _, err := readAuthenticatedNPMPayload(context.Background(), bytes.NewReader(payloadTestTar(t, files, header))); err == nil {
			t.Fatalf("unsafe archive entry accepted: %+v", header)
		}
	}
	pkg, err := readAuthenticatedNPMPayload(context.Background(), bytes.NewReader(payloadTestTar(t, files)))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for name, data := range files {
		payloadTestWrite(t, root, name, data)
	}
	if err := compareAuthenticatedPayload(context.Background(), root, pkg.entries, nil); err != nil {
		t.Fatal(err)
	}
	externalBin := filepath.Join(t.TempDir(), "bin")
	if err := os.Rename(filepath.Join(root, "bin"), externalBin); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(externalBin, filepath.Join(root, "bin")); err != nil {
		t.Fatal(err)
	}
	if err := compareAuthenticatedPayload(context.Background(), root, pkg.entries, nil); err == nil {
		t.Fatal("installed directory symlink accepted")
	}
}

func TestPayloadCorepackMetadataCannotAuthorizeOtherCommands(t *testing.T) {
	good := `{"locator":{"name":"pnpm","reference":"11.20.0"},"bin":{"pnpm":"./bin/pnpm.mjs","pnpx":"./bin/pnpx.mjs"},"hash":"sha512.official"}`
	bins := map[string]string{"pnpm": "./bin/pnpm.mjs", "pnpx": "./bin/pnpx.mjs"}
	if err := validatePayloadCorepackMetadata([]byte(good), "pnpm", "11.20.0", "sha512.official", bins); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		strings.Replace(good, "./bin/pnpm.mjs", "../../external.js", 1),
		strings.Replace(good, "sha512.official", "sha512.forged", 1),
		strings.Replace(good, "11.20.0", "9.0.0", 1),
		strings.Replace(good, `"bin":`, `"Bin":`, 1),
		strings.Replace(good, `"name":`, `"Name":`, 1),
	} {
		if err := validatePayloadCorepackMetadata([]byte(raw), "pnpm", "11.20.0", "sha512.official", bins); err == nil {
			t.Fatalf("unsafe metadata accepted: %s", raw)
		}
	}
}

func TestAuthenticatedBunZIPAndInstalledExecutable(t *testing.T) {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, err := zw.Create("bun-linux-x64/bun")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "official bun executable"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(b.Bytes())
	client := &http.Client{Transport: payloadTestTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(b.Bytes()))}, nil
	})}
	root := t.TempDir()
	payloadTestWrite(t, root, "bun", "official bun executable")
	err = withAuthenticatedPayload(context.Background(), client, "https://github.com/oven-sh/bun/releases/download/bun-v1.3.14/bun-linux-x64.zip", digest[:], sha256.New(), func(f *os.File) error {
		entry, err := readAuthenticatedBunPayload(context.Background(), f, "bun-linux-x64/bun")
		if err != nil {
			return err
		}
		dir, err := openPayloadPath(nil, root, true)
		if err != nil {
			return err
		}
		defer dir.Close()
		return comparePayloadFile(context.Background(), dir, "bun", entry)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPayloadRedirectsStayOnOfficialOrigins(t *testing.T) {
	for _, tc := range []struct {
		initial, redirect string
		allowed           bool
	}{
		{"https://registry.npmjs.org/pnpm/-/pnpm-11.20.0.tgz", "https://registry.npmjs.org/pnpm/-/pnpm-11.20.0.tgz", true},
		{"https://registry.npmjs.org/pnpm/-/pnpm-11.20.0.tgz", "https://github.com/payload", false},
		{"https://github.com/oven-sh/bun/releases/download/bun-v1.3.14/bun-linux-x64.zip", "https://release-assets.githubusercontent.com/signed-asset", true},
		{"https://github.com/oven-sh/bun/releases/download/bun-v1.3.14/bun-linux-x64.zip", "https://github.com/unrelated/repo/releases/download/payload", false},
		{"https://registry.npmjs.org/pnpm/-/pnpm-11.20.0.tgz", "https://registry.npmjs.org.evil.example/payload", false},
		{"https://registry.npmjs.org/pnpm/-/pnpm-11.20.0.tgz", "http://registry.npmjs.org/payload", false},
		{"https://registry.npmjs.org/pnpm/-/pnpm-11.20.0.tgz", "https://user@registry.npmjs.org/payload", false},
	} {
		u, err := url.Parse(tc.redirect)
		if err != nil {
			t.Fatal(err)
		}
		if got := allowedPayloadURL(tc.initial, u); got != tc.allowed {
			t.Fatalf("redirect %s -> %s: %v", tc.initial, u, got)
		}
	}
	u, _ := url.Parse("https://registry.npmjs.org/payload")
	if err := payloadHTTPClient(u.String()).CheckRedirect(&http.Request{URL: u}, make([]*http.Request, 4)); err == nil {
		t.Fatal("unbounded redirects accepted")
	}
}

// Optional, narrow acceptance check against a read-only copy of the existing
// fixture. This does not invoke Multipass, execute payloads or write the copy.
func TestAuthenticatedPayloadCopiedOfficialFixture(t *testing.T) {
	root := os.Getenv("FLUXO_NODE_PAYLOAD_FIXTURE")
	if root == "" {
		t.Skip("set FLUXO_NODE_PAYLOAD_FIXTURE to a copied fixture filesystem root to download and compare pinned official artifacts")
	}
	raw, err := os.ReadFile("../../../.github/release-tool-versions.conf")
	if err != nil {
		t.Fatal(err)
	}
	pins := map[string]*string{
		"COREPACK_VERSION": &PinnedCorepackVersion, "COREPACK_INTEGRITY": &PinnedCorepackIntegrity,
		"PNPM_VERSION": &PinnedPNPMVersion, "PNPM_INTEGRITY": &PinnedPNPMIntegrity,
		"YARN_VERSION": &PinnedYarnVersion, "YARN_INTEGRITY": &PinnedYarnIntegrity,
		"BUN_VERSION": &PinnedBunVersion, "BUN_AMD64_SHA256": &PinnedBunAMD64SHA256,
		"BUN_AMD64_BASELINE_SHA256": &PinnedBunAMD64BaselineSHA256, "BUN_ARM64_SHA256": &PinnedBunARM64SHA256,
	}
	for _, line := range strings.Split(string(raw), "\n") {
		name, value, _ := strings.Cut(line, "=")
		if pin := pins[name]; pin != nil {
			old := *pin
			t.Cleanup(func() { *pin = old })
			*pin = strings.Trim(value, "'\"")
		}
	}
	ctx := context.Background()
	progress := func(message string) { t.Log(message) }
	toolchain := filepath.Join(root, "opt/fluxo/node-toolchain")
	if err := authenticateManagerPayloadsAt(ctx, progress, toolchain, filepath.Join(root, "home/fluxo/.cache/node/corepack")); err != nil {
		t.Fatal(err)
	}
	if err := authenticateBunPayloadAt(ctx, progress, toolchain); err != nil {
		t.Fatal(err)
	}
}
