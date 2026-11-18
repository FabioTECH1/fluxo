package executiondomain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"fluxo/internal/syscmd"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const vmRolePrefix = "executiondomain-vm-"

type vmIdentity struct {
	PID          int    `json:"pid"`
	Start        string `json:"start"`
	Boot         string `json:"boot"`
	Cgroup       string `json:"cgroup"`
	InvocationID string `json:"invocation_id"`
	UID          int    `json:"uid"`
	Session      int    `json:"session"`
}

type vmResponse struct {
	Challenge string     `json:"challenge"`
	Sequence  int        `json:"sequence"`
	Identity  vmIdentity `json:"identity"`
}

func init() {
	if len(os.Args) < 2 || !strings.HasPrefix(os.Args[1], vmRolePrefix) {
		return
	}
	if err := runVMRole(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func vmGuard() bool {
	host, _ := os.Hostname()
	return (host == "fluxo-bridge-jammy-20261101" || host == "fluxo-arm64-2204-acceptance" && runtime.GOARCH == "arm64") && os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") == "1"
}

func vmAtomic(path string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path+".next", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(path+".next", path)
}

func vmWait(ctx context.Context, predicate func() (bool, error)) error {
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		ok, err := predicate()
		if err != nil || ok {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func vmRead(path string, value any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, value)
}

func vmCurrentIdentity() (vmIdentity, error) {
	i := vmIdentity{PID: os.Getpid(), UID: os.Geteuid(), InvocationID: os.Getenv("INVOCATION_ID")}
	stat, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return i, err
	}
	end := strings.LastIndexByte(string(stat), ')')
	if end < 0 {
		return i, errors.New("missing process stat name")
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 20 {
		return i, errors.New("incomplete process identity")
	}
	i.Start = fields[19]
	i.Session, err = strconv.Atoi(fields[3])
	if err != nil {
		return i, err
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return i, err
	}
	i.Boot = strings.TrimSpace(string(boot))
	group, err := os.ReadFile("/proc/self/cgroup")
	i.Cgroup = strings.TrimSpace(string(group))
	return i, err
}

func vmCommand(executable, role, root, unit string) *exec.Cmd {
	c := exec.Command(executable, vmRolePrefix+role, root, unit)
	c.Env = []string{"PATH=/usr/bin:/bin", "HOME=/home/fluxo", "FLUXO_PHP_UPDATE_VM_TEST=1", "INVOCATION_ID=" + os.Getenv("INVOCATION_ID")}
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c
}

func runVMRole() error {
	if !vmGuard() || len(os.Args) != 4 {
		return errors.New("guarded Ubuntu 22 acceptance helper only")
	}
	role, root, unit := strings.TrimPrefix(os.Args[1], vmRolePrefix), os.Args[2], os.Args[3]
	id := strings.TrimSuffix(strings.TrimPrefix(unit, "fluxo-executiondomain-"), ".service")
	if _, err := uuid.Parse(id); err != nil || unit != "fluxo-executiondomain-"+id+".service" || root != "/var/lib/fluxo-executiondomain-acceptance-"+id {
		return errors.New("unexpected private acceptance identity")
	}
	for _, path := range []string{"/var", "/var/lib", root, filepath.Join(root, "executiondomain.test")} {
		s, err := os.Lstat(path)
		if err != nil {
			return err
		}
		st, ok := s.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || s.Mode().Perm()&0022 != 0 || s.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe private acceptance executable or ancestor")
		}
	}
	i, err := vmCurrentIdentity()
	if err != nil || i.Cgroup != "0::/system.slice/"+unit || len(i.InvocationID) != 32 {
		return errors.New("acceptance helper outside its invocation domain")
	}
	exchange, control := filepath.Join(root, "exchange"), filepath.Join(root, "control")
	executable := filepath.Join(root, "executiondomain.test")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if role == "helper" {
		if os.Geteuid() != 0 {
			return errors.New("helper must be root")
		}
		counter, err := os.OpenFile(filepath.Join(control, "helper-starts"), os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(counter, i.PID)
		if err == nil {
			err = counter.Sync()
		}
		counter.Close()
		if err != nil {
			return err
		}
		if err = vmAtomic(filepath.Join(control, "helper.json"), i); err != nil {
			return err
		}
		credential, err := syscmd.ResolveCredential("fluxo")
		if err != nil || credential.Uid == 0 {
			return errors.New("missing unprivileged fluxo fixture identity")
		}
		c := vmCommand(executable, "detach", root, unit)
		c.SysProcAttr = &syscall.SysProcAttr{Credential: credential, Setsid: true}
		if err = c.Run(); err != nil {
			return err
		}
		if err = vmWait(ctx, func() (bool, error) {
			_, e := os.Stat(filepath.Join(exchange, "ready.json"))
			return e == nil, nil
		}); err != nil {
			return err
		}
		if err = vmAtomic(filepath.Join(control, "helper-ready"), true); err != nil {
			return err
		}
		return vmWait(ctx, func() (bool, error) {
			_, e := os.Stat(filepath.Join(control, "main-release"))
			return e == nil, nil
		})
	}
	credential, err := syscmd.ResolveCredential("fluxo")
	if err != nil || credential.Uid == 0 || uint32(os.Geteuid()) != credential.Uid {
		return errors.New("payload must use unprivileged fluxo identity")
	}
	if role == "detach" {
		c := vmCommand(executable, "payload", root, unit)
		c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err = c.Start(); err != nil {
			return err
		}
		return c.Process.Release()
	}
	if role != "payload" || i.Session != i.PID {
		return errors.New("payload must be detached session leader")
	}
	if err = vmAtomic(filepath.Join(exchange, "ready.json"), i); err != nil {
		return err
	}
	sequence, last := 0, ""
	return vmWait(ctx, func() (bool, error) {
		if _, e := os.Stat(filepath.Join(exchange, "release")); e == nil {
			current, e := vmCurrentIdentity()
			if e != nil {
				return false, e
			}
			return true, vmAtomic(filepath.Join(exchange, "completed.json"), current)
		}
		var challenge string
		if e := vmRead(filepath.Join(exchange, "challenge"), &challenge); e == nil && challenge != last {
			sequence++
			last = challenge
			current, e := vmCurrentIdentity()
			if e != nil {
				return false, e
			}
			return false, vmAtomic(filepath.Join(exchange, "response.json"), vmResponse{Challenge: challenge, Sequence: sequence, Identity: current})
		}
		return false, nil
	})
}

func vmShow(ctx context.Context, unit string) (map[string]string, error) {
	out, err := syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "show", unit, "--property=Id,LoadState,ActiveState,SubState,MainPID,InvocationID,ControlGroup,Type,RemainAfterExit,KillMode,Restart,CollectMode,User,Group,NoNewPrivileges,Delegate,ProtectControlGroups,WorkingDirectory,RuntimeMaxUSec,WatchdogUSec,TimeoutStopUSec,SendSIGKILL,Transient,ExecStart,ExecStartPre,ExecStartPost,ExecStop,ExecStopPost,DropInPaths,CapabilityBoundingSet")
	if err != nil {
		return nil, err
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

func vmConfiguredExecStart(observed, prefix string) bool {
	return strings.HasPrefix(observed, prefix+" ignore_errors=no ;") && strings.Count(observed, "{ path=") == 1
}

func vmPopulation(unit string) (int, error) {
	raw, err := os.ReadFile(filepath.Join("/sys/fs/cgroup/system.slice", unit, "pids.current"))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(raw)))
}

// TestVMPersistent249DomainLifetime changes PID 1 only in the dedicated,
// disposable Jammy acceptance VM. It does not test production authorization or
// migrate any Fluxo journal, and never force-kills a payload to prove drain.
func TestVMPersistent249DomainLifetime(t *testing.T) {
	if !vmGuard() || os.Geteuid() != 0 {
		t.Skip("explicit root opt-in on disposable Ubuntu 22 acceptance VM only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	version, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", "--property=Version", "--value")
	if err != nil || !strings.HasPrefix(strings.TrimSpace(version), "249") {
		t.Fatalf("test requires the actual systemd 249 candidate platform: %q %v", version, err)
	}
	if _, err = os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"successful-main-exit", "helper-sigkill"} {
		t.Run(mode, func(t *testing.T) { vmLifetimeScenario(t, ctx, mode) })
	}
}

func vmLifetimeScenario(t *testing.T, ctx context.Context, mode string) {
	t.Helper()
	id := uuid.NewString()
	unit := "fluxo-executiondomain-" + id + ".service"
	root := "/var/lib/fluxo-executiondomain-acceptance-" + id
	exchange, control := filepath.Join(root, "exchange"), filepath.Join(root, "control")
	executable := filepath.Join(root, "executiondomain.test")
	for _, path := range []string{root, exchange, control} {
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(control, 0700); err != nil {
		t.Fatal(err)
	}
	credential, err := syscmd.ResolveCredential("fluxo")
	if err != nil || credential.Uid == 0 {
		t.Fatal("missing nonroot fluxo fixture identity")
	}
	if err = os.Chown(exchange, int(credential.Uid), int(credential.Gid)); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(executable, raw, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(control, "helper-starts"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("retained %s evidence: %s", mode, root)
	t.Cleanup(func() {
		// Signal only this fixture's cooperative exit. If natural drain cannot be
		// confirmed, retain all evidence and report the failure; do not kill it.
		_ = vmAtomic(filepath.Join(exchange, "release"), true)
		_ = vmAtomic(filepath.Join(control, "main-release"), true)
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		if e := vmWait(cleanup, func() (bool, error) { n, e := vmPopulation(unit); return n == 0, e }); e != nil {
			t.Errorf("private payload did not drain naturally; preserved %s: %v", root, e)
			return
		}
		_, _ = syscmd.Run(cleanup, 5*time.Second, "/usr/bin/systemctl", "stop", unit)
		_, _ = syscmd.Run(cleanup, 5*time.Second, "/usr/bin/systemctl", "reset-failed", unit)
	})
	args := []string{"--quiet", "--unit=" + unit}
	args = append(args, Persistent249CandidateArguments()...)
	args = append(args, "--setenv=FLUXO_PHP_UPDATE_VM_TEST=1", "--", executable, vmRolePrefix+"helper", root, unit)
	if out, e := syscmd.Run(ctx, 20*time.Second, "/usr/bin/systemd-run", args...); e != nil {
		t.Fatalf("candidate submit: %v %s", e, out)
	}
	if err = vmWait(ctx, func() (bool, error) { _, e := os.Stat(filepath.Join(control, "helper-ready")); return e == nil, nil }); err != nil {
		t.Fatal("helper/payload readiness handshake", err)
	}
	var helper, child vmIdentity
	if err = vmRead(filepath.Join(control, "helper.json"), &helper); err != nil {
		t.Fatal(err)
	}
	if err = vmRead(filepath.Join(exchange, "ready.json"), &child); err != nil {
		t.Fatal(err)
	}
	if helper.UID != 0 || child.UID != int(credential.Uid) || helper.PID == child.PID || child.Session != child.PID || helper.Cgroup != "0::/system.slice/"+unit || child.Cgroup != helper.Cgroup || child.Boot != helper.Boot || child.InvocationID != helper.InvocationID {
		t.Fatalf("inconsistent helper/detached payload identities: %+v %+v", helper, child)
	}
	initial, err := vmShow(ctx, unit)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range Persistent249CandidateProperties() {
		if initial[k] != want {
			t.Fatalf("candidate %s=%q, expected %q", k, initial[k], want)
		}
	}
	caps := strings.Fields(initial["CapabilityBoundingSet"])
	sort.Strings(caps)
	if strings.Join(caps, " ") != "cap_dac_read_search cap_setgid cap_setuid" {
		t.Fatal("candidate capability restriction changed")
	}
	prefix := "{ path=" + executable + " ; argv[]=" + executable + " " + vmRolePrefix + "helper " + root + " " + unit + " ;"
	if !vmConfiguredExecStart(initial["ExecStart"], prefix) || initial["MainPID"] != strconv.Itoa(helper.PID) || initial["InvocationID"] != helper.InvocationID {
		t.Fatal("candidate executable or systemd invocation identity differs")
	}
	var groupStat unix.Stat_t
	if err = unix.Stat("/sys/fs/cgroup/system.slice/"+unit, &groupStat); err != nil {
		t.Fatal(err)
	}
	responses := map[string]vmResponse{}
	states := map[string]map[string]string{}
	check := func(stage string) {
		t.Helper()
		p, e := vmShow(ctx, unit)
		if e != nil {
			t.Fatal(e)
		}
		if e = vmAtomic(filepath.Join(control, "observed-"+stage+".json"), map[string]any{"stage": stage, "unit": p, "initial_unit": initial, "helper": helper, "payload": child}); e != nil {
			t.Fatal(e)
		}
		var st unix.Stat_t
		if e = unix.Stat("/sys/fs/cgroup/system.slice/"+unit, &st); e != nil || st.Ino != groupStat.Ino || st.Dev != groupStat.Dev || p["InvocationID"] != helper.InvocationID || p["ControlGroup"] != "/system.slice/"+unit || p["LoadState"] != "loaded" {
			t.Fatalf("%s lost persistent invocation identity: %v %+v", stage, e, p)
		}
		for k, want := range Persistent249CandidateProperties() {
			if p[k] != want {
				t.Fatalf("%s changed candidate %s=%q, expected %q", stage, k, p[k], want)
			}
		}
		// ExecStart includes changing PID/timestamp/exit bookkeeping. Validate
		// the complete configured path/argv and flags, not those observations.
		if !vmConfiguredExecStart(p["ExecStart"], prefix) || p["CapabilityBoundingSet"] != initial["CapabilityBoundingSet"] || stage != "initial" && p["MainPID"] != "0" {
			t.Fatalf("%s changed executable, capabilities or consumed main identity: initial ExecStart=%q capabilities=%q; observed ExecStart=%q capabilities=%q MainPID=%q", stage, initial["ExecStart"], initial["CapabilityBoundingSet"], p["ExecStart"], p["CapabilityBoundingSet"], p["MainPID"])
		}
		if stage == "after-main-exit" {
			if mode == "helper-sigkill" && p["ActiveState"] != "failed" || mode == "successful-main-exit" && (p["ActiveState"] != "active" || p["SubState"] != "exited") {
				t.Fatalf("unexpected %s transition: %+v", mode, p)
			}
		}
		challenge := uuid.NewString()
		if e = vmAtomic(filepath.Join(exchange, "challenge"), challenge); e != nil {
			t.Fatal(e)
		}
		if e = os.Chmod(filepath.Join(exchange, "challenge"), 0644); e != nil {
			t.Fatal(e)
		}
		var response vmResponse
		if e = vmWait(ctx, func() (bool, error) {
			e := vmRead(filepath.Join(exchange, "response.json"), &response)
			return e == nil && response.Challenge == challenge, nil
		}); e != nil {
			t.Fatalf("%s payload stopped responding: %v", stage, e)
		}
		if response.Identity != child || response.Sequence != len(responses)+1 {
			t.Fatalf("%s payload identity/progress changed: %+v", stage, response)
		}
		starts, e := os.ReadFile(filepath.Join(control, "helper-starts"))
		if e != nil || strings.TrimSpace(string(starts)) != strconv.Itoa(helper.PID) {
			t.Fatalf("%s replayed helper invocation: %v %q", stage, e, starts)
		}
		responses[stage] = response
		states[stage] = p
	}
	check("initial")
	if mode == "helper-sigkill" {
		fd, e := unix.PidfdOpen(helper.PID, 0)
		if e != nil {
			t.Fatal(e)
		}
		defer unix.Close(fd)
		stat, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", helper.PID))
		if e != nil {
			t.Fatal(e)
		}
		end := strings.LastIndexByte(string(stat), ')')
		if end < 0 || len(strings.Fields(string(stat[end+1:]))) < 20 || strings.Fields(string(stat[end+1:]))[19] != helper.Start {
			t.Fatal("helper changed before intentional pidfd-bound signal")
		}
		if e = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); e != nil {
			t.Fatal(e)
		}
	} else if err = vmAtomic(filepath.Join(control, "main-release"), true); err != nil {
		t.Fatal(err)
	}
	if err = vmWait(ctx, func() (bool, error) { p, e := vmShow(ctx, unit); return e == nil && p["MainPID"] == "0", e }); err != nil {
		t.Fatal("main exit handshake", err)
	}
	check("after-main-exit")
	for _, action := range []string{"reset-failed", "daemon-reload", "daemon-reexec"} {
		command := []string{action}
		if action == "reset-failed" {
			command = append(command, unit)
		}
		if out, e := syscmd.Run(ctx, 20*time.Second, "/usr/bin/systemctl", command...); e != nil {
			t.Fatalf("%s: %v %s", action, e, out)
		}
		check(action)
	}
	if err = vmAtomic(filepath.Join(exchange, "release"), true); err != nil {
		t.Fatal(err)
	}
	var completed vmIdentity
	if err = vmWait(ctx, func() (bool, error) {
		e := vmRead(filepath.Join(exchange, "completed.json"), &completed)
		return e == nil, nil
	}); err != nil || completed != child {
		t.Fatal("payload did not acknowledge natural completion", err)
	}
	if err = vmWait(ctx, func() (bool, error) { n, e := vmPopulation(unit); return n == 0, e }); err != nil {
		t.Fatal("populated domain after acknowledged natural exit", err)
	}
	if mode == "successful-main-exit" {
		// RemainAfterExit deliberately retains successful empty domains. Stop is
		// allowed here only after the independent population proof above.
		if out, e := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "stop", unit); e != nil {
			t.Fatalf("retire naturally empty unit: %v %s", e, out)
		}
	}
	if err = vmWait(ctx, func() (bool, error) { p, e := vmShow(ctx, unit); return e == nil && p["LoadState"] == "not-found", e }); err != nil {
		t.Fatal("naturally empty domain was not garbage-collected", err)
	}
	if err = vmAtomic(filepath.Join(control, "evidence.json"), map[string]any{"status": "PASS", "candidate": Persistent249CandidateV1, "scenario": mode, "helper": helper, "payload": child, "initial_unit": initial, "cgroup_inode": groupStat.Ino, "cgroup_device": groupStat.Dev, "responses": responses, "unit_states": states, "completed": completed, "natural_drain": true, "production_enabled": false}); err != nil {
		t.Fatal(err)
	}
}
