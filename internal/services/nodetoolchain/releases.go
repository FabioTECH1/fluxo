package nodetoolchain

// Node updates deliberately use a separate, exact release path. Toolchain repair
// must not select "latest LTS" while applying an administrator's reviewed plan.
import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/syscmd"
)

//go:embed node-release-keys.kbx
var nodeReleaseKeys []byte

//go:embed node-release-keys-source.txt
var nodeReleaseKeysSource string

type Release struct {
	Version        string `json:"version"`
	NPM            string `json:"npm"`
	Architecture   string `json:"architecture"`
	Filename       string `json:"filename"`
	SHA256         string `json:"sha256"`
	ManifestSHA256 string `json:"manifest_sha256"`
	Signer         string `json:"signer"`
	TrustRevision  string `json:"trust_revision"`
	TreeSHA256     string `json:"tree_sha256,omitempty"`
}

type OwnedRuntime struct {
	Release  Release           `json:"release"`
	Root     string            `json:"root"`
	Managers map[string]string `json:"managers"`
}

func TrustRevision() string                           { h := sha256.Sum256(nodeReleaseKeys); return hex.EncodeToString(h[:]) }
func NodeRoot() string                                { return managedNodeRoot }
func CurrentNodePath() string                         { return filepath.Join(managedNodeRoot, "current", "bin", "node") }
func LockRuntime(ctx context.Context) (func(), error) { return acquireToolchainLock(ctx) }

// Support policy reviewed against nodejs/Release schedule.json on 2026-10-22.
// A stale policy blocks discovery instead of continuing an unmaintained line.
func SupportedMajor(version string, now time.Time) error {
	v, ok := parseVersion(version)
	if !ok {
		return errors.New("Invalid installed Node.js version")
	}
	if now.UTC().Format("2006-01-02") > "2027-04-01" {
		return errors.New("Node.js support policy needs a Fluxo update before checking releases")
	}
	policy := map[int][2]string{22: {"2024-10-29", "2027-04-30"}, 24: {"2025-10-28", "2028-04-30"}, 26: {"2026-10-28", "2029-04-30"}}
	dates, ok := policy[v[0]]
	day := now.UTC().Format("2006-01-02")
	if !ok || day < dates[0] || day >= dates[1] {
		return errors.New("This Node.js major is not a supported LTS line; choose a major upgrade separately")
	}
	return nil
}

func strictReleaseVersion(value string) bool {
	v, ok := parseVersion(value)
	return ok && strings.TrimPrefix(value, "v") == fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
}

type releaseIndexEntry struct {
	Version string          `json:"version"`
	NPM     string          `json:"npm"`
	LTS     json.RawMessage `json:"lts"`
	Files   []string        `json:"files"`
}

func SelectUpdate(index []byte, installed, arch string, now time.Time) (string, string, error) {
	if err := SupportedMajor(installed, now); err != nil {
		return "", "", err
	}
	current, _ := parseVersion(installed)
	var entries []releaseIndexEntry
	if err := json.Unmarshal(index, &entries); err != nil {
		return "", "", errors.New("Invalid Node.js release index")
	}
	selected, npm := "", ""
	for _, e := range entries {
		v, ok := parseVersion(e.Version)
		var lts string
		if !ok || !strictReleaseVersion(e.Version) || v[0] != current[0] || json.Unmarshal(e.LTS, &lts) != nil || lts == "" || !containsString(e.Files, "linux-"+arch) {
			continue
		}
		version := strings.TrimPrefix(e.Version, "v")
		if version == strings.TrimPrefix(installed, "v") || !versionAtLeast(version, installed) {
			continue
		}
		if selected == "" || versionAtLeast(version, selected) {
			selected, npm = version, e.NPM
		}
	}
	return selected, npm, nil
}

// All update URLs are derived from fixed official origins. Redirects cannot
// leave nodejs.org, even when an upstream response supplies a Location header.
func fetchNode(ctx context.Context, resource string, limit int64, dst io.Writer) error {
	if !strings.HasPrefix(resource, "https://nodejs.org/dist/") {
		return errors.New("Invalid Node.js download origin")
	}
	client := &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 || r.URL.Scheme != "https" || r.URL.Host != "nodejs.org" || r.URL.User != nil {
			return errors.New("Unapproved Node.js redirect")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, "GET", resource, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Fluxo-Node-Updates")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Node.js download returned HTTP %d", resp.StatusCode)
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return errors.New("Node.js download exceeded its size limit")
	}
	return nil
}
func readNode(ctx context.Context, url string, limit int64) ([]byte, error) {
	var b strings.Builder
	err := fetchNode(ctx, url, limit, &b)
	return []byte(b.String()), err
}
func FindUpdate(ctx context.Context, installed string) (Release, error) {
	arch, err := nodeArchitecture()
	if err != nil {
		return Release{}, err
	}
	raw, err := readNode(ctx, "https://nodejs.org/dist/index.json", 5<<20)
	if err != nil {
		return Release{}, err
	}
	v, npm, err := SelectUpdate(raw, installed, arch, time.Now())
	if err != nil || v == "" {
		return Release{}, err
	}
	return AuthenticateRelease(ctx, v, npm)
}

