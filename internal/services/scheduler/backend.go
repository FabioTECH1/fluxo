package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fluxo/internal/syscmd"
)

// Observation is operational information, never permission to patch PHP. The
// receipt and drain paths independently revalidate the execution domain.
type BackendObservation struct {
	Engine        string      `json:"engine"`
	State         string      `json:"state"`
	Unit          string      `json:"unit,omitempty"`
	Package       string      `json:"package,omitempty"`
	Version       string      `json:"version,omitempty"`
	Architecture  string      `json:"architecture,omitempty"`
	Enabled       string      `json:"enabled,omitempty"`
	Qualification string      `json:"qualification"`
	Code          string      `json:"code,omitempty"`
	Message       string      `json:"message,omitempty"`
	ObservedAt    int64       `json:"observed_at"`
	PID           int         `json:"-"`
	Identity      *legacyHost `json:"-"`
}

type backendPackage struct{ status, version, architecture string }

func installedBackendPackages(output string) map[string]backendPackage {
	result := map[string]backendPackage{}
	for _, line := range strings.Split(output, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 4 || f[1] != "installed" || f[2] == "" {
			continue
		}
		name := strings.SplitN(f[0], ":", 2)[0]
		result[name] = backendPackage{f[1], f[2], f[3]}
	}
	return result
}

var backendCache struct {
	sync.Mutex
	at    time.Time
	value BackendObservation
}

func InvalidateBackendStatus() {
	backendCache.Lock()
	backendCache.at = time.Time{}
	backendCache.Unlock()
}

// BackendStatus does not initialize journals, publish triggers or advance a
// migration. One cache serves every cron row and page on the server.
func BackendStatus(ctx context.Context) BackendObservation {
	backendCache.Lock()
	defer backendCache.Unlock()
	if !backendCache.at.IsZero() && time.Since(backendCache.at) < 30*time.Second {
		return backendCache.value
	}
	result := observeBackend(ctx)
	if ctx.Err() != nil {
		return result
	}
	if result.State == "active" {
		if host, err := qualifyObservedBackend(ctx, result); err != nil {
			result.Qualification = "unqualified"
			result.Code = diagnosticCode(err)
			result.Message = err.Error()
		} else {
			result.Qualification = "qualified"
			result.Identity = &host
		}
	}
	if ctx.Err() != nil {
		return result
	}
	backendCache.value = result
	backendCache.at = time.Now()
	return result
}

type diagnosticError struct{ code, message string }

func (e *diagnosticError) Error() string    { return e.message }
func diagnostic(code, message string) error { return &diagnosticError{code, message} }
func diagnosticCode(err error) string {
	if e, ok := err.(*diagnosticError); ok {
		return e.code
	}
	return "backend_unqualified"
}

