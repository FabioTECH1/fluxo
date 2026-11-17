package executiondomain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const SchedulerBootstrapContract = "scheduler-bootstrap-drop-exec-v1"
const SchedulerWorkerCapabilities uint64 = (1 << unix.CAP_SETUID) | (1 << unix.CAP_SETGID) | (1 << unix.CAP_DAC_READ_SEARCH)
const SchedulerBootstrapCapabilities uint64 = SchedulerWorkerCapabilities | (1 << unix.CAP_SYS_PTRACE) | (1 << unix.CAP_SETPCAP)

type KernelCredentials struct {
	UID             [4]uint32 `json:"uid"`
	GID             [4]uint32 `json:"gid"`
	Inheritable     uint64    `json:"inheritable"`
	Permitted       uint64    `json:"permitted"`
	Effective       uint64    `json:"effective"`
	Bounding        uint64    `json:"bounding"`
	Ambient         uint64    `json:"ambient"`
	NoNewPrivileges int       `json:"no_new_privileges"`
}

func ParseKernelCredentials(raw []byte) (KernelCredentials, error) {
	var c KernelCredentials
	seen := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(value)
		switch key {
		case "Uid", "Gid":
			if seen[key] || len(f) != 4 {
				return c, errors.New("invalid kernel credential IDs")
			}
			var ids [4]uint32
			for i, s := range f {
				n, e := strconv.ParseUint(s, 10, 32)
				if e != nil {
					return c, e
				}
				ids[i] = uint32(n)
			}
			if key == "Uid" {
				c.UID = ids
			} else {
				c.GID = ids
			}
			seen[key] = true
		case "CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb", "NoNewPrivs":
			if seen[key] || len(f) != 1 {
				return c, errors.New("invalid kernel capability fields")
			}
			seen[key] = true
			base := 16
			if key == "NoNewPrivs" {
				base = 10
			}
			n, e := strconv.ParseUint(f[0], base, 64)
			if e != nil {
				return c, e
			}
			switch key {
			case "CapInh":
				c.Inheritable = n
			case "CapPrm":
				c.Permitted = n
			case "CapEff":
				c.Effective = n
			case "CapBnd":
				c.Bounding = n
			case "CapAmb":
				c.Ambient = n
			case "NoNewPrivs":
				c.NoNewPrivileges = int(n)
			}
		}
	}
	if len(seen) != 8 {
		return c, errors.New("incomplete kernel credential evidence")
	}
	return c, nil
}
func (c KernelCredentials) RequireRootCapabilities(mask uint64) error {
	if c.UID != [4]uint32{} || c.GID != [4]uint32{} || c.Permitted != mask || c.Effective != mask || c.Bounding != mask || c.Inheritable != 0 || c.Ambient != 0 || c.NoNewPrivileges != 1 {
		return errors.New("kernel execution credentials do not match the fixed Scheduler phase")
	}
	return nil
}
func ReadSchedulerThreadCredentials() (KernelCredentials, error) {
	b, e := os.ReadFile(fmt.Sprintf("/proc/self/task/%d/status", unix.Gettid()))
	if e != nil {
		return KernelCredentials{}, e
	}
	return ParseKernelCredentials(b)
}
func VerifySchedulerWorkerThreads() (map[int]KernelCredentials, error) {
	bits, e := unix.PrctlRetInt(unix.PR_GET_SECUREBITS, 0, 0, 0, 0)
	if e != nil || bits != 0 {
		return nil, errors.New("Scheduler worker securebits changed")
	}
	for tries := 0; tries < 4; tries++ {
		entries, e := os.ReadDir("/proc/self/task")
		if e != nil {
			return nil, e
		}
		out := map[int]KernelCredentials{}
		retry := false
		for _, entry := range entries {
			tid, e := strconv.Atoi(entry.Name())
			if e != nil || tid <= 0 {
				return nil, errors.New("invalid worker thread identity")
			}
			raw, e := os.ReadFile("/proc/self/task/" + entry.Name() + "/status")
			if os.IsNotExist(e) {
				retry = true
				break
			}
			if e != nil {
				return nil, e
			}
			c, e := ParseKernelCredentials(raw)
			if e != nil {
				return nil, e
			}
			if e = c.RequireRootCapabilities(SchedulerWorkerCapabilities); e != nil {
				return nil, e
			}
			out[tid] = c
		}
		if !retry && len(out) > 0 {
			return out, nil
		}
	}
	return nil, errors.New("worker thread evidence did not stabilize")
}

type PinnedExecutionImage struct {
	FD            int
	SHA256        string
	Device, Inode uint64
}