func AuthenticateRelease(ctx context.Context, version, npm string) (Release, error) {
	version = strings.TrimPrefix(version, "v")
	if !strictReleaseVersion(version) {
		return Release{}, errors.New("Invalid Node.js release")
	}
	arch, err := nodeArchitecture()
	if err != nil {
		return Release{}, err
	}
	filename := "node-v" + version + "-linux-" + arch + ".tar.gz"
	signed, err := readNode(ctx, "https://nodejs.org/dist/v"+version+"/SHASUMS256.txt.asc", 2<<20)
	if err != nil {
		return Release{}, err
	}
	sums, signer, err := verifyNodeManifest(ctx, signed, nodeReleaseKeys)
	if err != nil {
		return Release{}, err
	}
	checksum, err := exactChecksum(string(sums), filename)
	if err != nil {
		return Release{}, err
	}
	h := sha256.Sum256(signed)
	return Release{Version: version, NPM: npm, Architecture: arch, Filename: filename, SHA256: checksum, ManifestSHA256: hex.EncodeToString(h[:]), Signer: signer, TrustRevision: TrustRevision()}, nil
}
func verifyNodeManifest(ctx context.Context, signed, keys []byte) ([]byte, string, error) {
	if _, err := os.Stat("/usr/bin/gpgv"); err != nil {
		return nil, "", errors.New("gpgv is required to verify Node.js releases; provision it with the current Fluxo installer")
	}
	dir, err := os.MkdirTemp("", "fluxo-node-signature-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(dir)
	for name, data := range map[string][]byte{"keys.kbx": keys, "manifest.asc": signed} {
		if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			return nil, "", err
		}
	}
	out, err := syscmd.Run(ctx, 15*time.Second, "/usr/bin/gpgv", "--homedir", dir, "--keyring", filepath.Join(dir, "keys.kbx"), "--status-fd", "1", "--output", filepath.Join(dir, "sums"), filepath.Join(dir, "manifest.asc"))
	if err != nil {
		return nil, "", errors.New("Node.js release signature is untrusted or invalid; update Fluxo trust data before retrying")
	}
	for _, bad := range []string{"EXPKEYSIG", "EXPSIG", "REVKEYSIG", "KEYEXPIRED", "SIGEXPIRED", "BADSIG", "ERRSIG"} {
		if strings.Contains(out, bad) {
			return nil, "", errors.New("Node.js release signing key or signature is no longer valid")
		}
	}
	signer := ""
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 5 && f[0] == "[GNUPG:]" && f[1] == "VALIDSIG" {
			if signer != "" {
				return nil, "", errors.New("Ambiguous Node.js signature")
			}
			signer = f[2]
			stamp, e := strconv.ParseInt(f[4], 10, 64)
			if e != nil || stamp > time.Now().Add(5*time.Minute).Unix() {
				return nil, "", errors.New("Invalid Node.js signature time")
			}
		}
	}
	if len(signer) != 40 && len(signer) != 64 {
		return nil, "", errors.New("Node.js manifest has no verified release signer")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "sums"))
	return raw, signer, err
}
func exactChecksum(sums, name string) (string, error) {
	result := ""
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			if result != "" {
				return "", errors.New("Duplicate Node.js archive checksum")
			}
			h, err := validatedPinnedChecksum(f[0], "Node.js")
			if err != nil {
				return "", err
			}
			result = h
		}
	}
	if result == "" {
		return "", errors.New("Signed Node.js manifest does not contain the expected architecture archive")
	}
	return result, nil
}

