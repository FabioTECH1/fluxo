package maintenancetask

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"fluxo/internal/syscmd"
	"golang.org/x/sys/unix"
)

// ExecuteApplication is a fixed root trampoline, not a generic privileged
// command runner. Its parent remains the lifecycle guard. It consumes a second
// one-shot authority, masks service-manager sockets only for the application,
// then executes the recorded shell command after dropping all root credentials.
func ExecuteApplication(ctx context.Context, unit string) error {
	if !validUnit(unit) {
		return errors.New("Invalid application invocation")
	}
	unlock, err := gate(ctx, false)
	if err != nil {
		return err
	}
	j, err := read()
	if err != nil {
		unlock()
		return err
	}
	v, ok := j.Invocations[unit]
	if !ok || v.Status != "started" || v.ApplicationStarted || v.Registration.Contract.Kind != ApplicationJob {
		unlock()
		return errors.New("Application launch authority is absent or already consumed")
	}
	r, ok := j.Entries[v.Registration.Contract.ID]
	if !ok || !r.Enabled || r.Identity != v.Registration.Identity {
		unlock()
		return errors.New("Application registration was retired before launch")
	}
	group, err := os.ReadFile("/proc/self/cgroup")
	if err != nil || strings.TrimSpace(string(group)) != "0::/system.slice/"+unit {
		unlock()
		return errors.New("Application trampoline left its recorded execution domain")
	}
	p, err := inspectInvocation(ctx, v)
	if err != nil {
		unlock()
		return err
	}
	if p["MainPID"] != fmt.Sprint(os.Getppid()) {
		unlock()
		return errors.New("Application trampoline is not a child of its lifecycle guard")
	}
	if err = liveGuard(v, p); err != nil {
		unlock()
		return err
	}
	self, err := os.Stat("/proc/self/exe")
	if err != nil {
		unlock()
		return err
	}
	expected, err := os.Stat(r.Helper)
	if err != nil || !os.SameFile(self, expected) {
		unlock()
		return errors.New("Application trampoline executable changed")
	}
	parentNS, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/mnt", os.Getppid()))
	if err != nil {
		unlock()
		return err
	}
	ownNS, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil || parentNS == ownNS {
		unlock()
		return errors.New("Application trampoline requires its own mount namespace")
	}
	if err = verifyRegistration(ctx, r); err == nil {
		err = withDatabase(ctx, r, true)
	}
	if err != nil {
		unlock()
		return err
	}
	v.ApplicationStarted = true
	j.Invocations[unit] = v
	err = save(j)
	unlock()
	if err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err = unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return err
	}
	if err = maskApplicationManagers(); err != nil {
		return err
	}
	command, input := cronCommand(r.Contract.Command)
	return syscmd.RunTrackedApplicationInput(r.Contract.User, r.Contract.Directory, input, "/bin/sh", "-c", command)
}

func maskApplicationManagers() error {
	dir := filepath.Join(stateRoot, "application-denied-directory")
	if err := os.Mkdir(dir, 0000); err != nil && !os.IsExist(err) {
		return err
	}
	file := filepath.Join(stateRoot, "application-denied-file")
	fd, err := unix.Open(file, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0000)
	if err == nil {
		unix.Close(fd)
	} else if !errors.Is(err, unix.EEXIST) {
		return err
	}
	for _, path := range []string{dir, file} {
		i, e := os.Lstat(path)
		if e != nil {
			return e
		}
		s := i.Sys().(*syscall.Stat_t)
		if s.Uid != 0 || i.Mode().Perm() != 0 || i.Mode()&os.ModeSymlink != 0 || (path == dir && !i.IsDir()) || (path == file && (!i.Mode().IsRegular() || s.Nlink != 1 || i.Size() != 0)) {
			return errors.New("Application service-manager mask changed")
		}
	}
	for _, target := range []string{"/run/user", "/run/systemd/private", "/run/dbus/system_bus_socket"} {
		st, e := os.Lstat(target)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return errors.New("Service-manager endpoint became a symlink")
		}
		source := file
		if st.IsDir() {
			source = dir
		}
		if e = unix.Mount(source, target, "", unix.MS_BIND, ""); e != nil {
			return e
		}
		if e = unix.Mount("", target, "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY|unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, ""); e != nil {
			return e
		}
	}
	return nil
}
