package packages

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ProcessBootKey uses the same boot/start identity as runtime inventories.
func ProcessBootKey(pid int) (string, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	end := strings.LastIndexByte(string(raw), ')')
	if end < 0 {
		return "", errors.New("Invalid process identity")
	}
	fields := strings.Fields(string(raw)[end+1:])
	if len(fields) < 20 {
		return "", errors.New("Incomplete process identity")
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(boot)) + ":" + fields[19], nil
}

// VerifyLocalChildIdentity does not authorize an execution domain by itself.
// Its caller must authenticate the unit, durable invocation, current helper
// role and actual shared-lease owner before and after this observation.
func VerifyLocalChildIdentity(pid int, key, group string, uid, gid uint32) error {
	if pid <= 1 || uid == 0 || key == "" {
		return errors.New("Invalid non-root scheduled-job process")
	}
	before, err := ProcessBootKey(pid)
	if err != nil {
		return err
	}
	if before != key {
		return errors.New("Scheduled-job process identity changed")
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return err
	}
	found := 0
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "Uid:" && fields[0] != "Gid:" {
			continue
		}
		if len(fields) != 5 {
			return errors.New("Incomplete scheduled-job credentials")
		}
		want := uid
		if fields[0] == "Gid:" {
			want = gid
		}
		for _, value := range fields[1:] {
			n, e := strconv.ParseUint(value, 10, 32)
			if e != nil || n != uint64(want) {
				return errors.New("Scheduled-job process credentials changed")
			}
		}
		found++
	}
	if found != 2 {
		return errors.New("Scheduled-job credentials unavailable")
	}
	cgroup, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(cgroup)) != "0::"+group {
		return errors.New("Scheduled-job process moved execution domains")
	}
	after, err := ProcessBootKey(pid)
	if err != nil {
		return err
	}
	if after != before {
		return errors.New("Scheduled-job process was replaced")
	}
	return nil
}

func VerifySharedGuardProcess(pid int, helper, group, boot string) error {
	before, err := ProcessBootKey(pid)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(before, boot+":") {
		return errors.New("Scheduled-job guard boot changed")
	}
	expected, err := os.Stat(helper)
	if err != nil {
		return err
	}
	actual, err := os.Stat(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return err
	}
	if !os.SameFile(expected, actual) {
		return errors.New("Scheduled-job guard executable changed")
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(raw)) != "0::"+group {
		return errors.New("Scheduled-job guard left its execution domain")
	}
	if err = VerifySharedLifecycleOwner(pid); err != nil {
		return err
	}
	after, err := ProcessBootKey(pid)
	if err != nil {
		return err
	}
	if after != before {
		return errors.New("Scheduled-job guard was replaced")
	}
	actual, err = os.Stat(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return err
	}
	if !os.SameFile(expected, actual) {
		return errors.New("Scheduled-job guard role changed")
	}
	return nil
}
