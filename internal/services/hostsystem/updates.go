package hostsystem

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const policyPath = "/etc/apt/apt.conf.d/99zz-fluxo-security-updates"
const policyHeader = "// Managed by Fluxo: automatic security updates only.\n"

var configLine = regexp.MustCompile(`^([A-Za-z0-9:._-]+) "((?:[^"\\]|\\.)*)";$`)

func parseAPT(raw string) (map[string]string, error) {
	m := map[string]string{}
	for _, l := range strings.Split(raw, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		v := configLine.FindStringSubmatch(l)
		if v == nil {
			return nil, errors.New("Unrecognized effective APT configuration")
		}
		decoded, de := strconv.Unquote(`"` + v[2] + `"`)
		if de != nil {
			return nil, errors.New("Unrecognized APT string encoding")
		}
		v[2] = decoded
		if _, ok := m[v[1]]; ok && !strings.HasSuffix(v[1], "::") {
			return nil, errors.New("Ambiguous effective APT configuration")
		}
		if strings.HasSuffix(v[1], "::") {
			m[v[1]] += v[2] + "\x00"
		} else {
			m[v[1]] = v[2]
		}
	}
	return m, nil
}
func (h *Host) apt(ctx context.Context) (map[string]string, error) {
	raw, e := h.Run(ctx, "/usr/bin/apt-config", "dump")
	if e != nil {
		return nil, e
	}
	return parseAPT(raw)
}
func allowedOrigins(raw string) bool {
	found := false
	for _, l := range strings.Split(raw, "\n") {
		v := configLine.FindStringSubmatch(strings.TrimSpace(l))
		if v == nil {
			continue
		}
		if v[1] == "Unattended-Upgrade::Origins-Pattern::" && v[2] != "" {
			return false
		}
		if v[1] != "Unattended-Upgrade::Allowed-Origins::" {
			continue
		}
		// Ubuntu's stock base/security and ESM security origins. Expanded and literal
		// distro variables are accepted; arbitrary third-party origins are external.
		origin := v[2]
		if strings.Contains(origin, "${distro_id}") {
			origin = strings.ReplaceAll(origin, "${distro_id}", "Ubuntu")
		}
		if !regexp.MustCompile(`^(Ubuntu|UbuntuESMApps|UbuntuESM):([a-z]+|\$\{distro_codename\})(-security|-apps-security|-infra-security)?$`).MatchString(origin) {
			return false
		}
		if strings.HasSuffix(origin, "security") {
			found = true
		}
	}
	return found
}
func (h *Host) Updates(ctx context.Context) Updates {
	u := Updates{State: "unknown", AutomaticReboot: "unknown", LastResult: "unknown", Detail: "Unable to verify Ubuntu automatic security-update policy."}
	osrelease, e := h.read("/etc/os-release")
	if link, le := os.Readlink(h.path("/etc/os-release")); le == nil && (link == "../usr/lib/os-release" || link == "/usr/lib/os-release") {
		osrelease, e = h.read("/usr/lib/os-release")
	}
	if e != nil || !regexp.MustCompile(`(?m)^ID=("ubuntu"|ubuntu)$`).Match(osrelease) {
		u.State = "unsupported"
		u.Detail = "Only Ubuntu unattended-upgrades is supported."
		return u
	}
	if e = h.installedTool("/usr/bin/unattended-upgrade"); os.IsNotExist(e) {
		u.State = "not_installed"
		u.Detail = "unattended-upgrades is not installed. Install it over SSH: sudo apt install unattended-upgrades"
		return u
	} else if e != nil {
		return u
	}
	installed, installErr := h.Run(ctx, "/usr/bin/dpkg-query", "--show", "--showformat=${db:Status-Status}", "unattended-upgrades")
	if installErr != nil || strings.TrimSpace(installed) != "installed" {
		u.Detail = "The unattended-upgrades package installation could not be verified. Review it over SSH."
		return u
	}
	raw, e := h.Run(ctx, "/usr/bin/apt-config", "dump")
	if e != nil {
		return u
	}
	m, e := parseAPT(raw)
	if e != nil {
		u.Detail = e.Error()
		return u
	}
	reboot := strings.ToLower(m["Unattended-Upgrade::Automatic-Reboot"])
	switch reboot {
	case "", "false", "0", "no":
		u.AutomaticReboot = "disabled"
	case "true", "1", "yes":
		u.AutomaticReboot = "enabled"
	default:
		return u
	}
	attempt, e := h.unit(ctx, "apt-daily-upgrade.service")
	if e == nil && attempt["ExecMainStartTimestamp"] != "" && attempt["ExecMainStartTimestamp"] != "n/a" {
		u.LastAttempt = attempt["ExecMainStartTimestamp"]
		if attempt["ExecMainExitTimestamp"] != "" && attempt["ExecMainExitTimestamp"] != "n/a" && attempt["ActiveState"] != "activating" {
			if attempt["Result"] == "success" && attempt["ExecMainStatus"] == "0" {
				u.LastResult = "successful"
			} else if attempt["Result"] != "" {
				u.LastResult = "failed"
			}
		}
	}
	if u.AutomaticReboot != "disabled" {
		u.State = "external"
		u.Detail = "An administrator automatic-reboot policy is enabled. Review it over SSH; Fluxo has not changed it."
		return u
	}
	if m["APT::Periodic::Enable"] == "0" || m["Dir::Etc::parts"] != "apt.conf.d" || m["Dir::Etc::main"] != "apt.conf" || !allowedOrigins(raw) {
		u.State = "external"
		u.Detail = "Security-only coverage could not be confirmed. Review unattended-upgrades origins over SSH."
		return u
	}
	for _, name := range []string{"apt-daily.timer", "apt-daily-upgrade.timer"} {
		v, e := h.unit(ctx, name)
		if e != nil {
			return u
		}
		if (v["FragmentPath"] != "/usr/lib/systemd/system/"+name && v["FragmentPath"] != "/lib/systemd/system/"+name) || v["DropInPaths"] != "" || v["Triggers"] != strings.TrimSuffix(name, ".timer")+".service" || v["LoadState"] != "loaded" || v["ActiveState"] != "active" || (v["UnitFileState"] != "enabled" && v["UnitFileState"] != "enabled-runtime") {
			u.State = "external"
			u.Detail = "Ubuntu APT timers are stopped, disabled or externally managed. Review them over SSH."
			return u
		}
	}
	if n, e := strconv.Atoi(m["APT::Periodic::Update-Package-Lists"]); e != nil || n < 1 {
		u.State = "external"
		u.Detail = "APT package-list refresh is disabled or uses an unsupported schedule. Review it over SSH."
		return u
	}
	switch m["APT::Periodic::Unattended-Upgrade"] {
	case "0":
		u.State = "disabled"
	case "1":
		u.State = "enabled"
	default:
		u.State = "external"
		u.Detail = "The unattended-upgrades interval is externally managed. Review it over SSH."
		return u
	}
	u.Editable = true
	u.Detail = "Covers configured Ubuntu security repositories. Third-party software may need separate updates."
	return u
}

