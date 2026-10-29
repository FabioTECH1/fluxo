package packages

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// VerifySharedLifecycleOwner inspects metadata and the kernel's lock table.
// It never opens a native package-lock inode or acquires a second lease.
func VerifySharedLifecycleOwner(pid int) error {
	var st syscall.Stat_t
	if err := syscall.Lstat(LifecycleLockPath, &st); err != nil {
		return err
	}
	if st.Uid != 0 || st.Nlink != 1 || st.Mode&syscall.S_IFMT != syscall.S_IFREG || st.Mode&0022 != 0 {
		return errors.New("Lifecycle lock identity is unsafe")
	}
	b, err := os.ReadFile("/proc/locks")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) != 8 || f[1] != "FLOCK" || f[2] != "ADVISORY" || f[3] != "READ" || f[4] != strconv.Itoa(pid) {
			continue
		}
		device := strings.Split(f[5], ":")
		if len(device) != 3 {
			continue
		}
		major, e1 := strconv.ParseUint(device[0], 16, 32)
		minor, e2 := strconv.ParseUint(device[1], 16, 32)
		inode, e3 := strconv.ParseUint(device[2], 10, 64)
		if e1 == nil && e2 == nil && e3 == nil && major == uint64(unix.Major(uint64(st.Dev))) && minor == uint64(unix.Minor(uint64(st.Dev))) && inode == st.Ino && f[6] == "0" && f[7] == "EOF" {
			return nil
		}
	}
	return errors.New("Scheduled-job helper no longer owns its shared lifecycle lease")
}
