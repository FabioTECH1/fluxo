package executiondomain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"fluxo/internal/services/packages"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

type bootstrapABIManifest struct{ Root, Token, Boot, Image, Unit, App, Failure string }
type bootstrapABISeal struct {
	ID                      string
	PID, TID                int
	Start                   string
	Lease                   packages.LifecycleTransfer
	ImageSHA                string
	ImageDevice, ImageInode uint64
	Service                 *ServiceIdentity
	Bootstrap               KernelCredentials
}
type bootstrapABIReady struct {
	PID, TID               int
	Start                  string
	Threads                map[int]KernelCredentials
	Lease                  packages.LifecycleTransfer
	FDFlags                int
	ConfiguredCapabilities string
}

func bootstrapABIKnownHost() bool {
	h, _ := os.Hostname()
	return h == "fluxo-bridge-jammy-20261101" || h == "fluxo-bridge-prod-20261101" || h == "fluxo-bridge-resolute-20261101"
}
func bootstrapABIWrite(path string, value any) error {
	b, e := json.Marshal(value)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	d, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func bootstrapABIRead(path string, value any) error {
	b, e := readProtected(path, 4<<20)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, value)
}
func bootstrapABIStart(pid int) (string, error) {
	b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if e != nil {
		return "", e
	}
	last := strings.LastIndex(string(b), ")")
	if last < 0 {
		return "", errors.New("invalid PID stat")
	}
	f := strings.Fields(string(b)[last+2:])
	if len(f) < 20 {
		return "", errors.New("short PID stat")
	}
	return f[19], nil
}
func bootstrapABIValidate(root string) (bootstrapABIManifest, error) {
	var m bootstrapABIManifest
	if os.Geteuid() != 0 || !bootstrapABIKnownHost() || filepath.Dir(root) != "/var/lib" || !strings.HasPrefix(filepath.Base(root), "fluxo-bootstrap-abi-") {
		return m, errors.New("root known bootstrap ABI namespace only")
	}
	token := strings.TrimPrefix(filepath.Base(root), "fluxo-bootstrap-abi-")
	if _, e := uuid.Parse(token); e != nil {
		return m, e
	}
	if e := bootstrapABIRead(root+"/manifest.json", &m); e != nil {
		return m, e
	}
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		return m, e
	}
	if m.Root != root || m.Token != token || m.Boot != strings.TrimSpace(string(boot)) || m.Unit != "fluxo-scheduler-"+token+".service" || m.App != "/home/fluxo/bootstrap-abi-"+token {
		return m, errors.New("ABI manifest identity differs")
	}
	switch m.Failure {
	case "", "invalid-argv", "invalid-env", "closed-image", "wrong-image", "path-replace":
	default:
		return m, errors.New("unknown ABI fixture action")
	}
	return m, nil
}
func bootstrapABIWait(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, e := readProtected(path, 64); e == nil {
			return nil
		} else if !os.IsNotExist(e) {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
func bootstrapABIUnit(ctx context.Context, unit string) (map[string]string, error) {
	out, e := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", unit, "--property=LoadState,MainPID,ControlPID,InvocationID,ControlGroup,ActiveState,SubState,CapabilityBoundingSet,ExecMainStatus,ExecMainCode")
	if e != nil {
		return nil, e
	}
	p := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			p[k] = v
		}
	}
	return p, nil
}
func bootstrapABIFail(e error) {
	fmt.Fprintln(os.Stderr, "bootstrap ABI refusal:", e)
	_, _, _ = unix.RawSyscall(unix.SYS_EXIT_GROUP, 124, 0, 0)
	for {
	}
}
func init() {
	if len(os.Args) < 2 || (os.Args[1] != "execution-bootstrap-abi" && os.Args[1] != "execution-worker-abi") {
		return
	}
	worker := os.Args[1] == "execution-worker-abi"
	root := ""
	if worker {
		if len(os.Args) != 5 {
			bootstrapABIFail(errors.New("worker ABI args"))
		}
		fd, e := strconv.Atoi(os.Args[2])
		if e != nil || fd < 3 {
			bootstrapABIFail(errors.New("worker lease fd"))
		}
		if _, e = unix.FcntlInt(uintptr(fd), unix.F_SETFD, unix.FD_CLOEXEC); e != nil {
			bootstrapABIFail(e)
		}
		root = os.Args[3]
	} else {
		if len(os.Args) != 3 {
			bootstrapABIFail(errors.New("bootstrap ABI args"))
		}
		root = os.Args[2]
	}
	m, e := bootstrapABIValidate(root)
	if e != nil {
		bootstrapABIFail(e)
	}
	if worker {
		bootstrapABIWorker(m)
		os.Exit(0)
	}
	// Lock the original leader in this init goroutine, then transition from a
	// different locked OS thread. Successful exec must collapse it back to TGID.
	runtime.LockOSThread()
	if unix.Gettid() != os.Getpid() {
		bootstrapABIFail(errors.New("ABI leader reservation failed"))
	}
	go func() {
		runtime.LockOSThread()
		if unix.Gettid() == os.Getpid() {
			bootstrapABIFail(errors.New("ABI did not use a nonleader"))
		}
		lease, e := packages.AcquireLifecycleSharedLease(context.Background())
		if e != nil {
			bootstrapABIFail(e)
		}
		tr, e := lease.PrepareExecTransfer()
		if e != nil {
			bootstrapABIFail(e)
		}
		image, e := PinSelfExecutable(m.Image)
		if e != nil {
			bootstrapABIFail(e)
		}
		current, e := ReadSchedulerThreadCredentials()
		if e != nil {
			bootstrapABIFail(e)
		}
		if e = current.RequireRootCapabilities(SchedulerBootstrapCapabilities); e != nil {
			bootstrapABIFail(e)
		}
		p, e := bootstrapABIUnit(context.Background(), m.Unit)
		if e != nil || p["MainPID"] != strconv.Itoa(os.Getpid()) {
			bootstrapABIFail(errors.New("bootstrap PID1 main identity"))
		}
		domain, e := CaptureServiceIdentity(m.Unit, p)
		if e != nil {
			bootstrapABIFail(e)
		}
		start, e := bootstrapABIStart(os.Getpid())
		if e != nil {
			bootstrapABIFail(e)
		}
		seal := bootstrapABISeal{ID: uuid.NewString(), PID: os.Getpid(), TID: unix.Gettid(), Start: start, Lease: tr, ImageSHA: image.SHA256, ImageDevice: image.Device, ImageInode: image.Inode, Service: domain, Bootstrap: current}
		if e = bootstrapABIWrite(root+"/pending.json", seal); e != nil {
			bootstrapABIFail(e)
		}
		if e = bootstrapABIWait(root + "/allow-exec"); e != nil {
			bootstrapABIFail(e)
		}
		if e = lease.ArmExecTransfer(tr); e != nil {
			bootstrapABIFail(e)
		}
		argv := []string{root + "/helper", "execution-worker-abi", strconv.Itoa(tr.FD), root, seal.ID}
		env := []string{"HOME=/home/fluxo", "USER=fluxo", "LOGNAME=fluxo", "PATH=/usr/local/bin:/usr/bin:/bin"}
		if m.Failure != "" && m.Failure != "path-replace" {
			if e = bootstrapABIFullStderr(root); e != nil {
				bootstrapABIFail(e)
			}
			switch m.Failure {
			case "invalid-argv":
				argv[1] += "\x00"
			case "invalid-env":
				env = append(env, "INVALID=\x00")
			case "closed-image":
				if e = unix.Close(image.FD); e != nil {
					bootstrapABIFail(e)
				}
			case "wrong-image":
				bad, err := unix.Open("/dev/null", unix.O_RDONLY|unix.O_CLOEXEC, 0)
				if err != nil {
					bootstrapABIFail(err)
				}
				if e = unix.Close(image.FD); e != nil {
					bootstrapABIFail(e)
				}
				image.FD = bad
			}
		}
		DropAndExecSchedulerWorker(image, tr.FD, argv, env)
	}()
	select {}
}
func bootstrapABIWorker(m bootstrapABIManifest) {
	var seal bootstrapABISeal
	if e := bootstrapABIRead(m.Root+"/pending.json", &seal); e != nil {
		bootstrapABIFail(e)
	}
	fd, e := strconv.Atoi(os.Args[2])
	if e != nil || fd != seal.Lease.FD || os.Args[4] != seal.ID || seal.PID != os.Getpid() || seal.TID == seal.PID {
		bootstrapABIFail(errors.New("worker seal/process identity"))
	}
	start, e := bootstrapABIStart(os.Getpid())
	if e != nil || start != seal.Start {
		bootstrapABIFail(errors.New("nonleader exec changed TGID starttime"))
	}
	threads, e := VerifySchedulerWorkerThreads()
	if e != nil {
		bootstrapABIFail(e)
	}
	stopThreads, e := bootstrapABINewWorkerThreads()
	if e != nil {
		bootstrapABIFail(e)
	}
	defer stopThreads()
	threads, e = VerifySchedulerWorkerThreads()
	if e != nil {
		bootstrapABIFail(e)
	}
	if m.Failure == "path-replace" {
		st := unix.Stat_t{}
		fd, err := unix.Open("/proc/self/exe", unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			bootstrapABIFail(err)
		}
		err = unix.Fstat(fd, &st)
		unix.Close(fd)
		if err != nil {
			bootstrapABIFail(err)
		}
		if uint64(st.Dev) != seal.ImageDevice || st.Ino != seal.ImageInode {
			bootstrapABIFail(errors.New("pinned exec did not retain original inode"))
		}
		if e = bootstrapABIWrite(m.Root+"/reexec-observed.json", struct {
			PID           int
			Device, Inode uint64
			Threads       map[int]KernelCredentials
		}{os.Getpid(), uint64(st.Dev), st.Ino, threads}); e != nil {
			bootstrapABIFail(e)
		}
	}
	image, e := PinSelfExecutable(seal.ImageSHA)
	if e != nil {
		bootstrapABIFail(e)
	}
	if image.Device != seal.ImageDevice || image.Inode != seal.ImageInode {
		bootstrapABIFail(errors.New("worker image inode changed"))
	}
	image.Close()
	lease, e := packages.AdoptInheritedLifecycleLease(seal.Lease)
	if e != nil {
		bootstrapABIFail(e)
	}
	defer lease.Close()
	p, e := bootstrapABIUnit(context.Background(), m.Unit)
	if e != nil {
		bootstrapABIFail(e)
	}
	if e = ValidateServiceIdentity(seal.Service, m.Unit, p); e != nil {
		bootstrapABIFail(e)
	}
	configured := strings.Fields(p["CapabilityBoundingSet"])
	sort.Strings(configured)
	if p["MainPID"] != strconv.Itoa(seal.PID) || p["ControlPID"] != "0" || strings.Join(configured, " ") != "cap_dac_read_search cap_setgid cap_setpcap cap_setuid cap_sys_ptrace" {
		bootstrapABIFail(errors.New("configured bootstrap caps/unit changed"))
	}
	flags, e := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	if e != nil || flags&unix.FD_CLOEXEC == 0 {
		bootstrapABIFail(errors.New("worker lease CLOEXEC absent"))
	}
	if e = bootstrapABIWrite(m.Root+"/consumed.json", seal); e != nil {
		bootstrapABIFail(e)
	}
	// Actual unprivileged PHP must see no permitted/effective/ambient/inheritable
	// caps and must not inherit the private lifecycle descriptor.
	script := `$fds=[];foreach(glob('/proc/self/fd/*') as $f){$v=@readlink($f);if($v!==false)$fds[basename($f)]=$v;}file_put_contents($argv[1],json_encode(['status'=>file_get_contents('/proc/self/status'),'fds'=>(object)$fds]));`
	if e = syscmd.RunTrackedApplication("fluxo", m.App, "/usr/bin/php8.4", "-r", script, "--", m.App+"/php.json"); e != nil {
		bootstrapABIFail(e)
	}
	ready := bootstrapABIReady{PID: os.Getpid(), TID: unix.Gettid(), Start: start, Threads: threads, Lease: seal.Lease, FDFlags: flags, ConfiguredCapabilities: p["CapabilityBoundingSet"]}
	if e = bootstrapABIWrite(m.Root+"/worker.json", ready); e != nil {
		bootstrapABIFail(e)
	}
	if e = bootstrapABIWait(m.Root + "/release"); e != nil {
		bootstrapABIFail(e)
	}
	lease.Close()
	if e = bootstrapABIWrite(m.Root+"/completed.json", ready); e != nil {
		bootstrapABIFail(e)
	}
}
func bootstrapABIPoll(t *testing.T, ctx context.Context, what string, check func() bool) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if check() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s: %v", what, ctx.Err())
		case <-ticker.C:
		}
	}
}
func TestVMNonLeaderSchedulerBootstrapABI(t *testing.T) {
	if os.Geteuid() != 0 || !bootstrapABIKnownHost() || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("explicit known root native bootstrap ABI only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	token := uuid.NewString()
	root := "/var/lib/fluxo-bootstrap-abi-" + token
	app := "/home/fluxo/bootstrap-abi-" + token
	if e := os.Mkdir(root, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(app, 0755); e != nil {
		t.Fatal(e)
	}
	cred, e := syscmd.ResolveCredential("fluxo")
	if e != nil || cred.Uid == 0 {
		t.Fatal("unprivileged fixture user", e)
	}
	if e = os.Chown(app, int(cred.Uid), int(cred.Gid)); e != nil {
		t.Fatal(e)
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	exe, e = filepath.EvalSymlinks(exe)
	if e != nil {
		t.Fatal(e)
	}
	image, e := readProtected(exe, 128<<20)
	if e != nil {
		t.Fatal("stage protected root test image", e)
	}
	if e = os.WriteFile(root+"/helper", image, 0700); e != nil {
		t.Fatal(e)
	}
	sum := sha256Bytes(image)
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		t.Fatal(e)
	}
	m := bootstrapABIManifest{Root: root, Token: token, Boot: strings.TrimSpace(string(boot)), Image: sum, Unit: "fluxo-scheduler-" + token + ".service", App: app}
	if e = bootstrapABIWrite(root+"/manifest.json", m); e != nil {
		t.Fatal(e)
	}
	// Failure cleanup releases only this test's handshakes. It never signals a
	// payload or alters global schema/floor/registration state.
	defer os.WriteFile(root+"/allow-exec", []byte("1\n"), 0600)
	defer os.WriteFile(root+"/release", []byte("1\n"), 0600)
	args, e := SubmissionArguments(PersistentV1, m.Unit)
	if e != nil {
		t.Fatal(e)
	}
	args = append(args, "--property=User=root", "--property=Group=root", "--property=WorkingDirectory=/", "--property=CapabilityBoundingSet=CAP_SETUID CAP_SETGID CAP_DAC_READ_SEARCH CAP_SYS_PTRACE CAP_SETPCAP", "--property=StandardOutput=append:"+root+"/unit.log", "--property=StandardError=inherit", "--", root+"/helper", "execution-bootstrap-abi", root)
	if out, e := syscmd.Run(ctx, 20*time.Second, "/usr/bin/systemd-run", args...); e != nil {
		t.Fatal(e, out)
	}
	var pending bootstrapABISeal
	bootstrapABIPoll(t, ctx, "locked nonleader bootstrap pending seal", func() bool { return bootstrapABIRead(root+"/pending.json", &pending) == nil })
	if pending.TID == pending.PID || pending.PID <= 1 {
		t.Fatal("nonleader proof absent", pending)
	}
	observer, e := unix.Open(packages.LifecycleLockPath, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer unix.Close(observer)
	if e = unix.Flock(observer, unix.LOCK_EX|unix.LOCK_NB); !errors.Is(e, unix.EWOULDBLOCK) {
		t.Fatal("bootstrap did not retain original lifecycle flock", e)
	}
	if e = os.WriteFile(root+"/allow-exec", []byte("1\n"), 0600); e != nil {
		t.Fatal(e)
	}
	var ready bootstrapABIReady
	bootstrapABIPoll(t, ctx, "three-cap worker + real PHP after pinned nonleader exec", func() bool {
		if e := unix.Flock(observer, unix.LOCK_EX|unix.LOCK_NB); !errors.Is(e, unix.EWOULDBLOCK) {
			if e == nil {
				unix.Flock(observer, unix.LOCK_UN)
			}
			t.Fatal("exclusive contender did not observe an owned conflict across delayed handoff", e)
		}
		return bootstrapABIRead(root+"/worker.json", &ready) == nil
	})
	if ready.PID != pending.PID || ready.Start != pending.Start || ready.FDFlags&unix.FD_CLOEXEC == 0 || len(ready.Threads) == 0 {
		t.Fatal("worker handoff evidence", ready)
	}
	phpRaw, e := os.ReadFile(app + "/php.json")
	if e != nil {
		t.Fatal(e)
	}
	var php struct {
		Status string
		FDs    map[string]string
	}
	if e = json.Unmarshal(phpRaw, &php); e != nil {
		t.Fatal(e)
	}
	if len(php.FDs) < 3 || php.FDs["0"] == "" || php.FDs["1"] == "" || php.FDs["2"] == "" {
		t.Fatal("PHP descriptor evidence is incomplete", php.FDs)
	}
	phpCred, e := ParseKernelCredentials([]byte(php.Status))
	if e != nil {
		t.Fatal(e)
	}
	if phpCred.UID != [4]uint32{cred.Uid, cred.Uid, cred.Uid, cred.Uid} || phpCred.GID != [4]uint32{cred.Gid, cred.Gid, cred.Gid, cred.Gid} || phpCred.Permitted != 0 || phpCred.Effective != 0 || phpCred.Inheritable != 0 || phpCred.Ambient != 0 || phpCred.Bounding != SchedulerWorkerCapabilities || phpCred.NoNewPrivileges != 1 {
		t.Fatal("PHP credential/capability leakage", phpCred)
	}
	for _, target := range php.FDs {
		if target == packages.LifecycleLockPath || strings.Contains(target, "/pending.json") || target == root+"/helper" {
			t.Fatal("private bootstrap descriptor leaked to PHP", target)
		}
	}
	if e = os.WriteFile(root+"/release", []byte("1\n"), 0600); e != nil {
		t.Fatal(e)
	}
	bootstrapABIPoll(t, ctx, "natural lease release", func() bool {
		if _, e := os.Stat(root + "/completed.json"); e != nil {
			return false
		}
		e := unix.Flock(observer, unix.LOCK_EX|unix.LOCK_NB)
		if e == nil {
			unix.Flock(observer, unix.LOCK_UN)
			return true
		}
		return false
	})
	empty := func() error {
		p, e := bootstrapABIUnit(ctx, m.Unit)
		if e != nil {
			return e
		}
		if p["MainPID"] != "0" || p["ControlPID"] != "0" {
			return errors.New("ABI helper has not exited naturally")
		}
		b, e := os.ReadFile("/sys/fs/cgroup/system.slice/" + m.Unit + "/pids.current")
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil || strings.TrimSpace(string(b)) != "0" {
			return errors.New("ABI domain is still populated")
		}
		return nil
	}
	bootstrapABIPoll(t, ctx, "natural ABI helper/domain drain", func() bool { return empty() == nil })
	if e = StopNaturallyEmpty(ctx, PersistentV1, m.Unit, empty); e != nil {
		t.Fatal("retire own naturally empty ABI metadata", e)
	}
	t.Logf("native nonleader pinned bootstrap5→worker3, same TGID/start/OFD, all-thread and PHP credentials/FDs PASS; evidence %s", root)
}
func sha256Bytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Deliberately fill a blocking stderr pipe. The real post-drop fatal path must
// never attempt diagnostic I/O before exit_group; siblings still have bootstrap
// privileges until that group exit. Raw descriptors have no Go finalizers.
func bootstrapABIFullStderr(root string) error {
	var pair [2]int
	if e := unix.Pipe2(pair[:], unix.O_CLOEXEC|unix.O_NONBLOCK); e != nil {
		return e
	}
	chunk := make([]byte, 4096)
	for {
		_, e := unix.Write(pair[1], chunk)
		if errors.Is(e, unix.EAGAIN) {
			break
		}
		if e != nil {
			return e
		}
	}
	flags, e := unix.FcntlInt(uintptr(pair[1]), unix.F_GETFL, 0)
	if e != nil {
		return e
	}
	if _, e = unix.FcntlInt(uintptr(pair[1]), unix.F_SETFL, flags&^unix.O_NONBLOCK); e != nil {
		return e
	}
	if e = unix.Dup3(pair[1], 2, 0); e != nil {
		return e
	}
	return bootstrapABIWrite(root+"/stderr-full.json", struct{ Full bool }{true})
}
func bootstrapABINewWorkerThreads() (func(), error) {
	release := make(chan struct{})
	ready := make(chan error, 6)
	for i := 0; i < 6; i++ {
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			c, e := ReadSchedulerThreadCredentials()
			if e == nil {
				e = c.RequireRootCapabilities(SchedulerWorkerCapabilities)
			}
			if e == nil {
				bits, err := unix.PrctlRetInt(unix.PR_GET_SECUREBITS, 0, 0, 0, 0)
				if err != nil || bits != 0 {
					e = errors.New("new worker thread securebits differ")
				}
			}
			ready <- e
			<-release
		}()
	}
	for i := 0; i < 6; i++ {
		if e := <-ready; e != nil {
			close(release)
			return func() {}, e
		}
	}
	return func() { close(release) }, nil
}

func TestVMSchedulerBootstrapExecFailuresExitWholeProcess(t *testing.T) {
	if os.Geteuid() != 0 || !bootstrapABIKnownHost() || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("explicit root known native bootstrap failures only")
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	exe, e = filepath.EvalSymlinks(exe)
	if e != nil {
		t.Fatal(e)
	}
	image, e := readProtected(exe, 128<<20)
	if e != nil {
		t.Fatal("stage root-protected ABI test image", e)
	}
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		t.Fatal(e)
	}
	for _, failure := range []string{"invalid-argv", "invalid-env", "closed-image", "wrong-image", "path-replace"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			token := uuid.NewString()
			root := "/var/lib/fluxo-bootstrap-abi-" + token
			app := "/home/fluxo/bootstrap-abi-" + token
			if e = os.Mkdir(root, 0700); e != nil {
				t.Fatal(e)
			}
			if e = os.Mkdir(app, 0755); e != nil {
				t.Fatal(e)
			}
			cred, e := syscmd.ResolveCredential("fluxo")
			if e != nil {
				t.Fatal(e)
			}
			if e = os.Chown(app, int(cred.Uid), int(cred.Gid)); e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(root+"/helper", image, 0700); e != nil {
				t.Fatal(e)
			}
			m := bootstrapABIManifest{Root: root, Token: token, Boot: strings.TrimSpace(string(boot)), Image: sha256Bytes(image), Unit: "fluxo-scheduler-" + token + ".service", App: app, Failure: failure}
			if e = bootstrapABIWrite(root+"/manifest.json", m); e != nil {
				t.Fatal(e)
			}
			defer os.WriteFile(root+"/allow-exec", []byte("1\n"), 0600)
			defer os.WriteFile(root+"/release", []byte("1\n"), 0600)
			args, e := SubmissionArguments(PersistentV1, m.Unit)
			if e != nil {
				t.Fatal(e)
			}
			args = append(args, "--property=User=root", "--property=Group=root", "--property=WorkingDirectory=/", "--property=CapabilityBoundingSet=CAP_SETUID CAP_SETGID CAP_DAC_READ_SEARCH CAP_SYS_PTRACE CAP_SETPCAP", "--property=StandardOutput=append:"+root+"/unit.log", "--property=StandardError=inherit", "--", root+"/helper", "execution-bootstrap-abi", root)
			if out, e := syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemd-run", args...); e != nil {
				t.Fatal(e, out)
			}
			var pending bootstrapABISeal
			bootstrapABIPoll(t, ctx, "live privileged sibling seal", func() bool { return bootstrapABIRead(root+"/pending.json", &pending) == nil })
			entries, e := os.ReadDir(fmt.Sprintf("/proc/%d/task", pending.PID))
			if e != nil || len(entries) < 2 {
				t.Fatal("privileged siblings not established", e)
			}
			siblings := map[int]KernelCredentials{}
			for _, entry := range entries {
				tid, e := strconv.Atoi(entry.Name())
				if e != nil {
					t.Fatal(e)
				}
				raw, e := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/status", pending.PID, tid))
				if os.IsNotExist(e) {
					continue
				}
				if e != nil {
					t.Fatal(e)
				}
				c, e := ParseKernelCredentials(raw)
				if e != nil || c.RequireRootCapabilities(SchedulerBootstrapCapabilities) != nil {
					t.Fatal("bootstrap sibling is not exact five-cap phase", tid, c, e)
				}
				siblings[tid] = c
			}
			if len(siblings) < 2 || siblings[pending.TID].Effective != SchedulerBootstrapCapabilities {
				t.Fatal("nonleader privileged siblings not observed")
			}
			observer, e := unix.Open(packages.LifecycleLockPath, unix.O_RDWR|unix.O_CLOEXEC, 0)
			if e != nil {
				t.Fatal(e)
			}
			defer unix.Close(observer)
			if e = unix.Flock(observer, unix.LOCK_EX|unix.LOCK_NB); !errors.Is(e, unix.EWOULDBLOCK) {
				if e == nil {
					unix.Flock(observer, unix.LOCK_UN)
				}
				t.Fatal("pending failure fixture lacks shared lease", e)
			}
			if failure == "path-replace" {
				// Same bytes, different pathname inode: primitive must execute its pinned
				// original; the subsequent worker named-image check must still refuse it.
				if e = os.WriteFile(root+"/replacement", image, 0700); e != nil {
					t.Fatal(e)
				}
				if e = os.Rename(root+"/replacement", root+"/helper"); e != nil {
					t.Fatal(e)
				}
			}
			if e = os.WriteFile(root+"/allow-exec", []byte("1\n"), 0600); e != nil {
				t.Fatal(e)
			}
			expected := "125"
			if failure == "path-replace" {
				expected = "124"
			}
			var terminal map[string]string
			bootstrapABIPoll(t, ctx, "whole-process fatal exit with original lease released", func() bool {
				p, e := bootstrapABIUnit(ctx, m.Unit)
				if e != nil || p["MainPID"] != "0" {
					return false
				}
				if p["ExecMainStatus"] != expected {
					t.Fatalf("wrong failure status: %+v", p)
				}
				b, e := os.ReadFile("/sys/fs/cgroup/system.slice/" + m.Unit + "/pids.current")
				if e == nil && strings.TrimSpace(string(b)) != "0" {
					return false
				}
				if e != nil && !os.IsNotExist(e) {
					t.Fatal(e)
				}
				if e = unix.Flock(observer, unix.LOCK_EX|unix.LOCK_NB); e != nil {
					if !errors.Is(e, unix.EWOULDBLOCK) {
						t.Fatal("contender observation failed", e)
					}
					return false
				}
				unix.Flock(observer, unix.LOCK_UN)
				terminal = p
				return true
			})
			if failure != "path-replace" {
				var full struct{ Full bool }
				if e = bootstrapABIRead(root+"/stderr-full.json", &full); e != nil || !full.Full {
					t.Fatal("blocked stderr fault was not reached", e)
				}
			} else {
				var observed struct {
					PID           int
					Device, Inode uint64
					Threads       map[int]KernelCredentials
				}
				if e = bootstrapABIRead(root+"/reexec-observed.json", &observed); e != nil || observed.PID != pending.PID || observed.Device != pending.ImageDevice || observed.Inode != pending.ImageInode || len(observed.Threads) < 6 {
					t.Fatal("pinned old image was not observed before named-path refusal", observed, e)
				}
				var named unix.Stat_t
				if e = unix.Lstat(root+"/helper", &named); e != nil || named.Ino == observed.Inode {
					t.Fatal("replacement path did not have a different inode", e)
				}
			}
			for _, name := range []string{root + "/worker.json", root + "/consumed.json", app + "/php.json"} {
				if _, e = os.Stat(name); !os.IsNotExist(e) {
					t.Fatal("failed transition consumed application authority", name, e)
				}
			}
			if _, e = os.Stat(fmt.Sprintf("/proc/%d/task", pending.PID)); !os.IsNotExist(e) {
				t.Fatal("privileged sibling process survived group exit", e)
			}
			if e = bootstrapABIWrite(root+"/failure-evidence.json", struct {
				Failure  string
				Pending  bootstrapABISeal
				Siblings map[int]KernelCredentials
				Terminal map[string]string
			}{failure, pending, siblings, terminal}); e != nil {
				t.Fatal(e)
			}
			empty := func() error {
				p, e := bootstrapABIUnit(ctx, m.Unit)
				if e != nil {
					return e
				}
				if p["MainPID"] != "0" || p["ControlPID"] != "0" {
					return errors.New("ABI failure domain not naturally empty")
				}
				b, e := os.ReadFile("/sys/fs/cgroup/system.slice/" + m.Unit + "/pids.current")
				if os.IsNotExist(e) {
					return nil
				}
				if e != nil || strings.TrimSpace(string(b)) != "0" {
					return errors.New("ABI failure still populated")
				}
				return nil
			}
			if e = StopNaturallyEmpty(ctx, PersistentV1, m.Unit, empty); e != nil {
				t.Fatal(e)
			}
			t.Logf("%s whole-process exit%s, no payload/authority, lease released; evidence %s", failure, expected, root)
		})
	}
}
