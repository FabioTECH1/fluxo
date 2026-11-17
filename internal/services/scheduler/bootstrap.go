package scheduler

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/packages"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

type bootstrapSeal struct {
	ID            string                           `json:"id"`
	Phase         string                           `json:"phase"`
	PID           int                              `json:"pid"`
	Start         string                           `json:"start"`
	Boot          string                           `json:"boot"`
	EntryIdentity string                           `json:"entry_identity"`
	Loader        LoaderBinding                    `json:"loader"`
	ImageSHA256   string                           `json:"image_sha256"`
	ImageDevice   uint64                           `json:"image_device"`
	ImageInode    uint64                           `json:"image_inode"`
	Service       *executiondomain.ServiceIdentity `json:"service"`
	Lease         packages.LifecycleTransfer       `json:"lease"`
}

func validateBootstrapInvocation(v invocation) error {
	if v.ExecutionContract == "" {
		if v.Helper != "" || v.HelperSHA256 != "" || v.Bootstrap != nil {
			return errors.New("historical Scheduler authority cannot acquire bootstrap semantics")
		}
		return nil
	}
	if v.ExecutionContract != executiondomain.SchedulerBootstrapContract || !v.Guarded || v.Adapter == "" || filepath.Dir(v.Helper) != filepath.Join(stateDir, "helpers") || filepath.Base(v.Helper) != v.HelperSHA256 {
		return errors.New("unknown or malformed Scheduler execution contract")
	}
	sha, e := hex.DecodeString(v.HelperSHA256)
	if e != nil || len(sha) != 32 || strings.ToLower(v.HelperSHA256) != v.HelperSHA256 {
		return errors.New("Scheduler helper digest is invalid")
	}
	if v.Started && (v.Bootstrap == nil || v.Bootstrap.Phase != "consumed") {
		return errors.New("started Scheduler worker lacks a consumed bootstrap seal")
	}
	if s := v.Bootstrap; s != nil {
		if _, e = uuid.Parse(s.ID); e != nil {
			return e
		}
		if s.Phase != "pending" && s.Phase != "consumed" || !v.Started && s.Phase != "pending" || s.PID <= 1 || s.Start == "" || s.Boot != v.Boot || s.EntryIdentity != v.Entry.Identity || s.ImageSHA256 != v.HelperSHA256 || s.ImageInode == 0 || !s.Service.Valid() || !v.ServiceIdentity.Valid() || *s.Service != *v.ServiceIdentity || s.Lease.FD < 3 || s.Lease.OwnerPID != s.PID || s.Lease.Inode == 0 || s.Lease.MarkerOffset <= 0 || !s.Loader.Valid() || s.Loader.EntryID != v.Entry.ID || s.Loader.Binding != v.Entry.Identity || s.Loader.Nonce != v.Entry.LoaderNonce || s.Loader.TriggerHash != hashBytes([]byte(CronContent(v.Entry))) {
			return errors.New("Scheduler bootstrap seal does not bind its invocation")
		}
	}
	return nil
}
func bootstrapSelf(v invocation, image *executiondomain.PinnedExecutionImage) error {
	if image.SHA256 != v.HelperSHA256 {
		return errors.New("bootstrap helper image differs from durable invocation")
	}
	if err := trustedFile(v.Helper); err != nil {
		return err
	}
	var st unix.Stat_t
	if e := unix.Lstat(v.Helper, &st); e != nil {
		return e
	}
	if uint64(st.Dev) != image.Device || st.Ino != image.Inode {
		return errors.New("named immutable Scheduler helper was replaced")
	}
	return nil
}
func bootstrapProcess(v invocation, p map[string]string) error {
	raw, e := os.ReadFile("/proc/self/cgroup")
	if e != nil || strings.TrimSpace(string(raw)) != "0::/system.slice/"+v.Unit {
		return errors.New("Scheduler bootstrap/worker left its recorded cgroup")
	}
	boot, e := bootID()
	if e != nil || boot != v.Boot {
		return errors.New("Scheduler launch belongs to another boot")
	}
	if p["LoadState"] != "loaded" || p["MainPID"] != strconv.Itoa(os.Getpid()) || p["ControlPID"] != "0" {
		return errors.New("Scheduler process is not its exact PID1 main invocation")
	}
	return nil
}

