package syscmd

import (
	"context"
	"errors"
	"fluxo/internal/services/packages"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

var tinkerVersion = regexp.MustCompile(`^[578]\.[0-9]{1,2}$`)
var tinkerID = regexp.MustCompile(`^[a-f0-9]{32}$`)

// TinkerProcess owns a transient systemd execution domain, including detached
// descendants. Application output only travels through the PTY, never journald.
type TinkerProcess struct {
	PTY        *os.File
	Done       chan struct{}
	cmd        *exec.Cmd
	unit       string
	configDir  string
	once       sync.Once
	unregister func()
	closeErr   error
}

// TinkerOwner requires a real system service. BindsTo stops sessions when the
// panel exits or crashes; do not silently fall back to unmanaged processes.
func TinkerOwner() (string, error) {
	if os.Geteuid() != 0 {
		return "", errors.New("Tinker requires the installed root-managed Fluxo service")
	}
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		p := strings.SplitN(line, ":", 3)
		if len(p) == 3 && p[0] == "0" && p[1] == "" && strings.HasPrefix(p[2], "/system.slice/") {
			unit := strings.TrimPrefix(p[2], "/system.slice/")
			if regexp.MustCompile(`^[a-zA-Z0-9_.@-]+\.service$`).MatchString(unit) {
				return unit, nil
			}
		}
	}
	return "", errors.New("Tinker requires Fluxo to run in a systemd system service")
}

func StartTinker(id, version, directory string) (*TinkerProcess, error) {
	if !tinkerID.MatchString(id) || !tinkerVersion.MatchString(version) || !filepath.IsAbs(directory) {
		return nil, errors.New("Invalid Tinker launch settings")
	}
	owner, err := TinkerOwner()
	if err != nil {
		return nil, err
	}
	if err = TinkerPlatform(context.Background()); err != nil {
		return nil, err
	}
	credential, err := ResolveCredential("fluxo")
	if err != nil || credential.Uid == 0 || credential.Gid == 0 {
		return nil, errors.New("Tinker requires a non-root fluxo identity")
	}
	php := "/usr/bin/php" + version
	if _, err = os.Stat(php); err != nil {
		return nil, errors.New("The site's PHP CLI is not installed")
	}
	// This root-owned config contains no secrets and cannot be changed by PHP.
	dir, err := os.MkdirTemp("/run", "fluxo-tinker-config-")
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(dir)
		}
	}()
	if err = os.Chmod(dir, 0755); err != nil {
		return nil, err
	}
	config := `<?php
$fluxoPsyOptions = ['historyFile' => '/dev/null', 'historySize' => 1000, 'updateCheck' => 'never', 'pager' => 'cat'];
// Newer PsySH versions check PHP manual updates separately before the prompt.
// Keep session startup independent of outbound network availability while
// retaining compatibility with versions predating this option.
if (method_exists(\Psy\Configuration::class, 'setUpdateManualCheck')) {
    $fluxoPsyOptions['updateManualCheck'] = 'never';
}
return $fluxoPsyOptions;
`
	if err = os.WriteFile(filepath.Join(dir, "config.php"), []byte(config), 0444); err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(dir, "admission"), []byte("active"), 0600); err != nil {
		return nil, err
	}
	unit := "fluxo-tinker-" + id + ".service"
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	parentIdentity, err := tinkerProcessIdentity(os.Getpid())
	if err != nil {
		return nil, err
	}
	args := []string{"--quiet", "--wait", "--collect", "--pty", "--unit=" + unit,
		"--property=Type=exec", "--property=Restart=no", "--property=RemainAfterExit=no", "--property=User=root", "--property=Group=root",
		"--property=AmbientCapabilities=CAP_SETUID", "--property=SupplementaryGroups=", "--property=NoNewPrivileges=yes", "--property=CapabilityBoundingSet=CAP_SETUID CAP_SETGID CAP_CHOWN",
		"--property=RestrictSUIDSGID=yes", "--property=RestrictNamespaces=yes", "--property=PrivateTmp=yes",
		"--property=PrivateDevices=yes", "--property=ProtectControlGroups=yes", "--property=ProtectKernelTunables=yes",
		"--property=ProtectKernelModules=yes", "--property=ProtectKernelLogs=yes", "--property=ProtectClock=yes",
		"--property=Delegate=no", "--property=KillMode=control-group", "--property=TimeoutStopSec=3s",
		"--property=SendSIGKILL=yes", "--property=RuntimeMaxSec=900", "--property=MemoryMax=512M",
		"--property=LimitCORE=0", "--property=TasksMax=64", "--property=CPUQuota=100%", "--property=UMask=0077",
		"--property=BindsTo=" + owner, "--property=After=" + owner,
		"--property=RuntimeDirectory=fluxo-tinker-" + id + " " + filepath.Base(dir), "--property=RuntimeDirectoryMode=0755",
		"--working-directory=/", "--", executable, "tinker-execute", id, version, directory,
		strconv.Itoa(os.Getpid()), parentIdentity, dir}

	cmd := exec.Command("/usr/bin/systemd-run", args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "TERM=xterm-256color"}
	unregister := packages.RegisterTinkerDomain(id)
	f, err := startTinkerRelay(cmd)
	if err != nil {
		unregister()
		return nil, err
	}
	p := &TinkerProcess{PTY: f, Done: make(chan struct{}), cmd: cmd, unit: unit, configDir: dir, unregister: unregister}
	go func() { _ = cmd.Wait(); close(p.Done) }()
	cleanup = false

	return p, nil
}

