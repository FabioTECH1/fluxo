package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

const distributionRolePrefix = "distribution-cron-candidate-"

type distributionGuard struct {
	Active    bool       `json:"active"`
	ID        string     `json:"id"`
	RootNonce string     `json:"root_nonce"`
	JobNonce  string     `json:"job_nonce"`
	Host      legacyHost `json:"host"`
	UID       uint32     `json:"uid"`
	GID       uint32     `json:"gid"`
}

type distributionIdentity struct {
	PID     int    `json:"pid"`
	Start   string `json:"start"`
	Boot    string `json:"boot"`
	Group   string `json:"group"`
	UID     uint32 `json:"uid"`
	GID     uint32 `json:"gid"`
	Session int    `json:"session"`
}

type distributionReceipt struct {
	Nonce    string               `json:"nonce"`
	Identity distributionIdentity `json:"identity"`
	Origin   bool                 `json:"daemon_ancestry_verified"`
}

func distributionVMHost() string {
	host, _ := os.Hostname()
	switch host {
	case "fluxo-bridge-jammy-20261101":
		return jammyCronCandidateID
	case "fluxo-bridge-resolute-20261101":
		return resoluteCronCandidateID
	}
	return ""
}

func init() {
	if len(os.Args) < 2 || !strings.HasPrefix(os.Args[1], distributionRolePrefix) {
		return
	}
	if err := distributionNativeRole(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func distributionAtomic(path string, value any, mode os.FileMode) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path+".next", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if err = f.Chmod(mode); err != nil {
		f.Close()
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

func distributionRead(path string, value any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, value)
}

func distributionWait(ctx context.Context, ready func() (bool, error)) error {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		ok, err := ready()
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

func distributionProcess(pid int) (distributionIdentity, error) {
	i := distributionIdentity{PID: pid}
	start, err := startKey(pid)
	if err != nil {
		return i, err
	}
	i.Start = start
	i.Boot, err = bootID()
	if err != nil {
		return i, err
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return i, err
	}
	end := strings.LastIndexByte(string(stat), ')')
	if end < 0 {
		return i, errors.New("incomplete cron payload identity")
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 20 || fields[19] != start {
		return i, errors.New("cron payload process changed")
	}
	i.Session, err = strconv.Atoi(fields[3])
	if err != nil {
		return i, err
	}
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return i, err
	}
	for _, line := range strings.Split(string(status), "\n") {
		f := strings.Fields(line)
		if len(f) != 5 || f[0] != "Uid:" && f[0] != "Gid:" {
			continue
		}
		if f[1] != f[2] || f[1] != f[3] || f[1] != f[4] {
			return i, errors.New("cron payload has changing credentials")
		}
		value, e := strconv.ParseUint(f[1], 10, 32)
		if e != nil {
			return i, e
		}
		if f[0] == "Uid:" {
			i.UID = uint32(value)
		} else {
			i.GID = uint32(value)
		}
	}
	group, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	i.Group = strings.TrimSpace(string(group))
	if err != nil {
		return i, err
	}
	if current, e := startKey(pid); e != nil || current != i.Start {
		return i, errors.New("cron payload changed during observation")
	}
	return i, nil
}

func distributionAncestry(pid int, host legacyHost) error {
	for n := 0; n < 16 && pid > 1; n++ {
		if pid == host.PID {
			if start, err := startKey(pid); err == nil && start == host.Start {
				return nil
			}
			break
		}
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			return err
		}
		end := strings.LastIndexByte(string(raw), ')')
		if end < 0 {
			return errors.New("invalid cron ancestry")
		}
		f := strings.Fields(string(raw[end+1:]))
		if len(f) < 2 {
			return errors.New("incomplete cron ancestry")
		}
		pid, err = strconv.Atoi(f[1])
		if err != nil {
			return err
		}
	}
	return errors.New("payload did not originate from the recorded cron daemon")
}

func distributionNativeRole() error {
	if len(os.Args) != 4 || distributionVMHost() == "" {
		return errors.New("dedicated distribution candidate VM only")
	}
	role, root, nonce := strings.TrimPrefix(os.Args[1], distributionRolePrefix), os.Args[2], os.Args[3]
	id := strings.TrimPrefix(root, "/var/lib/fluxo-cron-candidate-")
	if _, err := uuid.Parse(id); err != nil || root != "/var/lib/fluxo-cron-candidate-"+id {
		return errors.New("invalid private cron candidate identity")
	}
	guardPath, executable := filepath.Join(root, "guard.json"), filepath.Join(root, "scheduler-candidate.test")
	if err := trustedFile(guardPath); err != nil {
		return err
	}
	if err := trustedFile(executable); err != nil {
		return err
	}
	var guard distributionGuard
	if err := distributionRead(guardPath, &guard); err != nil || !guard.Active || guard.ID != id || guard.Host.Backend != distributionVMHost() {
		return errors.New("inactive or mismatched root-owned test authorization")
	}
	i, err := distributionProcess(os.Getpid())
	if err != nil || i.Boot != guard.Host.Boot || i.Group != "0::/system.slice/cron.service" {
		return errors.New("candidate payload escaped the recorded cron domain")
	}
	exchange, control := filepath.Join(root, "exchange"), filepath.Join(root, "control")
	if role == "root" {
		if os.Geteuid() != 0 || nonce != guard.RootNonce {
			return errors.New("invalid root receipt authorization")
		}
		fresh, e := inspectDistributionCronCandidate(context.Background(), guard.Host.Backend)
		if e != nil || fresh != guard.Host {
			return errors.New("cron identity/configuration changed before root receipt")
		}
		if e = cronReceiptOrigin(guard.Host); e != nil {
			return e
		}
		path := filepath.Join(control, "root-receipt.json")
		if _, e = os.Stat(path); e == nil {
			return nil
		}
		return distributionAtomic(path, distributionReceipt{Nonce: nonce, Identity: i, Origin: true}, 0600)
	}
	if nonce != guard.JobNonce || i.UID != guard.UID || i.GID != guard.GID || i.UID == 0 {
		return errors.New("nonroot PAM payload has unexpected authorization or credentials")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	child := func(next string) *exec.Cmd {
		c := exec.Command(executable, distributionRolePrefix+next, root, nonce)
		c.Env = []string{"PATH=/usr/bin:/bin", "HOME=/home/fluxo", "USER=fluxo", "LOGNAME=fluxo"}
		c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		return c
	}
	if role == "job" {
		if err = cronReceiptOrigin(guard.Host); err != nil {
			return err
		}
		first, e := os.OpenFile(filepath.Join(exchange, "first-job"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(e, os.ErrExist) {
			return nil
		}
		if e != nil {
			return e
		}
		first.Close()
		if e = distributionUserAtomic(filepath.Join(exchange, "user-receipt.json"), distributionReceipt{Nonce: nonce, Identity: i, Origin: true}, 0600); e != nil {
			return e
		}
		if e = distributionWait(ctx, func() (bool, error) { _, e := os.Stat(filepath.Join(exchange, "detach-release")); return e == nil, nil }); e != nil {
			return e
		}
		return child("detach").Run()
	}
	if role == "detach" {
		c := child("payload")
		if err = c.Start(); err != nil {
			return err
		}
		return c.Process.Release()
	}
	if role != "payload" || i.Session != i.PID {
		return errors.New("expected session-detached non-PHP grandchild")
	}
	if err = distributionUserAtomic(filepath.Join(exchange, "payload-ready.json"), i, 0600); err != nil {
		return err
	}
	return distributionWait(ctx, func() (bool, error) {
		if _, e := os.Stat(filepath.Join(exchange, "release")); e == nil {
			current, e := distributionProcess(os.Getpid())
			if e != nil {
				return false, e
			}
			return true, distributionUserAtomic(filepath.Join(exchange, "completed.json"), current, 0600)
		}
		var challenge string
		if e := distributionRead(filepath.Join(exchange, "challenge.json"), &challenge); e == nil && challenge != "" {
			current, e := distributionProcess(os.Getpid())
			if e != nil {
				return false, e
			}
			return false, distributionUserAtomic(filepath.Join(exchange, "response.json"), map[string]any{"challenge": challenge, "identity": current}, 0600)
		}
		return false, nil
	})
}

// The authorization flag is consumed by the root test driver, not injected into
// cron's environment. The fixed root-owned guard and actual daemon ancestry
// authenticate each receipt; root and nonroot PAM execution are observed apart.
func TestVMDistributionCronCandidatePAMContainment(t *testing.T) {
	id := distributionVMHost()
	if id == "" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("explicit root opt-in on dedicated Jammy/Resolute candidate VMs only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	host, err := inspectDistributionCronCandidate(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := syscmd.ResolveCredential("fluxo")
	if err != nil || credential.Uid == 0 {
		t.Fatal("missing nonroot fluxo test account")
	}
	fixtureID := uuid.NewString()
	root := "/var/lib/fluxo-cron-candidate-" + fixtureID
	exchange, control := filepath.Join(root, "exchange"), filepath.Join(root, "control")
	for _, path := range []string{root, exchange, control} {
		if err = os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.Chmod(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.Chmod(control, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(exchange, int(credential.Uid), int(credential.Gid)); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "scheduler-candidate.test")
	if err = os.WriteFile(executable, binary, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(executable, 0755); err != nil {
		t.Fatal(err)
	}
	guard := distributionGuard{Active: true, ID: fixtureID, RootNonce: uuid.NewString(), JobNonce: uuid.NewString(), Host: host, UID: credential.Uid, GID: credential.Gid}
	guardPath := filepath.Join(root, "guard.json")
	if err = distributionAtomic(guardPath, guard, 0644); err != nil {
		t.Fatal(err)
	}
	trigger := "/etc/cron.d/fluxo-candidate-" + fixtureID
	content := fmt.Sprintf("# Disabled %s native qualification %s\n* * * * * root %s %sroot %s %s\n* * * * * fluxo %s %sjob %s %s\n", id, fixtureID, executable, distributionRolePrefix, root, guard.RootNonce, executable, distributionRolePrefix, root, guard.JobNonce)
	t.Logf("preserved candidate evidence: %s", root)
	t.Cleanup(func() {
		// Close this fixture's test-only authority before releasing its work.
		// A cached, delayed cron command then rejects without starting payload.
		guard.Active = false
		_ = distributionAtomic(guardPath, guard, 0644)
		_ = distributionUserAtomic(filepath.Join(exchange, "detach-release"), true, 0644)
		_ = distributionUserAtomic(filepath.Join(exchange, "release"), true, 0644)
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		if e := distributionWait(cleanup, func() (bool, error) {
			// Inspect only the unique executable inode belonging to this test.
			// No descendant is signalled or counted as naturally complete early.
			wanted, e := os.Stat(executable)
			if e != nil {
				return false, e
			}
			processes, e := os.ReadDir("/proc")
			if e != nil {
				return false, e
			}
			for _, process := range processes {
				if _, e := strconv.Atoi(process.Name()); e != nil {
					continue
				}
				actual, e := os.Stat(filepath.Join("/proc", process.Name(), "exe"))
				if e == nil && os.SameFile(wanted, actual) {
					return false, nil
				}
			}
			return true, nil
		}); e != nil {
			t.Errorf("private fixture processes have not exited naturally; evidence preserved: %v", e)
			return
		}
		if raw, e := os.ReadFile(trigger); e == nil && string(raw) == content {
			if e = trustedFile(trigger); e == nil {
				_ = os.Remove(trigger)
			}
		}
		guard.Active = false
		_ = distributionAtomic(guardPath, guard, 0644)
	})
	file, err := os.OpenFile(trigger, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString(content); err == nil {
		err = file.Sync()
	}
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	var rootReceipt, userReceipt distributionReceipt
	if err = distributionWait(ctx, func() (bool, error) {
		a := distributionRead(filepath.Join(control, "root-receipt.json"), &rootReceipt)
		b := distributionRead(filepath.Join(exchange, "user-receipt.json"), &userReceipt)
		return a == nil && b == nil, nil
	}); err != nil {
		t.Fatal("real minute root and nonroot PAM receipts", err)
	}
	if rootReceipt.Nonce != guard.RootNonce || !rootReceipt.Origin || rootReceipt.Identity.UID != 0 || rootReceipt.Identity.Group != "0::/system.slice/cron.service" || userReceipt.Nonce != guard.JobNonce || !userReceipt.Origin || userReceipt.Identity.UID != credential.Uid || userReceipt.Identity.GID != credential.Gid || userReceipt.Identity.Boot != host.Boot {
		t.Fatal("root/nonroot receipts do not match the exact test trigger")
	}
	// Verify the still-live nonroot job from the independent root driver before
	// permitting detachment. A user-written receipt alone is not that proof.
	live, err := distributionProcess(userReceipt.Identity.PID)
	if err != nil || live != userReceipt.Identity || distributionAncestry(live.PID, host) != nil {
		t.Fatalf("nonroot job's live PAM/daemon provenance differs: %+v %v", live, err)
	}
	actual, err := os.Stat(fmt.Sprintf("/proc/%d/exe", live.PID))
	expected, expectedErr := os.Stat(executable)
	if err != nil || expectedErr != nil || !os.SameFile(actual, expected) {
		t.Fatal("nonroot live job is not the protected fixed test payload")
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", live.PID))
	if err != nil || string(cmdline) != executable+"\x00"+distributionRolePrefix+"job\x00"+root+"\x00"+guard.JobNonce+"\x00" {
		t.Fatal("nonroot live job argv differs from its exact trigger")
	}
	if err = distributionAtomic(filepath.Join(control, "live-nonroot-proof.json"), live, 0600); err != nil {
		t.Fatal(err)
	}
	if err = distributionUserAtomic(filepath.Join(exchange, "detach-release"), true, 0644); err != nil {
		t.Fatal(err)
	}
	var payload distributionIdentity
	if err = distributionWait(ctx, func() (bool, error) {
		e := distributionRead(filepath.Join(exchange, "payload-ready.json"), &payload)
		_, processErr := startKey(live.PID)
		return e == nil && processErr != nil, nil
	}); err != nil {
		t.Fatal("nonroot parent exit and detached grandchild readiness", err)
	}
	if payload.PID == live.PID || payload.UID != credential.Uid || payload.GID != credential.Gid || payload.Session != payload.PID || payload.Group != "0::/system.slice/cron.service" || payload.Boot != host.Boot {
		t.Fatal("detached non-PHP payload left its PAM execution domain", payload)
	}
	challenge := uuid.NewString()
	if err = distributionUserAtomic(filepath.Join(exchange, "challenge.json"), challenge, 0644); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Challenge string               `json:"challenge"`
		Identity  distributionIdentity `json:"identity"`
	}
	if err = distributionWait(ctx, func() (bool, error) {
		e := distributionRead(filepath.Join(exchange, "response.json"), &response)
		return e == nil && response.Challenge == challenge && response.Identity == payload, nil
	}); err != nil {
		t.Fatal("detached payload progress handshake", err)
	}
	if fresh, e := inspectDistributionCronCandidate(ctx, id); e != nil || fresh != host {
		t.Fatal("daemon or pinned profile changed during PAM/descendant observation", e)
	}
	if pids, e := groupPIDs("/system.slice/cron.service"); e != nil || !pids[payload.PID] {
		t.Fatal("detached non-PHP payload disappeared from hierarchical accounting", e)
	}
	if err = distributionUserAtomic(filepath.Join(exchange, "release"), true, 0644); err != nil {
		t.Fatal(err)
	}
	var completed distributionIdentity
	if err = distributionWait(ctx, func() (bool, error) {
		e := distributionRead(filepath.Join(exchange, "completed.json"), &completed)
		if e != nil || completed != payload {
			return false, nil
		}
		pids, e := groupPIDs("/system.slice/cron.service")
		return e == nil && len(pids) == 1 && pids[host.PID], e
	}); err != nil {
		t.Fatal("native cron hierarchy did not drain naturally to its sole pinned daemon", err)
	}
	if fresh, e := inspectDistributionCronCandidate(ctx, id); e != nil || fresh != host {
		t.Fatal("daemon changed before natural-drain evidence", e)
	}
	if raw, e := os.ReadFile(trigger); e != nil || string(raw) != content || trustedFile(trigger) != nil {
		t.Fatal("private trigger changed; automatic removal refused", e)
	}
	if err = os.Remove(trigger); err != nil {
		t.Fatal(err)
	}
	guard.Active = false
	if err = distributionAtomic(guardPath, guard, 0644); err != nil {
		t.Fatal(err)
	}
	if err = distributionAtomic(filepath.Join(control, "evidence.json"), map[string]any{"status": "PASS", "backend_candidate": id, "production_enabled": false, "pam_module_closure_qualified": false, "host": host, "root_receipt": rootReceipt, "nonroot_receipt": userReceipt, "live_nonroot_proof": live, "detached_payload": payload, "completed": completed, "natural_hierarchy_drain": true, "own_trigger_removed": true}, 0600); err != nil {
		t.Fatal(err)
	}
}