func InspectOwnedNode(ctx context.Context) (OwnedRuntime, error) {
	var result OwnedRuntime
	link := filepath.Join(managedNodeRoot, "current")
	target, err := os.Readlink(link)
	if err != nil {
		return result, errors.New("Only a Fluxo-managed Node.js installation can be updated")
	}
	version := strings.TrimPrefix(filepath.Base(target), "v")
	if !versionPattern.MatchString(version) || target != filepath.Join(managedNodeRoot, "v"+version) {
		return result, errors.New("Unrecognized managed Node.js release link")
	}
	if err = validateOwnedRoots(filepath.Dir(managedNodeRoot), []string{managedNodeRoot}, 0, 0, true); err != nil {
		return result, err
	}
	for _, name := range []string{"node", "npm", "npx"} {
		p := filepath.Join("/usr/local/bin", name)
		t, e := os.Readlink(p)
		if e != nil || t != filepath.Join(link, "bin", name) {
			return result, fmt.Errorf("%s is managed outside Fluxo or shadows the managed runtime", name)
		}
		resolved, e := exec.LookPath(name)
		if e != nil || resolved != p {
			return result, fmt.Errorf("%s is shadowed in Fluxo's executable search path", name)
		}
	}
	if !versionsEqual(commandVersion(ctx, "node"), version) {
		return result, errors.New("Managed Node.js executable does not match its release identity")
	}
	h, err := TreeDigest(target)
	if err != nil {
		return result, err
	}
	npm := commandVersion(ctx, "npm")
	result = OwnedRuntime{Root: target, Release: Release{Version: version, NPM: strings.TrimPrefix(npm, "v"), TreeSHA256: h}, Managers: map[string]string{}}
	for _, name := range []string{"corepack", "pnpm", "yarn", "bun"} {
		v := commandVersion(ctx, name)
		if name == "pnpm" || name == "yarn" {
			v = packageManagerVersion(ctx, name)
		}
		result.Managers[name] = v
	}
	return result, nil
}

// TreeDigest binds contents, permissions and relative symlink targets. It
// excludes no application-controlled metadata and never follows a symlink.
func TreeDigest(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), info.Mode())
		if info.Mode()&os.ModeSymlink != 0 {
			t, e := os.Readlink(p)
			if e != nil {
				return e
			}
			fmt.Fprint(h, t)
		} else if info.Mode().IsRegular() {
			f, e := os.Open(p)
			if e != nil {
				return e
			}
			_, e = io.Copy(h, f)
			f.Close()
			if e != nil {
				return e
			}
		} else if !info.IsDir() {
			return errors.New("Unsupported file in Node.js release")
		}
		fmt.Fprint(h, "\x00")
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
}