// Bootstrap performs all privileged provider qualification before producing a
// one-use pending seal. It retains the SAME shared lifecycle OFD across exec;
// no application starts and no Started authority is consumed in this phase.
func Bootstrap(ctx context.Context, unit string) error {
	if e := executiondomain.GuardLoaderWrite(filepath.Dir(databasePath), executiondomain.Capabilities()); e != nil {
		return e
	}
	current, e := executiondomain.ReadSchedulerThreadCredentials()
	if e != nil {
		return e
	}
	if e = current.RequireRootCapabilities(executiondomain.SchedulerBootstrapCapabilities); e != nil {
		return e
	}
	if e = prepareSchedulerControlEnvironment(); e != nil {
		return e
	}
	leaseCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	lease, e := packages.AcquireLifecycleSharedLease(leaseCtx)
	cancel()
	if e != nil {
		return e
	}
	defer lease.Close()
	unlock, e := lock(ctx)
	if e != nil {
		return e
	}
	j, e := read()
	if e != nil {
		unlock()
		return e
	}
	v, ok := j.Invocations[unit]
	if !ok || j.Schema != executiondomain.SchedulerLoaderSchema || v.ExecutionContract != executiondomain.SchedulerBootstrapContract || v.Started || v.Bootstrap != nil {
		unlock()
		return errors.New("Scheduler bootstrap authority is absent, sealed or consumed")
	}
	if e = validateBootstrapInvocation(v); e != nil {
		unlock()
		return e
	}
	now, ok := j.Entries[v.Entry.ID]
	if !ok || !now.Enabled || now.Identity != v.Entry.Identity || identity(now) != identity(v.Entry) || now.LoaderNonce != v.Entry.LoaderNonce || j.Upgrade != nil || j.Holds[v.Entry.Version] != "" {
		unlock()
		return errors.New("Scheduler registration or maintenance hold changed before bootstrap")
	}
	if e = executiondomain.RequireAdapterHost(ctx, v.Adapter); e != nil {
		unlock()
		return e
	}
	p, e := inspectInvocationUnit(ctx, unit, v)
	if e != nil {
		unlock()
		return e
	}
	if e = bootstrapProcess(v, p); e != nil {
		unlock()
		return e
	}
	image, e := executiondomain.PinSelfExecutable(v.HelperSHA256)
	if e != nil {
		unlock()
		return e
	}
	defer image.Close()
	if e = bootstrapSelf(v, image); e != nil {
		unlock()
		return e
	}
	if e = committed(ctx, v.Entry, true); e != nil {
		unlock()
		return e
	}
	binding, e := RequireEntryLoaderBinding(ctx, j, v.Entry)
	if e != nil {
		unlock()
		return e
	}
	service, e := executiondomain.CaptureServiceIdentity(unit, p)
	if e != nil {
		unlock()
		return e
	}
	transfer, e := lease.PrepareExecTransfer()
	if e != nil {
		unlock()
		return e
	}
	start, e := startKey(os.Getpid())
	if e != nil {
		unlock()
		return e
	}
	v.ServiceIdentity = service
	v.Bootstrap = &bootstrapSeal{ID: uuid.NewString(), Phase: "pending", PID: os.Getpid(), Start: start, Boot: v.Boot, EntryIdentity: v.Entry.Identity, Loader: binding, ImageSHA256: image.SHA256, ImageDevice: image.Device, ImageInode: image.Inode, Service: service, Lease: transfer}
	if e = validateBootstrapInvocation(v); e == nil {
		j.Invocations[unit] = v
		e = save(j)
	}
	unlock()
	if e != nil {
		return e
	}
	// No journal gate crosses exec. Arm only the designated inherited lease;
	// the transition marks all other private descriptors CLOEXEC and cannot return.
	if e = lease.ArmExecTransfer(transfer); e != nil {
		return e
	}
	argv := []string{v.Helper, "scheduler-worker", unit, v.Bootstrap.ID, strconv.Itoa(transfer.FD)}
	executiondomain.DropAndExecSchedulerWorker(image, transfer.FD, argv, schedulerControlEnvironment())
	return errors.New("unreachable Scheduler bootstrap transition")
}

