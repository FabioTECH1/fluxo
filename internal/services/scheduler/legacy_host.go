package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/syscmd"
)

// Qualification is deliberately narrow. These are the Ubuntu noble cron
// 3.0pl1-184ubuntu2 binaries and standard PAM/unit configuration inspected with
// its fully patched source. See legacy_migration.md for the ordering proof.
var legacyFiles = map[string]string{
	"/etc/pam.d/cron":                          "50a7062347b76fe4a429065547f2b53ffe49ac7d896f6b238010a3ee90446a84",
	"/etc/pam.d/common-session-noninteractive": "1bd1ef2b6241fc8b3c97bbaf57e5c6038c96c275e30c62ebee6eec15701e2e07",
	"/etc/pam.d/common-auth":                   "628197de9e50b6be37421b04a67f07924f515e0b0f4c06aed9fea953d20ed6e6",
	"/etc/pam.d/common-account":                "aa8a63d72e79399b6c51ebe4e9f828c954145a799eb4b8f3224724f51cbb9fac",
	"/usr/lib/systemd/system/cron.service":     "bbcd656457d22e12d8983ed0c253b5bd8cc6b12308fd16d9324400040de68e6f",
	"/etc/default/cron":                        "6a8f0b25001017f55a227be357cd418e46f514acd58c069d17a78c3072d9d569",
}

type legacyHost struct {
	Boot       string `json:"boot"`
	PID        int    `json:"pid"`
	Start      string `json:"start"`
	GroupInode uint64 `json:"group_inode"`
	// Empty means the original schema-2 Ubuntu cron identity. Cronie identities
	// are written only to the separate backend proof journal, never core state.
	Backend       string `json:"backend,omitempty"`
	Group         string `json:"group,omitempty"`
	Configuration string `json:"configuration,omitempty"`
}

func (h legacyHost) controlGroup() string {
	if h.Backend == "cronie-noble-1" {
		return h.Group
	}
	return "/system.slice/cron.service"
}

func fileDigest(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func qualifyLegacyHost(ctx context.Context) (legacyHost, error) {
	var host legacyHost
	unsupported := errors.New("Fluxo cannot verify earlier Scheduler jobs with this cron configuration; review Scheduler migration before updating PHP")
	want := map[string]string{"amd64": "6bd8593640af2413bce259fa0affc18dbf149892756ebe805bf316624f8b590f", "arm64": "613eacb4e7b0528b6b643955fdfb877f226d8bd11349dc7c8e84543290dd21de"}[runtime.GOARCH]
	if want == "" {
		return host, unsupported
	}
	for path, hash := range legacyFiles {
		if err := trustedFile(path); err != nil {
			return host, unsupported
		}
		if got, err := fileDigest(path); err != nil || got != hash {
			return host, unsupported
		}
	}
	if err := trustedFile("/usr/sbin/cron"); err != nil {
		return host, unsupported
	}
	if got, err := fileDigest("/usr/sbin/cron"); err != nil || got != want {
		return host, unsupported
	}
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", "cron.service", "--property=MainPID,ControlGroup,Delegate,DropInPaths,ActiveState,ExecStart")
	if err != nil {
		return host, err
	}
	p := properties(out)
	host.PID, err = strconv.Atoi(p["MainPID"])
	if err != nil || host.PID <= 1 || p["ControlGroup"] != "/system.slice/cron.service" || p["Delegate"] != "no" || p["DropInPaths"] != "" || p["ActiveState"] != "active" {
		return host, unsupported
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", host.PID))
	if err != nil || string(cmdline) != "/usr/sbin/cron\x00-f\x00-P\x00" {
		return host, unsupported
	}
	if got, err := fileDigest(fmt.Sprintf("/proc/%d/exe", host.PID)); err != nil || got != want {
		return host, unsupported
	}
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", host.PID))
	if err != nil || !strings.Contains(string(status), "Threads:\t1\n") {
		return host, unsupported
	}
	group, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", host.PID))
	if err != nil || strings.TrimSpace(string(group)) != "0::/system.slice/cron.service" {
		return host, unsupported
	}
	host.Start, err = startKey(host.PID)
	if err != nil {
		return host, err
	}
	host.Boot, err = bootID()
	if err != nil {
		return host, err
	}
	var st syscall.Stat_t
	if err = syscall.Stat("/sys/fs/cgroup/system.slice/cron.service", &st); err != nil {
		return host, err
	}
	host.GroupInode = st.Ino
	return host, nil
}

func cronReceiptOrigin(host legacyHost) error {
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil || strings.TrimSpace(string(raw)) != "0::"+host.controlGroup() {
		return errors.New("Scheduler migration receipt did not originate in cron")
	}
	pid := os.Getppid()
	for i := 0; i < 12 && pid > 1; i++ {
		if pid == host.PID {
			key, err := startKey(pid)
			if err == nil && key == host.Start {
				return nil
			}
			break
		}
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			break
		}
		fields := strings.Fields(string(b)[strings.LastIndex(string(b), ")")+1:])
		if len(fields) < 2 {
			break
		}
		pid, err = strconv.Atoi(fields[1])
		if err != nil {
			break
		}
	}
	return errors.New("Scheduler migration receipt is not a child of the recorded cron daemon")
}
