package nodetoolchain

// These assessments never execute installed code or change the managed tree.
// The caller must hold the toolchain lock and separately establish ownership,
// command links, Node authenticity and the root-owned runtime proof.
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
	"errors"
	"fmt"
	"hash"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	payloadDownloadLimit = 100 << 20
	payloadExpandedLimit = 512 << 20
	payloadFileLimit     = 256 << 20
	payloadMetadataLimit = 1 << 20
	payloadEntryLimit    = 30000
)

type authenticatedPayloadEntry struct {
	directory bool
	size      int64
	sha512    [sha512.Size]byte
}

type authenticatedPackage struct {
	entries map[string]authenticatedPayloadEntry
	name    string
	version string
	bin     map[string]string
}

func authenticateManagerPayloads(ctx context.Context, progress ProgressFunc) error {
	return authenticateManagerPayloadsAt(ctx, progress, managedRoot, managedCorepackHome)
}

func authenticateManagerPayloadsAt(ctx context.Context, progress ProgressFunc, toolchainRoot, corepackHome string) error {
	for _, component := range []struct {
		name, pkg, version, integrity, root string
	}{
		{"Corepack", "corepack", PinnedCorepackVersion, PinnedCorepackIntegrity, filepath.Join(toolchainRoot, "lib/node_modules/corepack")},
		{"pnpm", "pnpm", PinnedPNPMVersion, PinnedPNPMIntegrity, ""},
		{"Yarn", "@yarnpkg/cli-dist", PinnedYarnVersion, PinnedYarnIntegrity, ""},
	} {
		version, err := payloadPinnedVersion(component.version, component.name)
		if err != nil {
			return err
		}
		digest, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(component.integrity, "sha512-"))
		if !strings.HasPrefix(component.integrity, "sha512-") || err != nil || len(digest) != sha512.Size {
			return fmt.Errorf("%s assessment requires a valid pinned SHA-512 npm integrity; update Fluxo release metadata", component.name)
		}
		resource := "https://registry.npmjs.org/" + component.pkg + "/-/" + path.Base(component.pkg) + "-" + version + ".tgz"
		reportProgress(progress, "Authenticating existing %s %s against its pinned official artifact...", component.name, version)
		err = withAuthenticatedPayload(ctx, payloadHTTPClient(resource), resource, digest, sha512.New(), func(f *os.File) error {
			pkg, err := readAuthenticatedNPMPayload(ctx, f)
			if err != nil {
				return err
			}
			if pkg.name != component.pkg || pkg.version != version {
				return errors.New("authenticated archive has an unexpected package name or version")
			}
			if component.name == "Corepack" {
				return compareAuthenticatedPayload(ctx, component.root, pkg.entries, nil)
			}
			name := strings.ToLower(component.name)
			entries, bins, metadataHash := pkg.entries, pkg.bin, "sha512."+hex.EncodeToString(digest)
			if name == "pnpm" {
				// Corepack's pnpm spec exposes these two commands. The npm
				// manifest may additionally publish pn/pnx aliases.
				bins = map[string]string{"pnpm": pkg.bin["pnpm"], "pnpx": pkg.bin["pnpx"]}
				if bins["pnpm"] == "" || bins["pnpx"] == "" {
					return errors.New("authenticated pnpm archive lacks pnpm/pnpx command payloads")
				}
			}
			if name == "yarn" {
				entry, ok := pkg.entries["bin/yarn.js"]
				if !ok || entry.directory || len(pkg.bin) != 2 || strings.TrimPrefix(pkg.bin["yarn"], "./") != "bin/yarn.js" || strings.TrimPrefix(pkg.bin["yarnpkg"], "./") != "bin/yarn.js" {
					return errors.New("authenticated Yarn archive has unexpected command payloads")
				}
				entries = map[string]authenticatedPayloadEntry{"yarn.js": entry}
				bins = nil // Corepack's single-file Yarn format uses a bin-name list.
				metadataHash = "sha512." + hex.EncodeToString(entry.sha512[:])
			}
			root := filepath.Join(corepackHome, "v1", name, version)
			return compareAuthenticatedPayload(ctx, root, entries, func(dir *os.File) ([]byte, error) {
				raw, err := readPayloadMetadata(ctx, dir, ".corepack")
				if err == nil {
					err = validatePayloadCorepackMetadata(raw, name, version, metadataHash, bins)
				}
				return raw, err
			})
		})
		if err != nil {
			return fmt.Errorf("authenticate existing %s payload (retry assessment or explicitly repair the toolchain before updating): %w", component.name, err)
		}
	}
	// Only the active defaults are assessed. Historical/project-selected cache
	// versions are neither authenticated by this result nor removed or rewritten.
	dir, err := openPayloadPath(nil, corepackHome, true)
	if err != nil {
		return fmt.Errorf("authenticate Corepack active defaults: %w", err)
	}
	defer dir.Close()
	raw, err := readPayloadMetadata(ctx, dir, "lastKnownGood.json")
	if err != nil {
		return fmt.Errorf("authenticate Corepack active defaults: %w", err)
	}
	var defaults map[string]string
	if err := json.Unmarshal(raw, &defaults); err != nil {
		return fmt.Errorf("authenticate Corepack lastKnownGood.json: %w", err)
	}
	for name, pinned := range map[string]string{"pnpm": PinnedPNPMVersion, "yarn": PinnedYarnVersion} {
		version, _ := payloadPinnedVersion(pinned, name)
		if defaults[name] != version {
			return fmt.Errorf("Corepack active %s default is not pinned %s; explicitly select the pinned manager before updating", name, version)
		}
	}
	return nil
}