func observeBackend(ctx context.Context) BackendObservation {
	r := BackendObservation{Engine: "unknown", State: "unknown", Qualification: "unqualified", ObservedAt: time.Now().Unix()}
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/dpkg-query", "-W", "-f=${binary:Package}\t${db:Status-Status}\t${Version}\t${Architecture}\n")
	packages := installedBackendPackages(out)
	// Query the database once without named absent packages: dpkg-query can
	// return nonzero (and no usable Runner output) for those residual records.
	if err != nil {
		r.Code = "backend_unknown"
		r.Message = "Unable to inspect the installed scheduling engine."
		return r
	}
	var providers []BackendObservation
	for _, name := range []string{"cron", "cronie"} {
		unit := name + ".service"
		out, e := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", unit, "--property=Id,LoadState,ActiveState,MainPID,UnitFileState")
		if e != nil && !strings.Contains(out, "LoadState=not-found") {
			r.Code = "backend_unknown"
			r.Message = "Unable to inspect the scheduling service."
			return r
		}
		p := properties(out)
		pkg, installed := packages[name]
		if p["LoadState"] == "not-found" && !installed {
			continue
		}
		pid, _ := strconv.Atoi(p["MainPID"])
		state := "inactive"
		if p["ActiveState"] == "active" && pid > 1 {
			state = "active"
		}
		if p["UnitFileState"] == "masked" {
			state = "masked"
		}
		providers = append(providers, BackendObservation{Engine: name, State: state, Unit: p["Id"], Enabled: p["UnitFileState"], Package: name, Version: pkg.version, Architecture: pkg.architecture, PID: pid, ObservedAt: r.ObservedAt, Qualification: "unqualified"})
	}
	active := []BackendObservation{}
	for _, p := range providers {
		if p.State == "active" {
			active = append(active, p)
		}
	}
	// Detect duplicate or unmanaged scheduler processes independently of unit
	// names. This is discovery only, never a proof that historical work drained.
	processes, readErr := schedulerProcesses()
	if readErr != nil {
		r.Code = "backend_unknown"
		r.Message = "Unable to inspect scheduling processes."
		return r
	}
	if len(active) == 1 {
		processes, readErr = independentSchedulerProcesses(active[0].PID, processes)
		if readErr != nil {
			r.Code = "backend_unknown"
			r.Message = "Unable to verify scheduling process identities."
			return r
		}
	}
	if len(active) > 1 || len(processes) > 1 {
		r.State = "ambiguous"
		r.Code = "backend_ambiguous"
		r.Message = "Multiple scheduling engines are running. Review them before PHP maintenance."
		return r
	}
	if len(active) == 1 {
		r = active[0]
		if len(processes) != 1 || !processes[r.PID] {
			r.State = "ambiguous"
			r.Code = "backend_ambiguous"
			r.Message = "The running scheduler does not match its service. Review it before PHP maintenance."
		}
		return r
	}
	if len(processes) > 0 || packages["systemd-cron"].version != "" || packages["bcron"].version != "" || packages["fcron"].version != "" {
		r.Engine = "custom"
		r.State = "unqualified"
		r.Code = "backend_unqualified"
		r.Message = "A different scheduling engine is installed or running. Existing schedules are preserved; automatic PHP maintenance needs a supported adapter."
		return r
	}
	if len(providers) == 1 {
		r = providers[0]
		r.Code = "backend_inactive"
		r.Message = "The scheduling service is not running. Review its status before PHP maintenance."
		return r
	}
	if len(providers) > 1 {
		r.State = "ambiguous"
		r.Code = "backend_ambiguous"
		r.Message = "More than one scheduling provider is configured. Review which provider should run."
		return r
	}
	r.Engine = "none"
	r.State = "missing"
	r.Code = "backend_missing"
	r.Message = "No scheduling engine was found. Scheduled jobs cannot be verified; configure a scheduler before PHP maintenance."
	return r
}

// Forked job supervisors retain comm=crond. They are workloads in the daemon's
// domain, not additional scheduling engines. Filtering them is discovery only;
// the drain proof continues counting the entire hierarchy without subtraction.
func independentSchedulerProcesses(main int, processes map[int]bool) (map[int]bool, error) {
	key, err := startKey(main)
	if err != nil {
		return nil, err
	}
	exe, err := os.Stat(fmt.Sprintf("/proc/%d/exe", main))
	if err != nil {
		return nil, err
	}
	group, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", main))
	if err != nil {
		return nil, err
	}
	result := map[int]bool{}
	for pid := range processes {
		if pid == main {
			result[pid] = true
			continue
		}
		childExe, e := os.Stat(fmt.Sprintf("/proc/%d/exe", pid))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		childGroup, e := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if !os.SameFile(exe, childExe) || string(group) != string(childGroup) || !processDescendsFrom(pid, main) {
			result[pid] = true
		}
	}
	if current, err := startKey(main); err != nil || current != key {
		return nil, fmt.Errorf("Scheduling daemon changed during process inspection")
	}
	return result, nil
}

func processDescendsFrom(pid, ancestor int) bool {
	for depth := 0; depth < 64 && pid > 1; depth++ {
		if pid == ancestor {
			return true
		}
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			return false
		}
		i := strings.LastIndex(string(b), ")")
		if i < 0 {
			return false
		}
		fields := strings.Fields(string(b)[i+1:])
		if len(fields) < 2 {
			return false
		}
		pid, err = strconv.Atoi(fields[1])
		if err != nil {
			return false
		}
	}
	return false
}

func schedulerProcesses() (map[int]bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	result := map[int]bool{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		switch strings.TrimSpace(string(b)) {
		case "cron", "crond", "fcron", "bcron-sched":
			result[pid] = true
		}
	}
	return result, nil
}

func qualifyObservedBackend(ctx context.Context, observation BackendObservation) (legacyHost, error) {
	if observation.State != "active" {
		return legacyHost{}, diagnostic(observation.Code, observation.Message)
	}
	switch observation.Engine {
	case "cron":
		return qualifyLegacyHost(ctx)
	case "cronie":
		return qualifyCronieHost(ctx, observation)
	}
	return legacyHost{}, diagnostic("backend_unqualified", fmt.Sprintf("Automatic PHP maintenance does not yet support the %s scheduling engine.", observation.Engine))
}

var backendObserver = observeBackend

func qualifyBackendHost(ctx context.Context) (legacyHost, error) {
	return qualifyObservedBackend(ctx, backendObserver(ctx))
}