func (p *TinkerProcess) Resize(rows, cols uint16) error {
	if rows < 2 || rows > 200 || cols < 10 || cols > 400 {
		return errors.New("Invalid terminal dimensions")
	}
	raw, err := p.PTY.SyscallConn()
	if err != nil {
		return err
	}
	var resizeErr error
	err = raw.Control(func(fd uintptr) {
		resizeErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Row: rows, Col: cols})
	})
	return errors.Join(err, resizeErr)
}

// Close retains the caller's maintenance lease until the execution domain is
// positively empty. A completed systemd stop job is not a population proof.
// A cleanup failure is surfaced, so admission can remain blocked.
func (p *TinkerProcess) Close() error {
	var err error
	p.once.Do(func() {
		// Serialize revocation with the helper's final check and process start.
		revokeCtx, revokeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		revokeErr := revokeTinkerAdmission(revokeCtx, p.configDir)
		revokeCancel()
		_ = p.PTY.Close()
		ctx := context.Background()
		_, err = Run(ctx, 10*time.Second, "/usr/bin/systemctl", "stop", p.unit)
		if err != nil {
			out, checkErr := Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", p.unit, "--property=LoadState", "--value")
			if checkErr == nil && strings.TrimSpace(out) == "not-found" {
				err = nil
			}
		}
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
		drainErr := waitTinkerDomainEmpty(drainCtx, p.unit)
		drainCancel()
		if revokeErr != nil && drainErr == nil {
			// Stopping a helper stalled before process start releases its launch
			// gate. Revoke it now before any delayed replacement could launch.
			retryCtx, retryCancel := context.WithTimeout(context.Background(), 2*time.Second)
			revokeErr = revokeTinkerAdmission(retryCtx, p.configDir)
			retryCancel()
		}
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		select {
		case <-p.Done:
		case <-time.After(5 * time.Second):
			err = fmt.Errorf("Tinker relay did not exit")
		}
		err = errors.Join(revokeErr, err, drainErr)
		if err == nil {
			_ = os.RemoveAll(p.configDir)
			if p.unregister != nil {
				p.unregister()
			}
		}
		p.closeErr = err
	})
	return p.closeErr
}

// ExecuteTinker is a fixed root supervisor entry point. It acquires its own
// maintenance lease before any application bootstrap and verifies the original
// panel process, closing delayed systemd-start and daemon-crash admission races.
func ExecuteTinker(args []string) error {
	if len(args) != 6 || !tinkerID.MatchString(args[0]) || !tinkerVersion.MatchString(args[1]) || !filepath.IsAbs(args[2]) || os.Geteuid() != 0 {
		return errors.New("Invalid Tinker supervisor invocation")
	}
	id, version, directory := args[0], args[1], args[2]
	parent, err := strconv.Atoi(args[3])
	if err != nil || parent <= 1 {
		return errors.New("Invalid panel identity")
	}
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil || !strings.Contains(string(b), "0::/system.slice/fluxo-tinker-"+id+".service\n") {
		return errors.New("Tinker requires its managed execution domain")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := packages.Lifecycle(ctx, true)
	if err != nil {
		return err
	}
	defer release()
	identity, err := tinkerProcessIdentity(parent)
	if err != nil || identity != args[4] {
		return errors.New("Original panel process has ended")
	}
	if !strings.HasPrefix(args[5], "/run/fluxo-tinker-config-") || filepath.Dir(args[5]) != "/run" {
		return errors.New("Invalid configuration directory")
	}
	unlockLaunch, err := lockTinkerLaunch(ctx, args[5])
	if err != nil {
		return err
	}
	defer unlockLaunch()
	if err := tinkerAdmitted(args[5]); err != nil {
		return err
	}
	credential, err := ResolveCredential("fluxo")
	if err != nil || credential.Uid == 0 || credential.Gid == 0 {
		return errors.New("Invalid application identity")
	}
	home := "/run/fluxo-tinker-" + id
	if err = os.Chmod(home, 0700); err != nil {
		return err
	}
	if err = os.Chown(home, int(credential.Uid), int(credential.Gid)); err != nil {
		return err
	}
	config := filepath.Join(args[5], "config.php")
	if !strings.HasPrefix(args[5], "/run/fluxo-tinker-config-") || filepath.Dir(args[5]) != "/run" {
		return errors.New("Invalid configuration directory")
	}
	cmd := exec.Command("/usr/bin/php"+version, "artisan", "tinker", "--ansi")
	cmd.Dir = directory
	cmd.Env = []string{"HOME=" + home, "XDG_CONFIG_HOME=" + home, "XDG_DATA_HOME=" + home, "XDG_CACHE_HOME=" + home, "USER=fluxo", "LOGNAME=fluxo", "PATH=/usr/local/bin:/usr/bin:/bin", "TERM=xterm-256color", "LANG=C.UTF-8", "PSYSH_CONFIG=" + config}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: credential}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Both supervisor and PHP share the terminal foreground process group.
	// Keep the supervisor alive while PsySH handles Ctrl-C itself.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		for {
			select {
			case <-interrupts:
			case <-stopped:
				return
			}
		}
	}()
	if err = startTinkerApplication(cmd); err != nil {
		return err
	}
	unlockLaunch()
	return cmd.Wait()
}

