package maintenancetask

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const dpkgBootstrapManifest = "/var/lib/dpkg/info/dpkg.md5sums"
const dpkgBootstrapRevision = "protected-dpkg-payload-v1"

var dpkgBootstrapPrograms = []string{"/usr/bin/dpkg", "/usr/bin/dpkg-query"}

type dpkgBootstrap struct{ files map[string]string }

// The protected local package database is an explicit OS trust boundary. This
// proves payload consistency before querying the database; it is not a signed
// provenance proof or protection against arbitrary privileged replacement of
// both the package database and code. No package query bootstraps this check.
func inspectDpkgBootstrap(read func(string, int64) ([]byte, error)) (dpkgBootstrap, error) {
	result := dpkgBootstrap{files: map[string]string{}}
	manifest, err := read(dpkgBootstrapManifest, 8<<20)
	if err != nil {
		return result, fmt.Errorf("protected dpkg bootstrap metadata: %w", err)
	}
	expected, err := parseDpkgBootstrapManifest(manifest)
	if err != nil {
		return result, err
	}
	result.files[dpkgBootstrapManifest] = digest(manifest)
	for _, path := range dpkgBootstrapPrograms {
		code, err := read(path, 32<<20)
		if err != nil {
			return result, fmt.Errorf("protected dpkg bootstrap program %s: %w", path, err)
		}
		sum := md5.Sum(code)
		if hex.EncodeToString(sum[:]) != expected[strings.TrimPrefix(path, "/")] {
			return result, fmt.Errorf("dpkg bootstrap payload differs from protected package metadata: %s", path)
		}
		result.files[path] = digest(code)
	}
	return result, nil
}

func parseDpkgBootstrapManifest(raw []byte) (map[string]string, error) {
	expected, err := parseProtectedMD5Manifest(raw)
	if err != nil {
		return nil, err
	}
	for _, path := range []string{"bin/dpkg", "bin/dpkg-query"} {
		if _, alias := expected[path]; alias {
			return nil, errors.New("aliased dpkg bootstrap program path")
		}
	}
	for _, path := range dpkgBootstrapPrograms {
		if expected[strings.TrimPrefix(path, "/")] == "" {
			return nil, fmt.Errorf("missing dpkg bootstrap integrity entry: %s", path)
		}
	}
	return expected, nil
}

func parseProtectedMD5Manifest(raw []byte) (map[string]string, error) {
	expected := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" {
			continue
		}
		if len(line) < 35 || line[32:34] != "  " {
			return nil, errors.New("unsupported protected package integrity metadata")
		}
		sum, path := strings.ToLower(line[:32]), line[34:]
		decoded, err := hex.DecodeString(sum)
		if err != nil || len(decoded) != md5.Size || filepath.IsAbs(path) || filepath.Clean(path) != path || path == "." || strings.HasPrefix(path, "../") || strings.ContainsAny(path, "\x00\r\n") {
			return nil, errors.New("invalid or aliased protected package integrity path")
		}
		if _, duplicate := expected[path]; duplicate {
			return nil, fmt.Errorf("duplicate protected package integrity entry: %s", path)
		}
		expected[path] = sum
	}
	return expected, nil
}

func (b dpkgBootstrap) record(p *profile) {
	for path, hash := range b.files {
		p.Files[path] = hash
	}
	p.External["dpkg-bootstrap-contract"] = dpkgBootstrapRevision
}

func (b dpkgBootstrap) verifyAndRun(read func(string, int64) ([]byte, error), run func() error) error {
	current, err := inspectDpkgBootstrap(read)
	if err != nil {
		return err
	}
	if len(b.files) != len(current.files) {
		return errors.New("dpkg bootstrap inputs changed before execution")
	}
	for path, hash := range b.files {
		if current.files[path] != hash {
			return fmt.Errorf("dpkg bootstrap input changed before execution: %s", path)
		}
	}
	return run()
}