// SetUpdates is called only under exclusive lifecycle/package/native locks.
// Only the owned scalar override is changed. All other effective values must
// remain identical. Never install a package, start timers or alter auto-reboot.
func (h *Host) SetUpdates(ctx context.Context, enabled bool) (Updates, error) {
	u := h.Updates(ctx)
	if !u.Editable {
		return u, errors.New(u.Detail)
	}
	before, e := h.apt(ctx)
	if e != nil {
		return u, e
	}
	old, e := h.read(policyPath)
	exists := e == nil
	if e != nil && !os.IsNotExist(e) {
		return u, e
	}
	if exists && string(old) != policyHeader+`APT::Periodic::Unattended-Upgrade "1";`+"\n" && string(old) != policyHeader+`APT::Periodic::Unattended-Upgrade "0";`+"\n" {
		return u, errors.New("The Fluxo policy file was modified; review it over SSH")
	}
	value := "0"
	if enabled {
		value = "1"
	}
	next := []byte(policyHeader + `APT::Periodic::Unattended-Upgrade "` + value + `";` + "\n")
	writeErr := h.write(policyPath, next, 0644)
	after, e := h.apt(ctx)
	valid := writeErr == nil && e == nil && after["APT::Periodic::Unattended-Upgrade"] == value
	delete(before, "APT::Periodic::Unattended-Upgrade")
	delete(after, "APT::Periodic::Unattended-Upgrade")
	if !maps.Equal(before, after) {
		valid = false
	}
	verified := h.Updates(ctx)
	if !verified.Editable || enabled != (verified.State == "enabled") {
		valid = false
	}
	if !valid {
		// Restore only our bytes, and only if they are still the bytes we wrote.
		current, readErr := h.read(policyPath)
		if writeErr != nil && ((exists && readErr == nil && string(current) == string(old)) || (!exists && os.IsNotExist(readErr))) {
			return u, fmt.Errorf("Policy write failed; previous policy is unchanged: %w", writeErr)
		}
		if readErr != nil || string(current) != string(next) {
			return u, errors.New("Policy verification failed and the policy changed concurrently; review it over SSH")
		}
		if exists {
			e = h.write(policyPath, old, 0644)
		} else {
			e = os.Remove(h.path(policyPath))
			if e == nil {
				d, de := os.Open(filepath.Dir(h.path(policyPath)))
				if de == nil {
					e = d.Sync()
					d.Close()
				} else {
					e = de
				}
			}
		}
		if e != nil {
			return u, fmt.Errorf("Policy verification failed; rollback needs SSH review: %w", e)
		}
		return u, errors.New("Effective policy verification failed; previous Fluxo policy restored")
	}
	return verified, nil
}
func (h *Host) write(path string, b []byte, mode os.FileMode) error {
	path = h.path(path)
	dir := filepath.Dir(path)
	// The destination directory must already be protected; no path creation here.
	if e := h.directory(dir); e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".fluxo-system-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	d, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