func authenticateBunPayload(ctx context.Context, progress ProgressFunc) error {
	return authenticateBunPayloadAt(ctx, progress, managedRoot)
}

func authenticateBunPayloadAt(ctx context.Context, progress ProgressFunc, toolchainRoot string) error {
	version, err := payloadPinnedVersion(PinnedBunVersion, "Bun")
	if err != nil {
		return err
	}
	asset, err := bunAssetName()
	if err != nil {
		return err
	}
	checksum, err := pinnedBunChecksum(asset)
	if err != nil {
		return err
	}
	digest, _ := hex.DecodeString(checksum)
	resource := "https://github.com/oven-sh/bun/releases/download/bun-v" + version + "/" + asset
	reportProgress(progress, "Authenticating existing Bun %s against its pinned official artifact...", version)
	err = withAuthenticatedPayload(ctx, payloadHTTPClient(resource), resource, digest, sha256.New(), func(f *os.File) error {
		entry, err := readAuthenticatedBunPayload(ctx, f, strings.TrimSuffix(asset, ".zip")+"/bun")
		if err != nil {
			return err
		}
		dir, err := openPayloadPath(nil, filepath.Join(toolchainRoot, "bin"), true)
		if err != nil {
			return err
		}
		defer dir.Close()
		return comparePayloadFile(ctx, dir, "bun", entry)
	})
	if err != nil {
		return fmt.Errorf("authenticate existing Bun payload (retry assessment or explicitly repair the toolchain before updating): %w", err)
	}
	return nil
}

func payloadPinnedVersion(value, component string) (string, error) {
	version, err := validatedPinnedVersion(value, component)
	if err != nil || version == "" || !strictReleaseVersion(version) {
		return "", fmt.Errorf("%s assessment requires an exact pinned version; update Fluxo release metadata", component)
	}
	return version, nil
}

func payloadHTTPClient(resource string) *http.Client {
	return &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || !allowedPayloadURL(resource, req.URL) {
			return errors.New("unapproved payload download redirect")
		}
		return nil
	}}
}

func allowedPayloadURL(resource string, u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return false
	}
	if strings.HasPrefix(resource, "https://registry.npmjs.org/") {
		return u.Host == "registry.npmjs.org"
	}
	if strings.HasPrefix(resource, "https://github.com/oven-sh/bun/releases/download/") {
		return (u.Host == "github.com" && strings.HasPrefix(u.Path, "/oven-sh/bun/releases/download/")) || u.Host == "release-assets.githubusercontent.com" || u.Host == "objects.githubusercontent.com"
	}
	return false
}