func validRelease(r Release) bool {
	arch, e := nodeArchitecture()
	_, h := parseVersion(r.Version)
	_, hashErr := validatedPinnedChecksum(r.SHA256, "Node.js")
	return e == nil && h && r.Architecture == arch && r.Filename == "node-v"+r.Version+"-linux-"+arch+".tar.gz" && hashErr == nil && r.TrustRevision == TrustRevision() && r.ManifestSHA256 != "" && r.Signer != ""
}
func StageRelease(ctx context.Context, r Release, progress ProgressFunc) (Release, error) {
	if !validRelease(r) {
		return r, errors.New("Invalid or obsolete authenticated Node.js release")
	}
	for _, p := range []string{os.TempDir(), filepath.Dir(managedNodeRoot)} {
		var st syscall.Statfs_t
		if err := syscall.Statfs(p, &st); err != nil {
			return r, err
		}
		if uint64(st.Bavail)*uint64(st.Bsize) < 768<<20 || st.Ffree < 5000 {
			return r, errors.New("Insufficient disk space or inodes to stage and retain a Node.js release safely")
		}
	}
	reportProgress(progress, "Downloading and verifying Node.js %s", r.Version)
	dir, err := os.MkdirTemp("", "fluxo-node-archive-")
	if err != nil {
		return r, err
	}
	defer os.RemoveAll(dir)
	archive := filepath.Join(dir, r.Filename)
	f, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return r, err
	}
	err = fetchNode(ctx, "https://nodejs.org/dist/v"+r.Version+"/"+r.Filename, 160<<20, f)
	closeErr := f.Close()
	if err != nil {
		return r, err
	}
	if closeErr != nil {
		return r, closeErr
	}
	if err = verifyFileChecksum(archive, r.SHA256); err != nil {
		return r, err
	}
	if err = os.MkdirAll(managedNodeRoot, 0755); err != nil {
		return r, err
	}
	stage, err := os.MkdirTemp(managedNodeRoot, ".update-stage-")
	if err != nil {
		return r, err
	}
	defer os.RemoveAll(stage)
	if err = os.Chmod(stage, 0755); err != nil {
		return r, err
	}
	rootName := strings.TrimSuffix(r.Filename, ".tar.gz")
	if err = extractNodeArchive(archive, stage, rootName); err != nil {
		return r, err
	}
	root := filepath.Join(stage, rootName)
	version := commandVersionAtPath(ctx, filepath.Join(root, "bin", "node"))
	if !versionsEqual(version, r.Version) {
		return r, errors.New("Downloaded Node.js version does not match the approved release")
	}
	data, err := os.ReadFile(filepath.Join(root, "lib", "node_modules", "npm", "package.json"))
	if err != nil {
		return r, err
	}
	var npm struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &npm) != nil || npm.Version == "" || r.NPM != "" && r.NPM != npm.Version {
		return r, errors.New("Bundled npm does not match the approved release")
	}
	r.NPM = npm.Version
	r.TreeSHA256, err = TreeDigest(root)
	if err != nil {
		return r, err
	}
	destination := filepath.Join(managedNodeRoot, "v"+r.Version)
	if _, err = os.Lstat(destination); err == nil {
		actual, e := TreeDigest(destination)
		if e != nil || actual != r.TreeSHA256 {
			return r, errors.New("An existing Node.js release tree differs from the authenticated archive; manual repair is required")
		}
		return r, nil
	} else if !os.IsNotExist(err) {
		return r, err
	}
	if _, err = syscmd.Run(ctx, 30*time.Second, "/usr/bin/sync", "-f", stage); err != nil {
		return r, err
	}
	if err = os.Rename(root, destination); err != nil {
		return r, err
	}
	return r, syncNodeDirectory(managedNodeRoot)
}
func extractNodeArchive(archive, destination, expected string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(io.LimitReader(gz, 1<<30))
	seen := map[string]bool{}
	count := 0
	for {
		header, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		count++
		if count > 100000 {
			return errors.New("Too many Node.js archive entries")
		}
		name := strings.TrimSuffix(header.Name, "/")
		clean := path.Clean(name)
		if name != clean || path.IsAbs(name) || clean != expected && !strings.HasPrefix(clean, expected+"/") || seen[clean] {
			return errors.New("Unsafe or duplicate Node.js archive entry")
		}
		seen[clean] = true
		p := filepath.Join(destination, filepath.FromSlash(clean))
		for parent := filepath.Dir(p); parent != destination; parent = filepath.Dir(parent) {
			if !pathWithinRoot(destination, parent) {
				return errors.New("Unsafe archive parent")
			}
			if i, e := os.Lstat(parent); e == nil && !i.IsDir() {
				return errors.New("Node.js archive traverses a link")
			}
		}
		switch header.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(p, 0755)
		case tar.TypeReg:
			if header.Size < 0 || header.Size > 300<<20 {
				return errors.New("Oversized Node.js archive file")
			}
			if err = os.MkdirAll(filepath.Dir(p), 0755); err != nil {
				return err
			}
			var out *os.File
			out, err = os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644|os.FileMode(header.Mode)&0111)
			if err == nil {
				_, err = io.Copy(out, tr)
				e = out.Close()
				if err == nil {
					err = e
				}
			}
		case tar.TypeSymlink:
			target := path.Clean(path.Join(path.Dir(clean), header.Linkname))
			if path.IsAbs(header.Linkname) || target != expected && !strings.HasPrefix(target, expected+"/") {
				return errors.New("Unsafe Node.js archive symlink")
			}
			if err = os.MkdirAll(filepath.Dir(p), 0755); err == nil {
				err = os.Symlink(header.Linkname, p)
			}
		default:
			return errors.New("Unsupported Node.js archive entry type")
		}
		if err != nil {
			return err
		}
	}
	if !seen[expected+"/bin/node"] || !seen[expected+"/lib/node_modules/npm/package.json"] {
		return errors.New("Incomplete Node.js release archive")
	}
	return nil
}
func syncNodeDirectory(dir string) error {
	f, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func ActivateRelease(r Release) error {
	if !validRelease(r) || r.TreeSHA256 == "" {
		return errors.New("Invalid retained Node.js release")
	}
	root := filepath.Join(managedNodeRoot, "v"+r.Version)
	hash, err := TreeDigest(root)
	if err != nil || hash != r.TreeSHA256 {
		return errors.New("Retained Node.js release has changed")
	}
	if err = replaceManagedSymlink(filepath.Join(managedNodeRoot, "current"), root); err != nil {
		return err
	}
	return syncNodeDirectory(managedNodeRoot)
}
func ApproveRelease(r Release) error {
	if !validRelease(r) || r.TreeSHA256 == "" {
		return errors.New("Invalid Node.js approval")
	}
	state, err := loadManagedState()
	if err != nil {
		return err
	}
	state.ApprovedNode = &r
	return saveManagedState(state)
}
func ValidateManagers(ctx context.Context, expected map[string]string) error {
	names := make([]string, 0, len(expected))
	for n := range expected {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		v := commandVersion(ctx, name)
		if name == "pnpm" || name == "yarn" {
			v = packageManagerVersion(ctx, name)
		}
		if v == "" || v != expected[name] {
			return fmt.Errorf("%s changed or cannot run with this Node.js release", name)
		}
	}
	return nil
}
