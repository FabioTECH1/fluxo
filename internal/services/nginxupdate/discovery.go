// Package nginxupdate provides informational APT discovery only. It never
// invokes a package installation, repository rewrite or service activation.
package nginxupdate

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/scheduler"
	"fluxo/internal/syscmd"
)

//go:embed inspect.py
var inspection string

type Origin struct {
	Origin    string `json:"origin"`
	Archive   string `json:"archive"`
	Site      string `json:"site"`
	Component string `json:"component"`
	Trusted   bool   `json:"trusted"`
}
type Snapshot struct {
	State            string   `json:"state"`
	Reason           string   `json:"reason"`
	Binary           string   `json:"binary"`
	Package          string   `json:"package"`
	Architecture     string   `json:"architecture"`
	InstalledVersion string   `json:"installed_version"`
	CandidateVersion string   `json:"candidate_version"`
	Sources          []Origin `json:"sources"`
	Held             bool     `json:"held"`
	SourceIdentity   string   `json:"source_identity"`
	Identity         string   `json:"identity"`
}
type Runner func(context.Context, string, string, ...string) (string, error)

func run(ctx context.Context, input, name string, args ...string) (string, error) {
	w := &limitedOutput{}
	e := syscmd.RunMaintenance(ctx, input, w, nil, name, args...)
	return w.String(), e
}

type limitedOutput struct{ strings.Builder }

func (w *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > 65536-w.Len() {
		return 0, errors.New("Inspection output exceeded its limit")
	}
	return w.Builder.Write(p)
}

var ownerPattern = regexp.MustCompile(`^nginx(?:-[a-z0-9][a-z0-9+.-]*)?(?::[a-z0-9][a-z0-9-]*)?$`)

func owningPackage(output, binary string) (string, error) {
	found := ""
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		owner, path, ok := strings.Cut(line, ": ")
		if !ok || path != binary {
			continue
		}
		if !ownerPattern.MatchString(owner) || found != "" {
			return "", errors.New("Nginx binary ownership is ambiguous or unsupported")
		}
		found = owner
	}
	if found == "" {
		return "", errors.New("Nginx binary is not owned by a supported APT package")
	}
	return found, nil
}
func binaryDigest(path, metadata string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > 64<<20 {
		return "", errors.New("Unsupported Nginx binary")
	}
	expected := ""
	relative := strings.TrimPrefix(path, "/")
	for _, line := range strings.Split(metadata, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == relative {
			if expected != "" {
				return "", errors.New("Ambiguous binary package payload")
			}
			expected = fields[0]
		}
	}
	if len(expected) != 32 {
		return "", errors.New("No package payload checksum exists for this binary")
	}
	old, strong := md5.New(), sha256.New()
	if _, e = io.Copy(io.MultiWriter(old, strong), io.LimitReader(f, 64<<20+1)); e != nil {
		return "", e
	}
	if !strings.EqualFold(hex.EncodeToString(old.Sum(nil)), expected) {
		return "", errors.New("Nginx binary differs from its installed package payload; package update applicability is unknown")
	}
	return hex.EncodeToString(strong.Sum(nil)), nil
}