// The archive is parsed only after its entire compressed representation has
// matched the release pin. The temporary file is private and always removed.
func withAuthenticatedPayload(ctx context.Context, client *http.Client, resource string, digest []byte, h hash.Hash, assess func(*os.File) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resource, nil)
	if err != nil || !allowedPayloadURL(resource, req.URL) {
		return errors.New("unapproved payload download origin")
	}
	req.Header.Set("User-Agent", "Fluxo-Node-Payload-Assessment")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download pinned official artifact: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("official artifact download returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > payloadDownloadLimit {
		return errors.New("official artifact exceeds download size limit")
	}
	f, err := os.CreateTemp("", "fluxo-authenticated-payload-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(payloadContextReader{ctx, resp.Body}, payloadDownloadLimit+1))
	if err != nil {
		return err
	}
	if n > payloadDownloadLimit {
		return errors.New("official artifact exceeds download size limit")
	}
	if !bytes.Equal(h.Sum(nil), digest) {
		return errors.New("official artifact digest does not match the Fluxo release pin")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return assess(f)
}

type payloadContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r payloadContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func payloadArchiveName(name string) (string, error) {
	name = strings.TrimSuffix(name, "/")
	if name == "" || len(name) > 4096 || strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") || path.Clean(name) != name || strings.Count(name, "/") > 64 || name == "." || name == ".." || strings.HasPrefix(name, "../") {
		return "", fmt.Errorf("unsafe payload archive path %q", name)
	}
	return name, nil
}

// No archive entries are extracted. Links and special files are rejected,
// including entries not selected for Yarn's single-file cache representation.
func readAuthenticatedNPMPayload(ctx context.Context, f io.Reader) (authenticatedPackage, error) {
	pkg := authenticatedPackage{entries: make(map[string]authenticatedPayloadEntry)}
	gz, err := gzip.NewReader(f)
	if err != nil {
		return pkg, err
	}
	defer gz.Close()
	limited := &io.LimitedReader{R: payloadContextReader{ctx, gz}, N: payloadExpandedLimit + 1}
	tr := tar.NewReader(limited)
	seen := make(map[string]bool)
	var manifest []byte
	var total int64
	for count := 0; ; count++ {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return pkg, err
		}
		if count >= payloadEntryLimit || header.Size < 0 || header.Size > payloadFileLimit || header.Size > payloadExpandedLimit-total {
			return pkg, errors.New("npm archive exceeds payload assessment limits")
		}
		total += header.Size
		name, err := payloadArchiveName(header.Name)
		if err != nil {
			return pkg, err
		}
		if seen[name] {
			return pkg, fmt.Errorf("duplicate npm archive entry %s", name)
		}
		seen[name] = true
		if header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return pkg, fmt.Errorf("unsupported npm archive entry %s", name)
		}
		if name == "package" && header.Typeflag == tar.TypeDir {
			continue
		}
		if !strings.HasPrefix(name, "package/") {
			return pkg, fmt.Errorf("npm archive entry outside package/: %s", name)
		}
		name = strings.TrimPrefix(name, "package/")
		entry := authenticatedPayloadEntry{directory: header.Typeflag == tar.TypeDir, size: header.Size}
		if !entry.directory {
			h := sha512.New()
			var dst io.Writer = h
			var b bytes.Buffer
			if name == "package.json" {
				if header.Size > payloadMetadataLimit {
					return pkg, errors.New("npm package.json exceeds metadata size limit")
				}
				dst = io.MultiWriter(h, &b)
			}
			if _, err := io.Copy(dst, tr); err != nil {
				return pkg, err
			}
			copy(entry.sha512[:], h.Sum(nil))
			if name == "package.json" {
				manifest = b.Bytes()
			}
		}
		if old, ok := pkg.entries[name]; ok && (!old.directory || !entry.directory) {
			return pkg, fmt.Errorf("conflicting npm archive entry %s", name)
		}
		pkg.entries[name] = entry
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if old, ok := pkg.entries[parent]; ok && !old.directory {
				return pkg, fmt.Errorf("npm archive file used as directory: %s", parent)
			}
			pkg.entries[parent] = authenticatedPayloadEntry{directory: true}
		}
		if len(pkg.entries) > payloadEntryLimit {
			return pkg, errors.New("npm archive has too many payload paths")
		}
	}
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return pkg, err
	}
	if limited.N == 0 {
		return pkg, errors.New("npm archive exceeds expanded size limit")
	}
	var info struct {
		Name    string            `json:"name"`
		Version string            `json:"version"`
		Bin     map[string]string `json:"bin"`
	}
	if err := json.Unmarshal(manifest, &info); err != nil || len(info.Bin) == 0 {
		return pkg, errors.New("authenticated npm archive has no valid package.json command map")
	}
	for _, bin := range info.Bin {
		name, err := payloadArchiveName(strings.TrimPrefix(bin, "./"))
		entry, ok := pkg.entries[name]
		if err != nil || !ok || entry.directory {
			return pkg, errors.New("authenticated npm command map references a missing or unsafe payload")
		}
	}
	pkg.name, pkg.version, pkg.bin = info.Name, info.Version, info.Bin
	return pkg, nil
}

