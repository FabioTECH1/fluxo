package nodeupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

const consumerPolicy = 3 // Node/Bun apps and qualified PHP consumers use reviewed durable coordination.
const managedBunPath = "/opt/fluxo/node-toolchain/bin/bun"

// This receipt binds the already installed, root-managed Bun executable. Node
// maintenance neither downloads Bun nor adopts an application-owned runtime.
type BunRuntime struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

func inspectManagedBun(ctx context.Context) (*BunRuntime, error) {
	const statePath = "/var/lib/fluxo/node-toolchain.json"
	for _, p := range []string{managedBunPath, statePath, "/usr/local/bin"} {
		if err := trustedPath(p); err != nil {
			return nil, err
		}
	}
	var state struct {
		Bun bool `json:"bun"`
	}
	b, err := os.ReadFile(statePath)
	if err != nil || json.Unmarshal(b, &state) != nil || !state.Bun {
		return nil, errors.New("Bun application requires a Fluxo-managed Bun installation")
	}
	link := "/usr/local/bin/bun"
	target, err := os.Readlink(link)
	resolved, pathErr := exec.LookPath("bun")
	if err != nil || target != managedBunPath || pathErr != nil || resolved != link {
		return nil, errors.New("Bun executable is external or shadowed")
	}
	info, err := os.Lstat(managedBunPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return nil, errors.New("Managed Bun executable is not a regular executable")
	}
	f, err := os.Open(managedBunPath)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	f.Close()
	if err != nil {
		return nil, err
	}
	v, err := syscmd.Run(ctx, 10*time.Second, managedBunPath, "--version")
	v = strings.TrimSpace(v)
	if err != nil || v == "" || strings.ContainsAny(v, " \t\r\n") {
		return nil, errors.New("Cannot identify managed Bun version")
	}
	return &BunRuntime{Path: managedBunPath, Version: v, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

func validateBunRuntimes(ctx context.Context, impact Impact) error {
	var current *BunRuntime
	for _, u := range impact.Units {
		if u.BunOnly && u.Bun == nil {
			return errors.New("Review Bun application coordination again before maintenance")
		}
		if u.Bun == nil {
			continue
		}
		if current == nil {
			var err error
			current, err = inspectManagedBun(ctx)
			if err != nil {
				return err
			}
		}
		if *current != *u.Bun {
			return errors.New("Managed Bun changed since review; review again before maintenance")
		}
	}
	return nil
}

func hasBunProcess(u Unit) bool {
	for pid := range u.PIDs {
		p, err := os.Readlink(processExePath(pid))
		if err == nil && filepath.Base(strings.TrimSuffix(p, " (deleted)")) == "bun" {
			return true
		}
	}
	return false
}

func processExePath(pid int) string { return fmt.Sprintf("/proc/%d/exe", pid) }

func (g Generation) executionImpact() Impact {
	if g.Coordination != nil {
		return *g.Coordination
	}
	return g.Plan.Impact // Inspection only; transition requires fresh coordination.
}
