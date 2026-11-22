package hostsystem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/google/uuid"
)

const rebootPath = "/var/lib/fluxo/server-reboot.json"
const rebootUnit = "fluxo-server-reboot.service"

type Reboot struct {
	ID          string    `json:"id"`
	BootID      string    `json:"boot_id"`
	Phase       string    `json:"phase"`
	RequestedAt time.Time `json:"requested_at"`
	Expected    []string  `json:"expected_services"`
	Detail      string    `json:"detail"`
}

func (h *Host) RebootState() (*Reboot, error) {
	b, e := h.read(rebootPath)
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var v Reboot
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&v); e != nil {
		return nil, e
	}
	if _, e = uuid.Parse(v.ID); e != nil || len(v.Expected) > 128 || v.RequestedAt.IsZero() || (v.Phase != "preparing" && v.Phase != "accepted" && v.Phase != "unconfirmed" && v.Phase != "failed") {
		return nil, errors.New("Invalid saved restart request")
	}
	if _, e = uuid.Parse(v.BootID); e != nil {
		return nil, errors.New("Invalid saved restart boot identity")
	}
	for _, unit := range v.Expected {
		if !serviceName.MatchString(unit) {
			return nil, errors.New("Invalid saved restart service")
		}
	}
	return &v, nil
}
func (h *Host) saveReboot(v Reboot) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return h.write(rebootPath, b, 0600)
}

var serviceName = regexp.MustCompile(`^(nginx|mariadb|mysql|postgresql|redis-server|cron|cronie|supervisor|php[0-9]+\.[0-9]+-fpm|fluxo-daemon-[0-9]+(-[0-9]+)?)\.service$`)

