package scheduler

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// The existing distribution candidate validates effective unit/cgroup/image.
// Additionally inspect actual mapped vendor code and loader-affecting daemon
// environment. Do not use ldd or run a reviewed binary to discover its closure.
func (p *pamCandidateInspection) daemonLoaderInputs(pid int, binarySHA string) error {
	if pid <= 1 {
		return errors.New("candidate requires its identified live cron daemon")
	}
	before, err := startKey(pid)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil || !strings.Contains(string(raw), "Uid:\t0\t0\t0\t0\n") {
		return errors.New("candidate cron daemon identity is not root")
	}
	if sum, e := fileDigest(fmt.Sprintf("/proc/%d/exe", pid)); e != nil || sum != binarySHA {
		return errors.New("candidate cron mapped executable differs")
	}
	raw, err = os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return err
	}
	for _, entry := range strings.Split(string(raw), "\x00") {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "LD_") || strings.HasPrefix(key, "SYSTEMD_NSS_") || strings.HasPrefix(key, "SYSTEMD_USERDB_") || key == "SYSTEMD_BYPASS_USERDB" || key == "SYSTEMD_ONLY_USERDB" || strings.HasPrefix(key, "NSS_") || key == "GLIBC_TUNABLES" || key == "GCONV_PATH" || key == "LOCPATH" || key == "NLSPATH" {
			return errors.New("candidate daemon has an unreviewed loader/NSS environment override")
		}
	}
	maps, err := os.ReadFile(fmt.Sprintf("/proc/%d/maps", pid))
	if err != nil {
		return err
	}
	visiting := map[string]bool{}
	for _, line := range strings.Split(string(maps), "\n") {
		v := strings.Fields(line)
		if len(v) < 2 || !strings.Contains(v[1], "x") {
			continue
		}
		if len(v) == 6 && (v[5] == "[vdso]" || v[5] == "[vsyscall]") {
			continue
		}
		if len(v) < 6 || !strings.HasPrefix(v[5], "/") {
			return errors.New("candidate daemon has anonymous executable mappings")
		}
		if len(v) != 6 {
			return errors.New("candidate daemon maps replaced or ambiguous code paths")
		}
		path := v[5]
		if err = p.elfClosure(path, visiting); err != nil {
			return err
		}
		canonical, e := p.canonical(path)
		if e != nil {
			return e
		}

		if !pamCandidateMappedIdentity(p.observed[canonical], v[3], v[4]) {
			return errors.New("candidate daemon mapped image no longer matches protected vendor device/inode")
		}

	}
	after, err := startKey(pid)
	if err != nil || after != before {
		return errors.New("candidate cron daemon changed during loader inspection")
	}
	return nil
}

func pamCandidateMappedIdentity(observation pamCandidateObservation, device, inodeText string) bool {
	inode, err := strconv.ParseUint(inodeText, 10, 64)
	if err != nil || observation.Ino != inode {
		return false
	}
	parts := strings.Split(device, ":")
	if len(parts) != 2 {
		return false
	}
	major, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return false
	}
	minor, err := strconv.ParseUint(parts[1], 16, 32)
	if err != nil {
		return false
	}
	return uint64(unix.Major(observation.Dev)) == major && uint64(unix.Minor(observation.Dev)) == minor
}
