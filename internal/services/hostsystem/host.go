// Package hostsystem manages current Ubuntu settings. It has no migration path,
// package installation, application repair or automatic reboot policy.
package hostsystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/syscmd"
)

type Runner func(context.Context, string, ...string) (string, error)
type Host struct {
	Root         string
	Run          Runner
	wait         func(context.Context, time.Duration) error
	shutdownWait func() error
}

func New() *Host {
	return &Host{Run: func(ctx context.Context, name string, args ...string) (string, error) {
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return syscmd.RunMaintenanceStdout(c, name, args...)
	}}
}
func (h *Host) path(p string) string { return filepath.Join(h.Root, p) }
func (h *Host) read(p string) ([]byte, error) {
	p = h.path(p)
	if e := h.directory(filepath.Dir(p)); e != nil {
		return nil, e
	}
	fd, e := syscall.Open(p, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), p)
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	v, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.Mode().IsRegular() || v.Uid != h.owner() || v.Nlink != 1 || st.Mode().Perm()&0022 != 0 || st.Size() > 65536 {
		return nil, fmt.Errorf("Unsafe system input: %s", p)
	}
	return io.ReadAll(io.LimitReader(f, 65537))
}
func properties(raw string) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(raw, "\n") {
		k, v, ok := strings.Cut(l, "=")
		if ok {
			m[k] = v
		}
	}
	return m
}
func (h *Host) unit(ctx context.Context, name string) (map[string]string, error) {
	raw, e := h.Run(ctx, "/usr/bin/systemctl", "show", name, "--property=Id,LoadState,ActiveState,SubState,UnitFileState,Result,ExecMainStatus,ExecMainStartTimestamp,ExecMainExitTimestamp,FragmentPath,DropInPaths,Triggers")
	return properties(raw), e
}
func (h *Host) BootID() (string, error) {
	b, e := os.ReadFile(h.path("/proc/sys/kernel/random/boot_id"))
	if e != nil {
		return "", e
	}
	v := strings.TrimSpace(string(b))
	if len(v) != 36 || strings.ContainsAny(v, "/\n \t") {
		return "", errors.New("Boot identity is unavailable")
	}
	return v, nil
}

type Updates struct {
	State           string `json:"state"`
	Editable        bool   `json:"editable"`
	Detail          string `json:"detail"`
	AutomaticReboot string `json:"automatic_reboot"`
	LastAttempt     string `json:"last_attempt,omitempty"`
	LastResult      string `json:"last_result"`
}
type RestartRequired struct {
	State    string   `json:"state"`
	Packages []string `json:"packages"`
	Detail   string   `json:"detail"`
}
type Service struct {
	Unit   string `json:"unit"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}
type Status struct {
	BootID  string          `json:"boot_id"`
	Updates Updates         `json:"updates"`
	Restart RestartRequired `json:"restart_required"`
	Reboot  *Reboot         `json:"reboot"`
}

func (h *Host) Status(ctx context.Context) (Status, error) {
	boot, e := h.BootID()
	if e != nil {
		return Status{}, e
	}
	s := Status{BootID: boot, Updates: h.Updates(ctx), Restart: h.RestartRequired()}
	r, e := h.RebootState()
	if e != nil {
		return s, e
	}
	s.Reboot = r
	return s, nil
}
func (h *Host) RestartRequired() RestartRequired {
	r := RestartRequired{State: "unknown", Packages: []string{}, Detail: "Ubuntu restart status is unavailable."}
	if e := h.directory(h.path("/run")); e != nil {
		return r
	}
	_, e := h.read("/run/reboot-required")
	if os.IsNotExist(e) {
		r.State = "not_required"
		r.Detail = "Ubuntu does not currently report a required restart."
		return r
	}
	if e != nil {
		return r
	}
	r.State = "required"
	r.Detail = "Ubuntu reports a restart is required. Package reasons are unavailable."
	b, e := h.read("/run/reboot-required.pkgs")
	if e == nil {
		for _, p := range strings.Fields(string(b)) {
			if len(r.Packages) >= 128 {
				break
			}
			if validPackage(p) {
				r.Packages = append(r.Packages, p)
			}
		}
		if len(r.Packages) > 0 {
			r.Detail = "Reported by Ubuntu: " + strings.Join(r.Packages, ", ")
		}
	}
	return r
}
func validPackage(s string) bool {
	if s == "" || len(s) > 180 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.ContainsRune("+.-:", c)) {
			return false
		}
	}
	return true
}

func (h *Host) installedTool(path string) error {
	p := h.path(path)
	if e := h.directory(filepath.Dir(p)); e != nil {
		return e
	}
	fd, e := syscall.Open(p, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return e
	}
	f := os.NewFile(uintptr(fd), p)
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return e
	}
	v, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.Mode().IsRegular() || v.Uid != h.owner() || v.Nlink != 1 || st.Mode().Perm()&0022 != 0 || st.Mode().Perm()&0111 == 0 {
		return errors.New("Unsafe unattended-upgrades executable")
	}
	return nil
}