func readAuthenticatedBunPayload(ctx context.Context, f *os.File, target string) (authenticatedPayloadEntry, error) {
	var result authenticatedPayloadEntry
	info, err := f.Stat()
	if err != nil {
		return result, err
	}
	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return result, err
	}
	if len(zr.File) > payloadEntryLimit {
		return result, errors.New("Bun archive has too many entries")
	}
	seen := make(map[string]bool)
	var selected *zip.File
	var total uint64
	for _, file := range zr.File {
		name, err := payloadArchiveName(file.Name)
		if err != nil {
			return result, err
		}
		if seen[name] || (!file.Mode().IsRegular() && !file.Mode().IsDir()) {
			return result, fmt.Errorf("duplicate or unsupported Bun archive entry %s", name)
		}
		seen[name] = true
		if file.UncompressedSize64 > payloadFileLimit || file.UncompressedSize64 > payloadExpandedLimit-total {
			return result, errors.New("Bun archive exceeds expanded size limit")
		}
		total += file.UncompressedSize64
		if name == target && file.Mode().IsRegular() {
			selected = file
		}
	}
	if selected == nil {
		return result, errors.New("authenticated Bun archive lacks the expected executable")
	}
	r, err := selected.Open()
	if err != nil {
		return result, err
	}
	defer r.Close()
	h := sha512.New()
	n, err := io.Copy(h, io.LimitReader(payloadContextReader{ctx, r}, int64(selected.UncompressedSize64)+1))
	if err != nil || uint64(n) != selected.UncompressedSize64 {
		return result, fmt.Errorf("read Bun archive executable: size %d, error %v", n, err)
	}
	result.size = n
	copy(result.sha512[:], h.Sum(nil))
	return result, nil
}

// Descriptor-relative, no-follow opens avoid following installed symlinks or
// blocking on a FIFO substituted for a payload file. Parent ownership is the
// caller's responsibility; every path below the selected root is opened here.
func openPayloadPath(parent *os.File, name string, directory bool) (*os.File, error) {
	fd := unix.AT_FDCWD
	if parent != nil {
		fd = int(parent.Fd())
	}
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if directory {
		flags |= unix.O_DIRECTORY
	}
	opened, err := unix.Openat(fd, name, flags, 0)
	if err != nil {
		return nil, fmt.Errorf("read existing payload %s: %w", name, err)
	}
	f := os.NewFile(uintptr(opened), name)
	info, err := f.Stat()
	if err != nil || (directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		f.Close()
		return nil, fmt.Errorf("existing payload %s is not the expected regular file/directory", name)
	}
	return f, nil
}

func readPayloadMetadata(ctx context.Context, dir *os.File, name string) ([]byte, error) {
	f, err := openPayloadPath(dir, name, false)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(payloadContextReader{ctx, f}, payloadMetadataLimit+1))
	if len(raw) > payloadMetadataLimit {
		return nil, fmt.Errorf("%s exceeds metadata size limit", name)
	}
	return raw, err
}