// PID1 grants the supervisor ambient SETUID, which also sets its inheritable
// bit. Go's Credential transition clears the child's ambient/permitted/effective
// sets on UID change but preserves inheritable bits. Fork PHP from this same
// sanitized thread, keeping the capabilities needed for Go's UID/GID setup.
func startTinkerApplication(cmd *exec.Cmd) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	data := [2]unix.CapUserData{}
	if err := unix.Capget(&header, &data[0]); err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); err != nil {
		return err
	}
	data[0].Inheritable, data[1].Inheritable = 0, 0
	if err := unix.Capset(&header, &data[0]); err != nil {
		return err
	}
	actual := [2]unix.CapUserData{}
	if err := unix.Capget(&header, &actual[0]); err != nil {
		return err
	}
	if actual != data {
		return errors.New("Tinker capability readback differs")
	}
	for capability := 0; capability < 64; capability++ {
		ambient, err := unix.PrctlRetInt(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_IS_SET, uintptr(capability), 0, 0)
		if errors.Is(err, unix.EINVAL) {
			break
		}
		if err != nil || ambient != 0 {
			return errors.New("Tinker ambient capability readback differs")
		}
	}
	return cmd.Start()
}

func tinkerProcessIdentity(pid int) (string, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	i := strings.LastIndex(string(b), ") ")
	if i < 0 {
		return "", errors.New("Invalid process identity")
	}
	fields := strings.Fields(string(b)[i+2:])
	if len(fields) < 20 {
		return "", errors.New("Invalid process identity")
	}
	return fields[19], nil
}

func tinkerAdmitted(dir string) error {
	fd, err := syscall.Open(filepath.Join(dir, "admission"), syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return errors.New("Tinker admission was revoked")
	}
	defer syscall.Close(fd)
	var st syscall.Stat_t
	if err = syscall.Fstat(fd, &st); err != nil || st.Uid != 0 || st.Mode&syscall.S_IFMT != syscall.S_IFREG || st.Mode&0777 != 0600 || st.Nlink != 1 {
		return errors.New("Invalid Tinker admission marker")
	}
	return nil
}

func pollablePTY(original *os.File) (*os.File, error) {
	raw, err := original.SyscallConn()
	if err != nil {
		return nil, err
	}
	var duplicate int
	var dupErr error
	if err = raw.Control(func(fd uintptr) { duplicate, dupErr = unix.FcntlInt(fd, unix.F_DUPFD_CLOEXEC, 0) }); err != nil {
		return nil, err
	}
	if dupErr != nil {
		return nil, dupErr
	}
	if err = unix.SetNonblock(duplicate, true); err != nil {
		unix.Close(duplicate)
		return nil, err
	}
	f := os.NewFile(uintptr(duplicate), original.Name())
	if err = f.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		f.Close()
		return nil, err
	}
	_ = f.SetWriteDeadline(time.Time{})
	_ = original.Close()
	return f, nil
}

func startTinkerRelay(cmd *exec.Cmd) (*os.File, error) {
	master, tty, err := pty.Open()
	if err != nil {
		return nil, err
	}
	defer tty.Close()
	// PTY setup calls File.Fd, which can make Go's original descriptor blocking.
	// Reopen before launching anything so deadline support is proven first.
	f, err := pollablePTY(master)
	if err != nil {
		master.Close()
		return nil, err
	}
	if err = pty.Setsize(tty, &pty.Winsize{Rows: 24, Cols: 100}); err != nil {
		f.Close()
		return nil, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err = cmd.Start(); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
