package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/syscmd"
)

type backendAdapter struct {
	unit, group, executable, commandLine string
	fragment, environmentFile            string
	files                                map[string]string
	binaries                             map[string]string
}

// Ubuntu noble cronie 1.7.1-1, fetched from the official archive/ports. This
// adapter is independently qualified; an alias is not a containment proof.
var cronieAdapter = backendAdapter{
	fragment: "/usr/lib/systemd/system/cronie.service", environmentFile: "/etc/default/cronie",
	unit: "cronie.service", group: "/system.slice/cronie.service", executable: "/usr/sbin/crond", commandLine: "/usr/sbin/crond\x00-n\x00",
	binaries: map[string]string{"amd64": "a230dfc91820bc782134663b8a27520f506363ea8d9e0188421503f89d8453f7", "arm64": "c4dc96867f5435e4e488e63c60deca7653bbae6c132876ece22361b6d9662a8c"},
	files: map[string]string{
		"/usr/lib/systemd/system/cronie.service":   "d63def6ac1b4b89c2c7e4535a94a2e9f74a1006c9a19ef7fe9add7e839982627",
		"/etc/default/cronie":                      "65abfdedbee0d38444aaa896181951108b4a0b2756b859dd7e2566cad232a4dc",
		"/etc/pam.d/cronie":                        "181a408adfc59ee2e769b894ab7ab949b0bd85267abceedc49c515de745d6d6e",
		"/etc/pam.d/common-session-noninteractive": "1bd1ef2b6241fc8b3c97bbaf57e5c6038c96c275e30c62ebee6eec15701e2e07",
		"/etc/pam.d/common-auth":                   "628197de9e50b6be37421b04a67f07924f515e0b0f4c06aed9fea953d20ed6e6",
		"/etc/pam.d/common-account":                "aa8a63d72e79399b6c51ebe4e9f828c954145a799eb4b8f3224724f51cbb9fac",
	},
}

func qualifyCronieHost(ctx context.Context, observation BackendObservation) (legacyHost, error) {
	var h legacyHost
	unsupported := func(detail string) (legacyHost, error) {
		return h, diagnostic("backend_unqualified", "Cronie is running, but its "+detail+" is not qualified for automatic PHP maintenance. Existing schedules are preserved.")
	}
	// Runtime acceptance currently covers amd64. A matching arm64 package hash
	// alone does not qualify its execution/PAM/containment path.
	if runtime.GOARCH != "amd64" {
		return unsupported("runtime architecture")
	}
	if observation.Version != "1.7.1-1" || observation.Architecture != runtime.GOARCH {
		return unsupported("installed package")
	}
	if _, err := os.Stat("/sys/fs/selinux/enforce"); err == nil {
		return unsupported("SELinux configuration")
	} else if !os.IsNotExist(err) {
		return h, err
	}
	a := cronieAdapter
	identities := map[string]string{}
	for path, want := range a.files {
		if err := trustedFile(path); err != nil {
			return unsupported(path)
		}
		got, err := fileDigest(path)
		if err != nil || got != want {
			return unsupported(path)
		}
		identities[path] = got
	}
	want := a.binaries[runtime.GOARCH]
	if want == "" {
		return unsupported("architecture")
	}
	if err := trustedFile(a.executable); err != nil {
		return unsupported("executable")
	}
	if got, err := fileDigest(a.executable); err != nil || got != want {
		return unsupported("executable")
	}
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", a.unit, "--property=Id,MainPID,ControlGroup,Delegate,DropInPaths,ActiveState,ExecStart,Environment,EnvironmentFiles,User,FragmentPath,KillMode")
	if err != nil {
		return h, err
	}
	p := properties(out)
	h.PID, err = strconv.Atoi(p["MainPID"])
	if err != nil || h.PID <= 1 || p["Id"] != a.unit || p["ControlGroup"] != a.group || p["Delegate"] != "no" || p["DropInPaths"] != "" || p["ActiveState"] != "active" || p["Environment"] != "" || (p["User"] != "" && p["User"] != "root") || p["KillMode"] != "process" {
		return unsupported("effective service configuration")
	}
	if p["FragmentPath"] != cronieAdapter.fragment && !(cronieAdapter.fragment == "/usr/lib/systemd/system/cronie.service" && p["FragmentPath"] == "/lib/systemd/system/cronie.service") {
		return unsupported("service fragment")
	}
	if !strings.HasPrefix(p["ExecStart"], "{ path="+a.executable+" ; argv[]="+a.executable+" -n $DAEMON_ARGS ;") || p["EnvironmentFiles"] != a.environmentFile+" (ignore_errors=yes)" {
		return unsupported("effective service command")
	}
	cmd, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", h.PID))
	if err != nil || string(cmd) != a.commandLine {
		return unsupported("running command")
	}
	if got, err := fileDigest(fmt.Sprintf("/proc/%d/exe", h.PID)); err != nil || got != want {
		return unsupported("running executable")
	}
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", h.PID))
	if err != nil || !strings.Contains(string(status), "Threads:\t1\n") || !strings.Contains(string(status), "Uid:\t0\t0\t0\t0\n") {
		return unsupported("daemon identity")
	}
	group, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", h.PID))
	if err != nil || strings.TrimSpace(string(group)) != "0::"+a.group {
		return unsupported("execution domain")
	}
	h.Start, err = startKey(h.PID)
	if err != nil {
		return h, err
	}
	h.Boot, err = bootID()
	if err != nil {
		return h, err
	}
	var st syscall.Stat_t
	if err = syscall.Stat("/sys/fs/cgroup"+a.group, &st); err != nil {
		return h, err
	}
	h.GroupInode = st.Ino
	h.Backend = "cronie-noble-1"
	h.Group = a.group
	identities["executable"] = want
	identities["unit"] = a.unit
	identities["command"] = a.commandLine
	b, _ := json.Marshal(identities)
	h.Configuration = hashBytes(b)
	return h, nil
}
