package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/syscmd"
)

// Only metadata/digests are exported. Unknown custom unit/environment contents,
// PAM arguments and application commands never enter the diagnostic artifact.
func currentHostEvidence(ctx context.Context, b BackendObservation) map[string]any {
	r := map[string]any{"engine": b.Engine, "state": b.State, "observed_at": b.ObservedAt, "consistency": "unknown", "consistency_scope": []string{"boot", "pid_start", "service_properties", "cgroup_inode", "file_metadata_and_available_digests"}}
	boot, err := bootID()
	if err != nil {
		r["boot_error"] = err.Error()
	} else {
		r["boot"] = boot
	}
	if uptime, err := os.ReadFile("/proc/uptime"); err == nil {
		f := strings.Fields(string(uptime))
		if len(f) > 0 {
			r["uptime_seconds"] = f[0]
		}
	} else {
		r["uptime"] = unknownEvidence(err)
	}
	if ticks, err := syscmd.Run(ctx, time.Second, "/usr/bin/getconf", "CLK_TCK"); err == nil {
		r["clock_ticks_per_second"] = strings.TrimSpace(ticks)
	} else {
		r["clock_ticks_per_second"] = unknownEvidence(err)
	}
	files := map[string]string{}
	if b.Engine == "cronie" {
		for path := range cronieAdapter.files {
			files[path] = ""
		}
		files[cronieAdapter.executable] = ""
	} else if b.Engine == "cron" {
		for path := range legacyFiles {
			files[path] = ""
		}
		files["/usr/sbin/cron"] = ""
	}
	const serviceFields = "--property=Id,Names,LoadState,ActiveState,SubState,UnitFileState,MainPID,ControlGroup,Delegate,FragmentPath,DropInPaths,ExecMainStartTimestampMonotonic,KillMode,Restart"
	var serviceBefore map[string]string
	if b.Unit != "" {
		out, e := syscmd.Run(ctx, 5*time.Second, "systemctl", "show", b.Unit, serviceFields)
		if e != nil {
			r["service"] = unknownEvidence(e)
		} else {
			serviceBefore = properties(out)
			r["service"] = serviceBefore
			for _, path := range append(strings.Fields(serviceBefore["DropInPaths"]), serviceBefore["FragmentPath"]) {
				if filepath.IsAbs(path) {
					files[path] = ""
				}
			}
		}
	}
	paths := []string{}
	for path := range files {
		paths = append(paths, path)
	}
	for i := 0; i < len(paths) && i < 64; i++ {
		path := paths[i]
		if !strings.HasPrefix(path, "/etc/pam.d/") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			name := ""
			if len(fields) >= 2 && fields[0] == "@include" {
				name = fields[1]
			} else if len(fields) >= 3 && (fields[1] == "include" || fields[1] == "substack") {
				name = fields[2]
			}
			if name == "" || strings.HasPrefix(name, ".") || filepath.Base(name) != name {
				continue
			}
			next := filepath.Join("/etc/pam.d", name)
			if _, seen := files[next]; !seen {
				files[next] = ""
				paths = append(paths, next)
			}
		}
	}
	metadata := []any{}
	beforeFiles := map[string]map[string]any{}
	for _, path := range paths {
		item := fileEvidence(path)
		beforeFiles[path] = item
		metadata = append(metadata, item)
	}
	r["configuration"] = metadata
	if serviceBefore == nil {
		return r
	}
	propertiesBefore := serviceBefore
	pid, e := strconv.Atoi(propertiesBefore["MainPID"])
	if e != nil || pid <= 1 {
		r["process_state"] = "unknown"
		if e == nil && pid == 0 {
			r["process_state"] = "absent"
		}
		return r
	}
	key, err := startKey(pid)
	if err != nil {
		r["process_error"] = err.Error()
		return r
	}
	r["pid"] = pid
	r["start_ticks"] = key
	if hash, err := fileDigest(fmt.Sprintf("/proc/%d/exe", pid)); err == nil {
		r["running_executable_sha256"] = hash
	} else {
		r["running_executable_sha256"] = unknownEvidence(err)
	}
	if status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, "Uid:") {
				r["uids"] = strings.Fields(line)[1:]
			} else if strings.HasPrefix(line, "Threads:") {
				r["threads"] = strings.TrimSpace(strings.TrimPrefix(line, "Threads:"))
			}
		}
	} else {
		r["process_status"] = unknownEvidence(err)
	}
	var groupPath string
	var groupInode uint64
	if group := propertiesBefore["ControlGroup"]; group != "" && strings.HasPrefix(group, "/system.slice/") && !strings.Contains(group, "..") {
		path := "/sys/fs/cgroup" + group
		groupPath = path
		if st, err := os.Stat(path); err == nil {
			groupInode = st.Sys().(*syscall.Stat_t).Ino
			r["cgroup_inode"] = groupInode
		} else {
			r["cgroup_inode"] = unknownEvidence(err)
		}
		if count, err := os.ReadFile(filepath.Join(path, "pids.current")); err == nil {
			r["hierarchical_pid_count"] = strings.TrimSpace(string(count))
		} else {
			r["hierarchical_pid_count"] = unknownEvidence(err)
		}
	}
	after, err := startKey(pid)
	afterBoot, bootErr := bootID()
	consistent := err == nil && bootErr == nil && after == key && afterBoot == boot && b.PID == pid
	out, serviceErr := syscmd.Run(ctx, 5*time.Second, "systemctl", "show", b.Unit, serviceFields)
	consistent = consistent && serviceErr == nil && reflect.DeepEqual(properties(out), propertiesBefore)
	st, groupErr := os.Stat(groupPath)
	consistent = consistent && groupErr == nil && groupInode != 0 && st.Sys().(*syscall.Stat_t).Ino == groupInode
	unknown := err != nil || bootErr != nil || serviceErr != nil || groupErr != nil
	for path, before := range beforeFiles {
		after := fileEvidence(path)
		consistent = consistent && reflect.DeepEqual(before, after)
		unknown = unknown || before["state"] == "unknown" || after["state"] == "unknown"
	}
	if consistent && !unknown {
		r["consistency"] = "consistent"
	} else if !unknown {
		r["consistency"] = "changed"
	}
	if err != nil {
		r["consistency_error"] = err.Error()
	}
	if serviceErr != nil {
		r["service_recheck"] = unknownEvidence(serviceErr)
	}
	if groupErr != nil {
		r["cgroup_recheck"] = unknownEvidence(groupErr)
	}
	if exe, e := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); e == nil {
		r["running_executable_path"] = exe
		r["executable_metadata"] = fileEvidence(exe)
		if owner, e := syscmd.Run(ctx, time.Second, "/usr/bin/dpkg-query", "-S", exe); e == nil {
			r["executable_package_ownership"] = strings.TrimSpace(owner)
		} else {
			r["executable_package_ownership"] = unknownEvidence(e)
		}
	} else {
		r["running_executable_path"] = unknownEvidence(e)
	}
	return r
}

func unknownEvidence(err error) map[string]any {
	return map[string]any{"state": "unknown", "error": err.Error()}
}

func fileEvidence(path string) map[string]any {
	r := map[string]any{"path": path, "state": "present"}
	st, err := os.Lstat(path)
	if err != nil {
		r["state"], r["error"] = "unknown", err.Error()
		if os.IsNotExist(err) {
			r["state"] = "absent"
		}
		return r
	}
	stat := st.Sys().(*syscall.Stat_t)
	r["mode"], r["uid"], r["gid"], r["links"] = st.Mode().String(), stat.Uid, stat.Gid, stat.Nlink
	if st.Mode()&os.ModeSymlink != 0 {
		if target, err := os.Readlink(path); err == nil {
			r["link_target"] = target
			r["target_digest"] = "not_collected"
		} else {
			r["state"], r["error"] = "unknown", err.Error()
		}
	}
	if st.Mode().IsRegular() {
		if hash, e := fileDigest(path); e == nil {
			r["sha256"] = hash
		} else {
			r["state"], r["error"] = "unknown", e.Error()
		}
	}
	return r
}
