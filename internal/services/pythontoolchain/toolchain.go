package pythontoolchain

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"fluxo/internal/syscmd"
)

const (
	MinimumPythonVersion = "3.10.0"
	managedRoot          = "/opt/fluxo/python-toolchain"
	managedStatePath     = "/var/lib/fluxo/python-toolchain.json"
	defaultUVVersion     = "0.12.9"
	defaultUVAMD64SHA256 = "ec7a99cd05e0cd7f80243f135ce1361c76835cb0ee60055d14d20eba8eba1460"
	defaultUVARM64SHA256 = "c36fe17937ff6bd16dc42fc13854b5465999fcab2efe0af559381e945e3c6001"
)

var (
	installMu           sync.Mutex
	semanticVersion     = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)(?:\.([0-9]+))?(?:[-+][0-9A-Za-z.-]+)?$`)
	PinnedUVVersion     = defaultUVVersion
	PinnedUVAMD64SHA256 = defaultUVAMD64SHA256
	PinnedUVARM64SHA256 = defaultUVARM64SHA256
)

type Status struct {
	Installed            bool     `json:"installed"`
	Managed              bool     `json:"managed"`
	ToolchainReady       bool     `json:"toolchain_ready"`
	PythonCompatible     bool     `json:"python_compatible"`
	MinimumPythonVersion string   `json:"minimum_python_version"`
	Binary               string   `json:"binary"`
	Version              string   `json:"version"`
	Venv                 bool     `json:"venv"`
	Pip                  string   `json:"pip"`
	UV                   string   `json:"uv"`
	Missing              []string `json:"missing"`
}

type managedState struct {
	UVVersion string `json:"uv_version"`
}

func Inspect(ctx context.Context) Status {
	status := Status{MinimumPythonVersion: MinimumPythonVersion}
	status.Binary, _ = exec.LookPath("python3")
	status.Version = pythonVersion(ctx, status.Binary)
	status.Installed = status.Binary != "" && status.Version != ""
	status.PythonCompatible = status.Installed && versionAtLeast(status.Version, MinimumPythonVersion)
	if status.Installed {
		venvCheck := exec.CommandContext(ctx, status.Binary, "-c", "import importlib.util, venv; raise SystemExit(0 if importlib.util.find_spec('ensurepip') else 1)")
		status.Venv = venvCheck.Run() == nil
		status.Pip = moduleVersion(ctx, status.Binary, "pip")
	}
	status.UV = commandVersion(ctx, "uv")
	status.Managed = managedUVInstalled()

	if !status.Installed {
		status.Missing = append(status.Missing, "Python 3")
	} else if !status.PythonCompatible {
		status.Missing = append(status.Missing, "Python "+MinimumPythonVersion+" or newer")
	}
	if !status.Venv {
		status.Missing = append(status.Missing, "venv and ensurepip")
	}
	if status.UV == "" {
		status.Missing = append(status.Missing, "uv")
	}
	status.ToolchainReady = len(status.Missing) == 0
	return status
}

func Install(ctx context.Context) (Status, error) {
	installMu.Lock()
	defer installMu.Unlock()

	if _, err := user.Lookup("fluxo"); err != nil {
		return Inspect(ctx), fmt.Errorf("the fluxo system user must exist before installing Python application support: %w", err)
	}
	packages := []string{"python3", "python3-venv", "python3-dev", "build-essential", "pkg-config", "ca-certificates"}
	if out, err := syscmd.Run(ctx, 5*time.Minute, "apt-get", "update"); err != nil {
		return Inspect(ctx), fmt.Errorf("update package metadata: %s %w", out, err)
	}
	args := append([]string{"install", "-y"}, packages...)
	if out, err := syscmd.Run(ctx, 10*time.Minute, "apt-get", args...); err != nil {
		return Inspect(ctx), fmt.Errorf("install Python prerequisites: %s %w", out, err)
	}
	if err := installUV(ctx); err != nil {
		return Inspect(ctx), err
	}
	if err := validateVenvAsFluxo(ctx); err != nil {
		return Inspect(ctx), err
	}
	status := Inspect(ctx)
	if !status.ToolchainReady {
		return status, fmt.Errorf("Python application support remains incomplete: %s", strings.Join(status.Missing, ", "))
	}
	return status, nil
}

func RemoveManagedTools(ctx context.Context) error {
	installMu.Lock()
	defer installMu.Unlock()
	for _, name := range []string{"uv", "uvx"} {
		path := filepath.Join("/usr/local/bin", name)
		target, err := filepath.EvalSymlinks(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !pathWithin(target, managedRoot) {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.RemoveAll(managedRoot); err != nil {
		return err
	}
	if err := os.Remove(managedStatePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func installUV(ctx context.Context) error {
	version := strings.TrimPrefix(strings.TrimSpace(PinnedUVVersion), "v")
	if version == "" {
		return fmt.Errorf("uv release version is not configured")
	}
	arch := ""
	wantHash := ""
	switch runtime.GOARCH {
	case "amd64":
		arch = "x86_64"
		wantHash = PinnedUVAMD64SHA256
	case "arm64":
		arch = "aarch64"
		wantHash = PinnedUVARM64SHA256
	default:
		return fmt.Errorf("uv is not supported on architecture %s", runtime.GOARCH)
	}
	if len(wantHash) != 64 {
		return fmt.Errorf("uv checksum is not configured for %s", runtime.GOARCH)
	}
	archiveName := "uv-" + arch + "-unknown-linux-gnu.tar.gz"
	url := "https://github.com/astral-sh/uv/releases/download/" + version + "/" + archiveName
	for _, name := range []string{"uv", "uvx"} {
		link := filepath.Join("/usr/local/bin", name)
		current, err := filepath.EvalSymlinks(link)
		if err == nil && !pathWithin(current, managedRoot) {
			return fmt.Errorf("%s is managed outside Fluxo at %s", name, current)
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	tempDir, err := os.MkdirTemp("", "fluxo-uv-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	archivePath := filepath.Join(tempDir, archiveName)
	if err := download(ctx, url, archivePath); err != nil {
		return fmt.Errorf("download uv %s: %w", version, err)
	}
	if err := verifySHA256(archivePath, wantHash); err != nil {
		return fmt.Errorf("verify uv %s: %w", version, err)
	}
	extractDir := filepath.Join(tempDir, "extract")
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return err
	}
	if err := extractUVArchive(archivePath, extractDir); err != nil {
		return err
	}
	releaseDir := filepath.Join(managedRoot, "uv-"+version)
	if err := os.MkdirAll(releaseDir, 0755); err != nil {
		return err
	}
	for _, name := range []string{"uv", "uvx"} {
		source := filepath.Join(extractDir, name)
		target := filepath.Join(releaseDir, name)
		data, err := os.ReadFile(source)
		if err != nil {
			return fmt.Errorf("read extracted %s: %w", name, err)
		}
		if err := os.WriteFile(target, data, 0755); err != nil {
			return err
		}
		link := filepath.Join("/usr/local/bin", name)
		tempLink := link + ".fluxo-new"
		_ = os.Remove(tempLink)
		if err := os.Symlink(target, tempLink); err != nil {
			return err
		}
		if err := os.Rename(tempLink, link); err != nil {
			_ = os.Remove(tempLink)
			return err
		}
	}
	state, _ := json.Marshal(managedState{UVVersion: version})
	if err := os.MkdirAll(filepath.Dir(managedStatePath), 0750); err != nil {
		return err
	}
	if err := os.WriteFile(managedStatePath, append(state, '\n'), 0644); err != nil {
		return err
	}
	return nil
}

func validateVenvAsFluxo(ctx context.Context) error {
	account, err := user.Lookup("fluxo")
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return err
	}
	tempDir, err := os.MkdirTemp("", "fluxo-python-check-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	if err := os.Chown(tempDir, uid, gid); err != nil {
		return err
	}
	if out, err := syscmd.RunAsUserInDir(ctx, 2*time.Minute, "fluxo", tempDir, "python3", "-m", "venv", ".venv"); err != nil {
		return fmt.Errorf("create virtual environment as fluxo: %s %w", out, err)
	}
	if out, err := syscmd.RunAsUserInDir(ctx, 30*time.Second, "fluxo", tempDir, filepath.Join(tempDir, ".venv", "bin", "python"), "-m", "pip", "--version"); err != nil {
		return fmt.Errorf("validate virtual-environment pip as fluxo: %s %w", out, err)
	}
	return nil
}

func download(ctx context.Context, url, destination string) error {
	client := &http.Client{Timeout: 3 * time.Minute}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("unexpected HTTP status %s", resp.Status)
			resp.Body.Close()
			continue
		}
		file, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			resp.Body.Close()
			return err
		}
		written, copyErr := io.Copy(file, io.LimitReader(resp.Body, (128<<20)+1))
		closeErr := file.Close()
		resp.Body.Close()
		if written > 128<<20 {
			return fmt.Errorf("download exceeds 128 MiB limit")
		}
		if copyErr == nil && closeErr == nil {
			return nil
		}
		lastErr = copyErr
	}
	return lastErr
}

func verifySHA256(path, want string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("SHA-256 mismatch: got %s", got)
	}
	return nil
}

func extractUVArchive(archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	found := map[string]bool{}
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		base := filepath.Base(header.Name)
		if (base != "uv" && base != "uvx") || header.Typeflag != tar.TypeReg {
			continue
		}
		target := filepath.Join(destination, base)
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(reader, 128<<20))
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		found[base] = true
	}
	if !found["uv"] || !found["uvx"] {
		return fmt.Errorf("uv archive did not contain both uv and uvx")
	}
	return nil
}

func managedUVInstalled() bool {
	for _, name := range []string{"uv", "uvx"} {
		target, err := filepath.EvalSymlinks(filepath.Join("/usr/local/bin", name))
		if err != nil || !pathWithin(target, managedRoot) {
			return false
		}
	}
	return true
}

func pathWithin(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func pythonVersion(ctx context.Context, binary string) string {
	if binary == "" {
		return ""
	}
	out, err := exec.CommandContext(ctx, binary, "--version").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(string(out), "Python "))
}

func moduleVersion(ctx context.Context, binary, module string) string {
	out, err := exec.CommandContext(ctx, binary, "-m", module, "--version").CombinedOutput()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	for _, field := range fields {
		if _, ok := parseVersion(field); ok {
			return strings.TrimPrefix(field, "v")
		}
	}
	return ""
}

func commandVersion(ctx context.Context, name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil {
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	for _, field := range fields {
		if _, ok := parseVersion(field); ok {
			return strings.TrimPrefix(field, "v")
		}
	}
	return ""
}

func versionAtLeast(current, minimum string) bool {
	left, ok := parseVersion(current)
	if !ok {
		return false
	}
	right, ok := parseVersion(minimum)
	if !ok {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return left[i] > right[i]
		}
	}
	return true
}

func parseVersion(value string) ([3]int, bool) {
	var parsed [3]int
	matches := semanticVersion.FindStringSubmatch(strings.TrimSpace(value))
	if matches == nil {
		return parsed, false
	}
	for i := range parsed {
		component := matches[i+1]
		if component == "" {
			continue
		}
		number, err := strconv.Atoi(component)
		if err != nil {
			return parsed, false
		}
		parsed[i] = number
	}
	return parsed, true
}