// PinSelfExecutable opens the actual executing image, validates its protected
// named file and digest, and retains a raw CLOEXEC descriptor without a finalizer.
func PinSelfExecutable(expectedSHA string) (*PinnedExecutionImage, error) {
	path, e := os.Executable()
	if e != nil {
		return nil, e
	}
	named, e := readProtected(path, 128<<20)
	if e != nil {
		return nil, e
	}
	sum := sha256.Sum256(named)
	want := hex.EncodeToString(sum[:])
	if expectedSHA != "" && want != expectedSHA {
		return nil, errors.New("bootstrap image digest changed")
	}
	fd, e := unix.Open("/proc/self/exe", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	fail := func(e error) (*PinnedExecutionImage, error) { unix.Close(fd); return nil, e }
	var st, ns unix.Stat_t
	if e = unix.Fstat(fd, &st); e != nil {
		return fail(e)
	}
	if e = unix.Lstat(path, &ns); e != nil {
		return fail(e)
	}
	if !sameFileObservation(st, ns) {
		return fail(errors.New("named and executing bootstrap images differ"))
	}
	h := sha256.New()
	buf := make([]byte, 64<<10)
	var off int64
	for {
		n, err := unix.Pread(fd, buf, off)
		if err != nil {
			return fail(err)
		}
		if n == 0 {
			break
		}
		off += int64(n)
		if off > 128<<20 {
			return fail(errors.New("executing image exceeds budget"))
		}
		h.Write(buf[:n])
	}
	var after unix.Stat_t
	if e = unix.Fstat(fd, &after); e != nil || !sameFileObservation(st, after) || hex.EncodeToString(h.Sum(nil)) != want {
		return fail(errors.New("pinned executing image changed"))
	}
	return &PinnedExecutionImage{FD: fd, SHA256: want, Device: uint64(st.Dev), Inode: st.Ino}, nil
}
func (p *PinnedExecutionImage) Close() {
	if p != nil && p.FD >= 0 {
		unix.Close(p.FD)
		p.FD = -1
	}
}

// DropAndExecSchedulerWorker NEVER returns, including on any failed transition.
// Only the locked current thread drops credentials. Successful exec destroys all
// privileged sibling Go threads; failure uses exit_group rather than returning
// into a runtime whose siblings may still hold bootstrap capabilities.
func DropAndExecSchedulerWorker(p *PinnedExecutionImage, leaseFD int, argv, env []string) {
	runtime.LockOSThread()
	execPath := ""
	if p != nil {
		execPath = fmt.Sprintf("/proc/self/fd/%d", p.FD)
	}
	if p == nil || p.FD < 3 || leaseFD < 3 || p.FD == leaseFD {
		bootstrapFatal()
	}
	c, e := ReadSchedulerThreadCredentials()
	if e != nil || c.RequireRootCapabilities(SchedulerBootstrapCapabilities) != nil {
		bootstrapFatal()
	}
	bits, e := unix.PrctlRetInt(unix.PR_GET_SECUREBITS, 0, 0, 0, 0)
	if e != nil || bits != 0 {
		bootstrapFatal()
	}
	if e = unix.CloseRange(3, uint(^uint32(0)), unix.CLOSE_RANGE_CLOEXEC); e != nil {
		bootstrapFatal()
	}
	if _, e = unix.FcntlInt(uintptr(leaseFD), unix.F_SETFD, 0); e != nil {
		bootstrapFatal()
	}
	if e = unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); e != nil {
		bootstrapFatal()
	}
	// SETPCAP remains effective until BOTH bounding extras have been removed.
	for _, capability := range []int{unix.CAP_SYS_PTRACE, unix.CAP_SETPCAP} {
		if e = unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(capability), 0, 0, 0); e != nil {
			bootstrapFatal()
		}
	}
	data := [2]unix.CapUserData{{Effective: uint32(SchedulerWorkerCapabilities), Permitted: uint32(SchedulerWorkerCapabilities)}}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	if e = unix.Capset(&header, &data[0]); e != nil {
		bootstrapFatal()
	}
	if e = verifyDroppedSchedulerThread(); e != nil {
		bootstrapFatal()
	}
	// syscall.Exec uses Go1.26.3 runtime_BeforeExec to block thread creation.
	// /proc/self/fd pins this inode even if a mutable pathname is replaced.
	e = syscall.Exec(execPath, argv, env)
	_ = e
	bootstrapFatal()
}
func verifyDroppedSchedulerThread() error {
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	data := [2]unix.CapUserData{}
	if e := unix.Capget(&header, &data[0]); e != nil {
		return e
	}
	if data[0].Effective != uint32(SchedulerWorkerCapabilities) || data[0].Permitted != uint32(SchedulerWorkerCapabilities) || data[0].Inheritable != 0 || data[1] != (unix.CapUserData{}) {
		return errors.New("capset readback differs")
	}
	for capability := 0; capability < 64; capability++ {
		b, e := unix.PrctlRetInt(unix.PR_CAPBSET_READ, uintptr(capability), 0, 0, 0)
		if errors.Is(e, unix.EINVAL) {
			break
		}
		if e != nil {
			return e
		}
		want := 0
		if SchedulerWorkerCapabilities&(uint64(1)<<capability) != 0 {
			want = 1
		}
		if b != want {
			return errors.New("bounding capability readback differs")
		}
		a, e := unix.PrctlRetInt(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_IS_SET, uintptr(capability), 0, 0)
		if e != nil || a != 0 {
			return errors.New("ambient capability readback differs")
		}
	}
	n, e := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	if e != nil || n != 1 {
		return errors.New("NoNewPrivileges readback differs")
	}
	bits, e := unix.PrctlRetInt(unix.PR_GET_SECUREBITS, 0, 0, 0, 0)
	if e != nil || bits != 0 {
		return errors.New("securebits readback differs")
	}
	return nil
}
func bootstrapFatal() {
	// Even diagnostic I/O can block on a full pipe. Termination cannot wait
	// while sibling threads still retain the bootstrap capability set.
	_, _, _ = syscall.RawSyscall(syscall.SYS_EXIT_GROUP, 125, 0, 0)
	for {
	}
}