func validatePayloadCorepackMetadata(raw []byte, name, version, expectedHash string, bins map[string]string) error {
	// JSON.parse in Corepack is case sensitive; encoding/json's struct-field
	// matching is not. Require the actual schema keys before decoding fields.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 3 || fields["locator"] == nil || fields["bin"] == nil || fields["hash"] == nil {
		return fmt.Errorf("invalid %s .corepack metadata schema", name)
	}
	var locator map[string]json.RawMessage
	if err := json.Unmarshal(fields["locator"], &locator); err != nil || len(locator) != 2 || locator["name"] == nil || locator["reference"] == nil {
		return fmt.Errorf("invalid %s .corepack locator schema", name)
	}
	var meta struct {
		Locator struct {
			Name      string `json:"name"`
			Reference string `json:"reference"`
		} `json:"locator"`
		Bin  json.RawMessage `json:"bin"`
		Hash string          `json:"hash"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return fmt.Errorf("invalid %s .corepack metadata: %w", name, err)
	}
	if meta.Locator.Name != name || meta.Locator.Reference != version || meta.Hash != expectedHash {
		return fmt.Errorf("%s .corepack locator/hash does not match its authenticated pinned payload", name)
	}
	if bins != nil {
		var actual map[string]string
		if err := json.Unmarshal(meta.Bin, &actual); err != nil || len(actual) != len(bins) {
			return fmt.Errorf("%s .corepack command map does not match authenticated package.json", name)
		}
		for command, target := range bins {
			if strings.TrimPrefix(actual[command], "./") != strings.TrimPrefix(target, "./") {
				return fmt.Errorf("%s .corepack command %s does not match authenticated package.json", name, command)
			}
		}
	} else {
		var actual []string
		if err := json.Unmarshal(meta.Bin, &actual); err != nil || len(actual) != 2 || !((actual[0] == "yarn" && actual[1] == "yarnpkg") || (actual[0] == "yarnpkg" && actual[1] == "yarn")) {
			return errors.New("Yarn .corepack command list must contain yarn and yarnpkg")
		}
	}
	return nil
}

func compareAuthenticatedPayload(ctx context.Context, root string, entries map[string]authenticatedPayloadEntry, metadata func(*os.File) ([]byte, error)) error {
	dir, err := openPayloadPath(nil, root, true)
	if err != nil {
		return err
	}
	defer dir.Close()
	if metadata != nil {
		raw, err := metadata(dir)
		if err != nil {
			return err
		}
		entries = maps.Clone(entries)
		entries[".corepack"] = authenticatedPayloadEntry{size: int64(len(raw)), sha512: sha512.Sum512(raw)}
	}
	seen := make(map[string]bool)
	var walk func(*os.File, string) error
	walk = func(parent *os.File, prefix string) error {
		for {
			children, err := parent.ReadDir(128)
			if err != nil && err != io.EOF {
				return err
			}
			for _, child := range children {
				if err := ctx.Err(); err != nil {
					return err
				}
				name := path.Join(prefix, child.Name())
				expected, ok := entries[name]
				if !ok || seen[name] {
					return fmt.Errorf("unexpected existing payload path %s", filepath.Join(root, name))
				}
				seen[name] = true
				if expected.directory {
					dir, err := openPayloadPath(parent, child.Name(), true)
					if err != nil {
						return err
					}
					err = walk(dir, name)
					dir.Close()
					if err != nil {
						return err
					}
				} else if err := comparePayloadFile(ctx, parent, child.Name(), expected); err != nil {
					return fmt.Errorf("%s: %w", filepath.Join(root, name), err)
				}
			}
			if err == io.EOF {
				return nil
			}
		}
	}
	if err := walk(dir, ""); err != nil {
		return err
	}
	for name := range entries {
		if !seen[name] {
			return fmt.Errorf("missing authenticated payload path %s", filepath.Join(root, name))
		}
	}
	return nil
}

func comparePayloadFile(ctx context.Context, parent *os.File, name string, expected authenticatedPayloadEntry) error {
	f, err := openPayloadPath(parent, name, false)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() != expected.size {
		return errors.New("existing file size differs from the authenticated release")
	}
	h := sha512.New()
	n, err := io.Copy(h, io.LimitReader(payloadContextReader{ctx, f}, expected.size+1))
	if err != nil {
		return err
	}
	if n != expected.size || !bytes.Equal(h.Sum(nil), expected.sha512[:]) {
		return errors.New("existing file bytes differ from the authenticated release")
	}
	return nil
}