func (h *Host) serviceNames() ([]string, error) {
	names := []string{"nginx.service", "mariadb.service", "mysql.service", "postgresql.service", "redis-server.service", "cron.service", "cronie.service", "supervisor.service"}
	for _, pat := range []string{"/etc/systemd/system/fluxo-daemon-*.service", "/lib/systemd/system/php*-fpm.service", "/usr/lib/systemd/system/php*-fpm.service"} {
		paths, e := filepath.Glob(h.path(pat))
		if e != nil {
			return nil, e
		}
		for _, p := range paths {
			n := filepath.Base(p)
			if serviceName.MatchString(n) {
				names = append(names, n)
			}
		}
	}
	if len(names) > 256 {
		return nil, errors.New("Service inventory is too large to review safely")
	}
	return names, nil
}
func (h *Host) ExpectedServices(ctx context.Context) ([]string, error) {
	names, e := h.serviceNames()
	if e != nil {
		return nil, e
	}
	out := []string{}
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		p, e := h.unit(ctx, name)
		if e != nil {
			return nil, fmt.Errorf("Cannot inspect %s", name)
		}
		if p["LoadState"] == "not-found" {
			continue
		}
		if p["LoadState"] != "loaded" {
			return nil, fmt.Errorf("Unknown service state: %s", name)
		}
		if p["ActiveState"] == "active" && p["UnitFileState"] == "enabled" {
			out = append(out, name)
		}
	}
	if len(out) > 128 {
		return nil, errors.New("Too many expected services")
	}
	return out, nil
}
func (h *Host) Services(ctx context.Context, id string) ([]Service, error) {
	r, e := h.RebootState()
	if e != nil {
		return nil, e
	}
	if r == nil || r.ID != id {
		return nil, errors.New("Restart request not found")
	}
	boot, e := h.BootID()
	if e != nil {
		return nil, e
	}
	if boot == r.BootID {
		return nil, errors.New("The server has not confirmed a new boot")
	}
	out := []Service{}
	for _, name := range r.Expected {
		p, e := h.unit(ctx, name)
		v := Service{Unit: name, State: "unknown", Detail: "Service state could not be read."}
		if e == nil {
			v.Detail = "systemd: " + p["ActiveState"] + " / " + p["SubState"] + "; result " + p["Result"]
			switch p["ActiveState"] {
			case "active":
				v.State = "running"
			case "activating", "reloading":
				v.State = "starting"
			case "failed":
				v.State = "failed"
			case "inactive":
				v.State = "stopped"
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// Schedule is called while the request owns the exclusive system-management
// admission gate. A fixed transient service owns the same gate and every native
// package lock before publishing READY. It keeps them through delayed reboot.
func (h *Host) Schedule(ctx context.Context, boot string) (Reboot, error) {
	if h.Root == "" {
		if e := sameInstalledImage(); e != nil {
			return Reboot{}, e
		}
	}
	current, e := h.BootID()
	if e != nil || boot != current {
		return Reboot{}, errors.New("Server boot changed; refresh before restarting")
	}
	old, e := h.RebootState()
	if e != nil {
		return Reboot{}, e
	}
	if old != nil && old.BootID == boot && old.Phase != "failed" {
		return Reboot{}, errors.New("A restart request already exists; check its status before trying again")
	}
	expected, e := h.ExpectedServices(ctx)
	if e != nil {
		return Reboot{}, e
	}
	v := Reboot{ID: uuid.NewString(), BootID: boot, Phase: "preparing", RequestedAt: time.Now().UTC(), Expected: expected, Detail: "Waiting for exclusive maintenance admission."}
	if e = h.saveReboot(v); e != nil {
		return v, e
	}
	_, e = h.Run(ctx, "/usr/bin/systemd-run", "--unit="+rebootUnit, "--collect", "--property=Type=exec", "--property=TimeoutStartSec=30s", "--setenv=FLUXO_ENV=prod", "--setenv=FLUXO_DATA_DIR=/var/lib/fluxo", "/usr/local/bin/fluxo", "server-reboot-execute", v.ID)
	if e != nil {
		v.Phase = "failed"
		v.Detail = "Failed to schedule restart: " + e.Error()
		if se := h.saveReboot(v); se != nil {
			return v, se
		}
		return v, e
	}
	return v, nil
}

// Execute is invoked only by the fixed root service. admit must hold lifecycle,
// Fluxo packages, native dpkg and pending-operation exclusion until it returns.
func (h *Host) Execute(ctx context.Context, id string, admit func(context.Context) (func(), error)) (err error) {
	if _, e := uuid.Parse(id); e != nil {
		return e
	}
	v, e := h.RebootState()
	if e != nil {
		return e
	}
	if v == nil || v.ID != id || v.Phase != "preparing" {
		return errors.New("No matching pending restart")
	}
	fail := func(e error) error {
		v.Phase = "failed"
		v.Detail = e.Error()
		if se := h.saveReboot(*v); se != nil {
			return errors.Join(e, se)
		}
		return e
	}
	release, e := admit(ctx)
	if e != nil {
		return fail(e)
	}
	defer release()
	boot, e := h.BootID()
	if e != nil || boot != v.BootID {
		return fail(errors.New("Boot changed before restart admission"))
	}
	v.Phase = "accepted"
	v.Detail = "Restart accepted; waiting for shutdown."
	if e = h.saveReboot(*v); e != nil {
		return e
	}
	// READY is observed before the HTTP acknowledgement. No observer/read triggers
	// reboot; only this explicitly requested helper can reach this point.
	if e = h.pause(ctx, 10*time.Second); e != nil {
		return fail(e)
	}
	if _, e = h.Run(ctx, "/usr/bin/systemctl", "reboot"); e != nil {
		v.Phase = "unconfirmed"
		v.Detail = "Shutdown could not be confirmed. Use SSH or the provider console to review the pending restart; do not resubmit it."
		if se := h.saveReboot(*v); se != nil { /* Keep exclusion even when the diagnostic cannot be persisted. */
		}
	}
	// Once a shutdown request has been sent, a command timeout/error cannot prove
	// it was rejected by PID1. Never release exclusion on an arbitrary deadline.
	// Normal shutdown terminates this transient service and closes its locks.
	// A stuck/uncertain shutdown requires explicit operator console/SSH review.
	if h.shutdownWait != nil {
		return h.shutdownWait()
	}
	select {}

}
func (h *Host) AwaitAccepted(ctx context.Context, id string) (Reboot, error) {
	for {
		v, e := h.RebootState()
		if e != nil {
			return Reboot{}, e
		}
		if v == nil || v.ID != id {
			return Reboot{}, errors.New("Restart state changed")
		}
		if v.Phase == "accepted" {
			return *v, nil
		}
		if v.Phase == "failed" {
			return *v, errors.New(v.Detail)
		}
		select {
		case <-ctx.Done():
			return *v, errors.New("Restart scheduling is unconfirmed; check status before retrying")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (h *Host) pause(ctx context.Context, d time.Duration) error {
	if h.wait != nil {
		return h.wait(ctx, d)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
