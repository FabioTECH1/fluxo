package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

type InstallerInventory struct {
	Action     string             `json:"action"`
	Message    string             `json:"message,omitempty"`
	Packages   map[string]string  `json:"packages"`
	AllowedNew []string           `json:"allowed_new"`
	Backend    BackendObservation `json:"backend"`
}

func InstallerPolicy(ctx context.Context, existing bool) InstallerInventory {
	r := InstallerInventory{Action: "blocked", Packages: map[string]string{}, AllowedNew: []string{}, Backend: observeBackend(ctx)}
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/dpkg-query", "-W", "-f=${binary:Package}\t${db:Status-Status}\t${db:Status-Eflag}\t${Version}\t${Provides}\n")
	if err != nil {
		r.Message = "Unable to inspect installed scheduling providers."
		return r
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			continue
		}
		name := strings.SplitN(f[0], ":", 2)[0]
		provider := name == "cron" || name == "cronie" || name == "cron-daemon-common" || name == "systemd-cron" || name == "bcron" || name == "fcron"
		// Versioned virtual Provides are accepted as provider declarations.
		provider = provider || regexp.MustCompile(`(^|,)\s*cron-daemon(?:\s|,|$)`).MatchString(f[4])
		if !provider {
			continue
		}
		if f[1] != "installed" && f[1] != "config-files" && f[1] != "not-installed" || f[2] != "ok" {
			r.Message = "Scheduling packages need repair before installing Fluxo; review their dpkg state."
			return r
		}
		if f[1] == "installed" && f[3] != "" {
			r.Packages[name] = f[3]
		}
	}
	if r.Backend.State == "unknown" || r.Backend.State == "ambiguous" {
		r.Message = r.Backend.Message
		return r
	}
	// A panel upgrade is not authorization to install/activate a missing engine.
	if existing {
		r.Action = "preserve"
		return r
	}
	providers := len(r.Packages)
	if _, ok := r.Packages["cron-daemon-common"]; ok {
		providers--
	}
	if providers > 0 || r.Backend.State != "missing" {
		r.Action = "preserve"
		return r
	}
	if hasExistingSchedules() {
		r.Message = "No scheduling engine is installed, but existing cron files could become active. Configure the intended scheduler before installing Fluxo; no schedules were started."
		return r
	}
	r.Action = "install-cron"
	r.AllowedNew = []string{"cron", "cron-daemon-common"}
	return r
}

func hasExistingSchedules() bool {
	return existingSchedulesAt("/")
}

func existingSchedulesAt(root string) bool {
	resolve := func(path string) string { return filepath.Join(root, strings.TrimPrefix(path, "/")) }
	for _, dir := range []string{"/etc/cron.hourly", "/etc/cron.daily", "/etc/cron.weekly", "/etc/cron.monthly"} {
		dir = resolve(dir)
		entries, err := os.ReadDir(dir)
		if err != nil && !os.IsNotExist(err) {
			return true
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			st, err := os.Stat(filepath.Join(dir, entry.Name()))
			if err != nil {
				return true
			}
			if st.Mode().IsRegular() && st.Mode().Perm()&0111 != 0 {
				return true
			}
		}
	}
	paths := []string{resolve("/etc/crontab"), resolve("/etc/anacrontab")}
	for _, dir := range []string{"/etc/cron.d", "/var/spool/cron/crontabs"} {
		dir = resolve(dir)
		entries, err := os.ReadDir(dir)
		if err != nil && !os.IsNotExist(err) {
			return true
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), ".") {
				paths = append(paths, filepath.Join(dir, entry.Name()))
			}
		}
	}
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return true
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				return true
			}
		}
	}
	return false
}