// ProductionInspect is called only after an explicit metadata refresh, under
// native installed-package exclusion. Python reads libapt; it never commits.
func ProductionInspect(ctx context.Context) (Snapshot, error) {
	return inspectWith(ctx, run, exec.LookPath)
}
func inspectWith(ctx context.Context, runner Runner, lookup func(string) (string, error)) (Snapshot, error) {
	unavailable := func(reason string) Snapshot {
		return Snapshot{State: "unsupported", Reason: reason, Sources: []Origin{}}
	}
	binary, e := lookup("nginx")
	if e != nil {
		return unavailable("Nginx binary was not found; package update status is unknown."), nil
	}
	binary, e = filepath.EvalSymlinks(binary)
	if e != nil {
		return unavailable("Nginx binary path cannot be resolved."), nil
	}
	output, e := runner(ctx, "", "/usr/bin/dpkg-query", "--search", binary)
	if e != nil {
		return unavailable("The binary is not managed by dpkg, or ownership could not be read."), nil
	}
	owner, e := owningPackage(output, binary)
	if e != nil {
		return unavailable(e.Error()), nil
	}
	metadata, e := runner(ctx, "", "/usr/bin/dpkg-query", "--control-show", owner, "md5sums")
	if e != nil {
		return unavailable("The installed binary package has no readable payload manifest."), nil
	}
	digest, e := binaryDigest(binary, metadata)
	if e != nil {
		return unavailable(e.Error()), nil
	}
	input, _ := json.Marshal(map[string]string{"package": owner})
	raw, e := runner(ctx, string(input), "/usr/bin/python3", "-I", "-c", inspection)
	if e != nil {
		return Snapshot{}, errors.New("APT candidate inspection failed; no current result was accepted")
	}
	var result Snapshot
	payload, e := observation(raw)
	if e != nil {
		return Snapshot{}, e
	}
	if e = json.Unmarshal([]byte(payload), &result); e != nil {
		return Snapshot{}, errors.New("APT inspection returned invalid data")
	}
	if result.Package == "" || result.InstalledVersion == "" || result.SourceIdentity == "" {
		return Snapshot{}, errors.New("APT inspection did not return complete package identity")
	}
	after, e := binaryDigest(binary, metadata)
	if e != nil || after != digest {
		return Snapshot{}, errors.New("Installed binary changed during the check; retry after package work finishes")
	}
	result.Binary = binary
	identity, _ := json.Marshal(map[string]any{"package": result.Package, "architecture": result.Architecture, "installed": result.InstalledVersion, "candidate": result.CandidateVersion, "policy": result.SourceIdentity, "sources": result.Sources, "binary": digest})
	sum := sha256.Sum256(identity)
	result.Identity = hex.EncodeToString(sum[:])
	return result, nil
}

// CheckAdmission respects existing application/runtime maintenance, without
// turning discovery into host certification or a runtime repair operation.
func CheckAdmission(ctx context.Context) (func(), error) {
	a, e := packages.Lifecycle(ctx, true)
	if e != nil {
		return nil, e
	}
	b, e := packages.Acquire(ctx)
	if e != nil {
		a()
		return nil, e
	}
	release := func() { b(); a() }
	if busy, e := scheduler.UpgradeBusy(); e != nil || busy {
		release()
		return nil, errors.New("Finish the current Fluxo installation before checking Nginx packages")
	}
	if busy, e := database.PHPMaintenancePending(); e != nil || busy {
		release()
		return nil, errors.New("Finish current Fluxo maintenance before checking Nginx packages")
	}
	return release, nil
}
func observation(raw string) (string, error) {
	payload := ""
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "FLUXO_NGINX_JSON:") {
			if payload != "" {
				return "", errors.New("APT inspection returned duplicate results")
			}
			payload = strings.TrimPrefix(line, "FLUXO_NGINX_JSON:")
		}
	}
	if payload == "" {
		return "", errors.New("APT inspection returned no result")
	}
	return payload, nil
}
func Refresh(ctx context.Context) error { return refreshWith(ctx, run) }
func refreshWith(ctx context.Context, runner Runner) error {
	policy := func() (string, error) {
		raw, e := runner(ctx, `{"action":"policy"}`, "/usr/bin/python3", "-I", "-c", inspection)
		if e != nil {
			return "", errors.New("APT policy could not be inspected")
		}
		raw, e = observation(raw)
		if e != nil {
			return "", e
		}
		var result struct {
			Policy string `json:"policy"`
		}
		if json.Unmarshal([]byte(raw), &result) != nil || len(result.Policy) != 64 {
			return "", errors.New("APT policy identity is incomplete")
		}
		return result.Policy, nil
	}
	before, e := policy()
	if e != nil {
		return e
	}
	_, e = runner(ctx, "", "/usr/bin/apt-get", "-o", "APT::Update::Error-Mode=any", "-o", "Acquire::Retries=1", "-o", "Acquire::http::Timeout=20", "-o", "Acquire::https::Timeout=20", "-o", "DPkg::Lock::Timeout=10", "update")
	if e != nil {
		return errors.New("Repository refresh failed; previous candidates are stale and were not accepted")
	}
	after, e := policy()
	if e != nil {
		return e
	}
	if before != after {
		return errors.New("APT sources or policy changed during refresh; retry the check")
	}
	return nil
}

func nativeInspect(ctx context.Context) (Snapshot, error) {
	release, e := packages.NativeDPKG(ctx)
	if e != nil {
		return Snapshot{}, fmt.Errorf("APT/dpkg is busy; no package changes were made")
	}
	defer release()
	return ProductionInspect(ctx)
}

const checkTimeout = 90 * time.Second