// ExecuteWorker consumes the exact seal and Started in one durable transition.
// It rechecks mutable entry/hold/epoch and DB admission under the gate, without
// attempting the privileged provider probes already completed by Bootstrap.
// The seal does not freeze external PID1/admin changes after qualification.
func ExecuteWorker(ctx context.Context, unit, sealID string, fd int) error {
	if fd < 3 {
		return errors.New("invalid Scheduler inherited lifecycle descriptor")
	}
	if _, e := unix.FcntlInt(uintptr(fd), unix.F_SETFD, unix.FD_CLOEXEC); e != nil {
		return e
	}
	if _, e := executiondomain.VerifySchedulerWorkerThreads(); e != nil {
		return e
	}
	if e := prepareSchedulerControlEnvironment(); e != nil {
		return e
	}
	if e := executiondomain.GuardLoaderWrite(filepath.Dir(databasePath), executiondomain.Capabilities()); e != nil {
		return e
	}
	unlock, e := lock(ctx)
	if e != nil {
		return e
	}
	j, e := read()
	if e != nil {
		unlock()
		return e
	}
	v, ok := j.Invocations[unit]
	if !ok || j.Schema != executiondomain.SchedulerLoaderSchema || v.ExecutionContract != executiondomain.SchedulerBootstrapContract || v.Started || v.Bootstrap == nil || v.Bootstrap.Phase != "pending" || v.Bootstrap.ID != sealID || v.Bootstrap.Lease.FD != fd {
		unlock()
		return errors.New("Scheduler worker seal is absent, stale or consumed")
	}
	if e = validateBootstrapInvocation(v); e != nil {
		unlock()
		return e
	}
	s := v.Bootstrap
	start, e := startKey(os.Getpid())
	if e != nil || os.Getpid() != s.PID || start != s.Start {
		unlock()
		return errors.New("Scheduler worker process generation changed across exec")
	}
	image, e := executiondomain.PinSelfExecutable(s.ImageSHA256)
	if e != nil {
		unlock()
		return e
	}
	defer image.Close()
	if image.Device != s.ImageDevice || image.Inode != s.ImageInode {
		unlock()
		return errors.New("Scheduler worker image differs from sealed bootstrap")
	}
	if e = bootstrapSelf(v, image); e != nil {
		unlock()
		return e
	}
	lease, e := packages.AdoptInheritedLifecycleLease(s.Lease)
	if e != nil {
		unlock()
		return e
	}
	defer lease.Close()
	p, e := inspectInvocationUnit(ctx, unit, v)
	if e != nil {
		unlock()
		return e
	}
	if e = bootstrapProcess(v, p); e != nil {
		unlock()
		return e
	}
	if e = executiondomain.ValidateServiceIdentity(s.Service, unit, p); e != nil {
		unlock()
		return e
	}
	now, ok := j.Entries[v.Entry.ID]
	if !ok || !now.Enabled || now.Identity != v.Entry.Identity || identity(now) != identity(v.Entry) || now.LoaderNonce != v.Entry.LoaderNonce || j.Upgrade != nil || j.Holds[v.Entry.Version] != "" {
		unlock()
		return errors.New("Scheduler registration or maintenance hold changed during bootstrap")
	}
	if e = ValidateEntryLoaderBinding(j, v.Entry, s.Loader); e != nil {
		unlock()
		return e
	}
	if e = committed(ctx, v.Entry, true); e != nil {
		unlock()
		return e
	}
	s.Phase = "consumed"
	v.Started = true
	v.Submitted = true
	j.Invocations[unit] = v
	e = save(j)
	unlock()
	if e != nil {
		return e
	}
	return runSchedulerPayload(ctx, v)
}
func runSchedulerPayload(ctx context.Context, v invocation) error {
	runErr := syscmd.RunTrackedApplication("fluxo", v.Entry.Directory, "/usr/bin/php"+v.Entry.Version, filepath.Join(v.Entry.Directory, "artisan"), "schedule:run")
	for {
		pids, e := groupPIDs("/system.slice/" + v.Unit)
		if e != nil {
			return e
		}
		delete(pids, os.Getpid())
		if len(pids) == 0 {
			return runErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func installBootstrapHelper() (string, string, error) {
	exe, e := os.Executable()
	if e != nil {
		return "", "", e
	}
	exe, e = filepath.EvalSymlinks(exe)
	if e != nil {
		return "", "", e
	}
	if e = trustedFile(exe); e != nil {
		return "", "", e
	}
	code, e := os.ReadFile(exe)
	if e != nil {
		return "", "", e
	}
	if len(code) > 128<<20 {
		return "", "", errors.New("Scheduler helper exceeds preservation budget")
	}
	sha := hashBytes(code)
	dir := filepath.Join(stateDir, "helpers")
	if e = os.Mkdir(dir, 0700); e != nil && !os.IsExist(e) {
		return "", "", e
	}
	if e = owned(dir, true); e != nil {
		return "", "", e
	}
	path := filepath.Join(dir, sha)
	if e = trustedFile(path); e == nil {
		sum, err := fileDigest(path)
		if err != nil || sum != sha {
			return "", "", errors.New("content-addressed Scheduler helper changed")
		}
		return path, sha, nil
	} else if !os.IsNotExist(e) {
		return "", "", e
	}
	if e = atomicFile(path, code, 0700); e != nil {
		return "", "", e
	}
	return path, sha, nil
}

// Only these isolated fixed helper entrypoints call this function. The daemon
// never changes its environment. Static Go must sanitize BEFORE any dynamic
// control tool or metadata helper can honor inherited loader, bus, package or
// user-home selectors. The application receives its own clean fluxo environment
// from RunTrackedApplication after the irreversible drop/exec boundary.
func schedulerControlEnvironment() []string {
	return []string{"HOME=/root", "USER=root", "LOGNAME=root", "PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
}
func prepareSchedulerControlEnvironment() error {
	os.Clearenv()
	for _, value := range schedulerControlEnvironment() {
		key, val, _ := strings.Cut(value, "=")
		if err := os.Setenv(key, val); err != nil {
			return err
		}
	}
	return nil
}
