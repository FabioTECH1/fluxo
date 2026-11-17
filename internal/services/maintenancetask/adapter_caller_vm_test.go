package maintenancetask

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/scheduler"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

// This fixture adds only uniquely named private journal state and one identified
// row to the real panel DB. It does not fabricate a floor or a positive receipt.
// Test-only selection calls the real reviewed host qualifier and runtime; it
// never substitutes an APT/Certbot profile, application runner or systemd unit.
type taskCallerManifest struct {
	Format                                                                               int `json:"format"`
	ID                                                                                   int `json:"id"`
	UUID, Root, Host, Boot, Image, Backend, FloorID, FloorPlan, Command, App, Expression string
	AllowReceipts                                                                        bool `json:"allow_receipts"`
}

const taskCallerPrefix = "fluxo-task-adapter-caller-"

func taskCallerKnownHost(host string) bool {
	return host == "fluxo-bridge-jammy-20261101" || host == "fluxo-bridge-resolute-20261101"
}

func taskCallerBackend(host string) string {
	if host == "fluxo-bridge-jammy-20261101" {
		return "ubuntu-jammy-cron-v1"
	}
	if host == "fluxo-bridge-resolute-20261101" {
		return "ubuntu-resolute-cron-v1"
	}
	return ""
}

func readTaskCallerManifest(root, exe string) (taskCallerManifest, error) {
	var m taskCallerManifest
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || !taskCallerKnownHost(host) || filepath.Dir(root) != "/var/lib" || !strings.HasPrefix(filepath.Base(root), taskCallerPrefix) {
		return m, errors.New("native task caller host/root guard")
	}
	token := strings.TrimPrefix(filepath.Base(root), taskCallerPrefix)
	if _, err := uuid.Parse(token); err != nil {
		return m, err
	}
	raw, err := readRegular(filepath.Join(root, "manifest.json"), 1<<20)
	if err != nil {
		return m, err
	}
	if err = json.Unmarshal(raw, &m); err != nil {
		return m, err
	}
	boot, err := bootID()
	if err != nil {
		return m, err
	}
	if m.Format != 1 || m.ID <= 0 || m.UUID != token || m.Root != root || m.Host != host || m.Boot != boot || m.Backend != taskCallerBackend(host) || m.App != "/home/fluxo/"+taskCallerPrefix+token || m.Command != "/usr/bin/python3 "+m.App+"/payload.py" {
		return m, errors.New("native caller manifest identity differs")
	}
	if _, ok := QualifyApplication(m.ID, 0, "", m.Expression, m.Command, "fluxo"); !ok {
		return m, errors.New("native caller manifest application contract is invalid")
	}
	floor, err := executiondomain.ReadFloor("/var/lib/fluxo")
	if err != nil {
		return m, err
	}
	if floor == nil || floor.Phase != "committed" || floor.SchedulerSchema != executiondomain.CurrentSchedulerSchema || floor.SystemTaskSchema != executiondomain.CurrentSystemTaskSchema || floor.ActivationID != m.FloorID || floor.PlanSHA256 != m.FloorPlan {
		return m, errors.New("native caller requires its actual committed current floor")
	}
	b, err := readRegular(exe, 128<<20)
	if err != nil {
		return m, err
	}
	if digest(b) != m.Image {
		return m, errors.New("native caller image differs from protected manifest")
	}
	allowed := filepath.Join(root, "caller.test")
	helper := filepath.Join(root, "state", "helpers", m.Image)
	if exe != allowed && exe != helper {
		return m, errors.New("native caller executable outside manifest namespace")
	}
	return m, nil
}

func selectTaskCaller(m taskCallerManifest) {
	stateRoot = filepath.Join(m.Root, "state")
	invocationAdapterSelector = func(ctx context.Context) (executiondomain.AdapterID, error) {
		if err := executiondomain.RequireAdapterHost(ctx, executiondomain.PersistentV1); err != nil {
			return "", err
		}
		return executiondomain.PersistentV1, nil
	}
	qualifyRegistrationBackend = func(ctx context.Context) (scheduler.CronBackendIdentity, error) {
		return scheduler.QualifyDistributionCronBackend(ctx, m.Backend)
	}
}

func init() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return
	}
	root := filepath.Dir(exe)
	if filepath.Base(filepath.Dir(exe)) == "helpers" && filepath.Base(filepath.Dir(filepath.Dir(exe))) == "state" {
		root = filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	}
	if filepath.Dir(root) != "/var/lib" || !strings.HasPrefix(filepath.Base(root), taskCallerPrefix) {
		return
	}
	// Presence of this reserved private executable namespace is always strict:
	// malformed/missing manifests cannot fall through to a live global helper.
	m, err := readTaskCallerManifest(root, exe)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	selectTaskCaller(m)
	if len(os.Args) == 2 && os.Args[1] == "--execution-capabilities" {
		json.NewEncoder(os.Stdout).Encode(executiondomain.Capabilities())
		os.Exit(0)
	}
	ctx := context.Background()
	if len(os.Args) == 3 && os.Args[1] == "system-task-execute" {
		err = Execute(ctx, os.Args[2])
	} else if len(os.Args) == 3 && os.Args[1] == "system-task-application" {
		err = ExecuteApplication(ctx, os.Args[2])
	} else if len(os.Args) == 4 && (os.Args[1] == "system-task-dispatch" || os.Args[1] == "system-task-receipt") {
		id, e := strconv.Atoi(os.Args[2])
		if e != nil || id != m.ID {
			err = errors.New("native caller row differs")
		} else if os.Args[1] == "system-task-receipt" {
			if !m.AllowReceipts {
				err = errors.New("native fixture receipt phase is not open")
			} else {
				err = ReceiveReceipt(ctx, id, os.Args[3])
			}
		} else {
			_, err = Dispatch(ctx, id, os.Args[3])
			if err != nil && strings.Contains(err.Error(), "real loader acknowledgement") {
				// Only a genuine cron-origin negative attempt can create this
				// marker. A direct helper invocation cannot stand in for it.
				if j, e := read(); e == nil {
					if r, ok := j.Entries[id]; ok && !r.Received && scheduler.VerifyCronReceiptOrigin(ctx, r.Backend) == nil {
						b, _ := json.Marshal(struct {
							ID                 int
							Nonce, Boot, Error string
						}{id, r.Nonce, r.Boot, err.Error()})
						if e = atomicFile(filepath.Join(root, "unreceived-cron-refusal.json"), b, 0600); e != nil {
							err = e
						}
					}
				}
			}
		}
	} else {
		err = errors.New("native caller accepts only fixed helper entrypoints")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

const taskCallerPayload = `import os,sys,json,time
root=os.path.dirname(os.path.abspath(__file__))
with open(root+'/launches','a') as f: f.write('start\n');f.flush();os.fsync(f.fileno())
if os.fork(): os._exit(0)
os.setsid()
if os.fork(): os._exit(0)
pid=os.getpid()
stat=open('/proc/self/stat').read();start=stat[stat.rfind(')')+2:].split()[19]
info=dict(pid=pid,start=start,boot=open('/proc/sys/kernel/random/boot_id').read().strip(),group=open('/proc/self/cgroup').read().strip(),uid=os.geteuid())
with open(root+'/ready','w') as f: json.dump(info,f);f.flush();os.fsync(f.fileno())
i=0
while not os.path.exists(root+'/release'):
 i+=1
 with open(root+'/progress','w') as f: f.write(str(i))
 time.sleep(.05)
with open(root+'/drained','w') as f: f.write('natural\n');f.flush();os.fsync(f.fileno())
os._exit(0)
`

type taskCallerChild struct {
	PID                int `json:"pid"`
	Start, Boot, Group string
	UID                int `json:"uid"`
}

func taskCallerPoll(t *testing.T, ctx context.Context, description string, ready func() bool) {
	t.Helper()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		if ready() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s: %v", description, ctx.Err())
		case <-tick.C:
		}
	}
}

func taskCallerRead(t *testing.T) journal {
	t.Helper()
	j, err := read()
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func taskCallerRun(t *testing.T, killHelper bool) {
	t.Helper()
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || !taskCallerKnownHost(host) || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("explicit root known22/26 VM caller acceptance only")
	}
	report, err := executiondomain.InspectCompatibility("/var/lib/fluxo")
	if err != nil || !report.Compatible {
		t.Fatalf("real current global compatibility required: %+v %v", report, err)
	}
	floor, err := executiondomain.ReadFloor("/var/lib/fluxo")
	if err != nil || floor == nil || floor.Phase != "committed" {
		t.Fatal("real current floor", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	token := uuid.NewString()
	root := "/var/lib/" + taskCallerPrefix + token
	app := "/home/fluxo/" + taskCallerPrefix + token
	if err = os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(app, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(app, "payload.py"), []byte(taskCallerPayload), 0644); err != nil {
		t.Fatal(err)
	}
	credential, err := syscmd.ResolveCredential("fluxo")
	if err != nil || credential.Uid == 0 {
		t.Fatal("nonroot fixture identity", err)
	}
	if err = os.Chown(app, int(credential.Uid), int(credential.Gid)); err != nil {
		t.Fatal(err)
	}
	// Release only this unique payload on every path. Evidence and rows remain.
	defer os.WriteFile(filepath.Join(app, "release"), []byte("natural\n"), 0644)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	image, err := readRegular(exe, 128<<20)
	if err != nil {
		t.Fatal("stage root-protected test binary before running", err)
	}
	if err = atomicFile(filepath.Join(root, "caller.test"), image, 0700); err != nil {
		t.Fatal(err)
	}
	boot, err := bootID()
	if err != nil {
		t.Fatal(err)
	}
	command := "/usr/bin/python3 " + app + "/payload.py"
	// One upcoming calendar minute gives cron a real negative dispatch. It
	// cannot issue a second tick after the later receipt enables Run now.
	target := time.Now().Add(2 * time.Minute).Truncate(time.Minute)
	expression := fmt.Sprintf("%d %d %d %d *", target.Minute(), target.Hour(), target.Day(), int(target.Month()))
	db, err := sql.Open("sqlite", "file:/var/lib/fluxo/fluxo.db?_pragma=busy_timeout(2000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	result, err := db.ExecContext(ctx, "INSERT INTO crons(site_id,name,expression,command,user) VALUES(0,?,?,?,'fluxo')", taskCallerPrefix+token, expression, command)
	if err != nil {
		t.Fatal(err)
	}
	row, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	m := taskCallerManifest{Format: 1, ID: int(row), UUID: token, Root: root, Host: host, Boot: boot, Image: digest(image), Backend: taskCallerBackend(host), FloorID: floor.ActivationID, FloorPlan: floor.PlanSHA256, Command: command, App: app, Expression: expression}
	writeManifest := func() {
		b, e := json.Marshal(m)
		if e != nil {
			t.Fatal(e)
		}
		if e = atomicFile(filepath.Join(root, "manifest.json"), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	writeManifest()
	oldRoot, oldSelector, oldBackend := stateRoot, invocationAdapterSelector, qualifyRegistrationBackend
	defer func() {
		stateRoot = oldRoot
		invocationAdapterSelector = oldSelector
		qualifyRegistrationBackend = oldBackend
	}()
	selectTaskCaller(m)
	c, ok := QualifyApplication(m.ID, 0, "", expression, command, "fluxo")
	if !ok {
		t.Fatal("fixture contract")
	}
	if err = Materialize(ctx, c, "/var/lib/fluxo/fluxo.db", true); err != nil {
		t.Fatal(err)
	}
	j := taskCallerRead(t)
	r := j.Entries[m.ID]
	before, err := readRegular(filepath.Join(stateRoot, "state.json"), 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Dispatch(ctx, m.ID, r.Identity); err == nil || !strings.Contains(err.Error(), "acknowledgement") {
		t.Fatal("unreceived direct dispatch admitted", err)
	}
	after, err := readRegular(filepath.Join(stateRoot, "state.json"), 8<<20)
	if err != nil || string(before) != string(after) {
		t.Fatal("unreceived dispatch changed invocation journal", err)
	}
	invoke := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, filepath.Join(root, "caller.test"), args...)
		cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
		return cmd.CombinedOutput()
	}
	if out, e := invoke("system-task-dispatch", strconv.Itoa(m.ID), r.Identity); e == nil || !strings.Contains(string(out), "acknowledgement") {
		t.Fatalf("unreceived CLI admitted: %v %s", e, out)
	}
	if err = ReceiveReceipt(ctx, m.ID, uuid.NewString()); err == nil {
		t.Fatal("wrong nonce admitted")
	}
	if len(taskCallerRead(t).Invocations) != 0 {
		t.Fatal("pre-receipt command wrote invocation authority")
	}
	taskCallerPoll(t, ctx, "actual cron dispatch refusal before receipt", func() bool {
		b, e := readRegular(filepath.Join(root, "unreceived-cron-refusal.json"), 1<<20)
		if e != nil {
			return false
		}
		var refused struct {
			ID                 int
			Nonce, Boot, Error string
		}
		return json.Unmarshal(b, &refused) == nil && refused.ID == m.ID && refused.Nonce == r.Nonce && refused.Boot == boot && strings.Contains(refused.Error, "real loader acknowledgement")
	})
	if len(taskCallerRead(t).Invocations) != 0 {
		t.Fatal("real unreceived cron dispatch wrote authority")
	}
	m.AllowReceipts = true
	writeManifest()
	taskCallerPoll(t, ctx, "real current-origin cron receipt", func() bool { return taskCallerRead(t).Entries[m.ID].Received })
	r = taskCallerRead(t).Entries[m.ID]
	if err = requireLaunchRegistration(ctx, r, nil); err != nil {
		t.Fatal("real receipt not usable by launch", err)
	}
	unit, err := Dispatch(ctx, m.ID, r.Identity)
	if err != nil {
		t.Fatal(err)
	}
	_ = unit // public Dispatch returns a message; the private journal identifies its unit.
	var v invocation
	taskCallerPoll(t, ctx, "one consumed persistent authority and application readiness", func() bool {
		j = taskCallerRead(t)
		if len(j.Invocations) != 1 {
			return false
		}
		for _, candidate := range j.Invocations {
			v = candidate
		}
		_, e := os.Stat(filepath.Join(app, "ready"))
		return v.Status == "started" && v.ApplicationStarted && v.ServiceIdentity != nil && e == nil
	})
	if v.Adapter != executiondomain.PersistentV1 {
		t.Fatal("adapter not persisted before execution", v.Adapter)
	}
	raw, err := os.ReadFile(filepath.Join(app, "ready"))
	if err != nil {
		t.Fatal(err)
	}
	var child taskCallerChild
	if err = json.Unmarshal(raw, &child); err != nil {
		t.Fatal(err)
	}
	if child.UID != int(credential.Uid) || child.PID <= 1 || child.Boot != boot || child.Group != "0::/system.slice/"+v.Unit {
		t.Fatalf("detached nonroot child identity: %+v", child)
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", child.PID))
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(stat)[strings.LastIndex(string(stat), ")")+2:])
	if len(fields) < 20 || fields[19] != child.Start {
		t.Fatal("child PID generation changed")
	}
	p, err := inspectInvocation(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	progress, _ := os.ReadFile(filepath.Join(app, "progress"))
	if killHelper {
		pid, e := strconv.Atoi(p["MainPID"])
		if e != nil || pid <= 1 {
			t.Fatal("guard PID", p, e)
		}
		fd, e := unix.PidfdOpen(pid, 0)
		if e != nil {
			t.Fatal(e)
		}
		again, e := inspectInvocation(ctx, v)
		if e != nil || again["MainPID"] != strconv.Itoa(pid) {
			unix.Close(fd)
			t.Fatal("guard changed before fault", e)
		}
		e = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
		unix.Close(fd)
		if e != nil {
			t.Fatal(e)
		}
		taskCallerPoll(t, ctx, "helper death with retained payload", func() bool { now, e := inspectInvocation(ctx, v); return e == nil && now["MainPID"] == "0" })
		unlock, e := gate(ctx, false)
		if e != nil {
			t.Fatal(e)
		}
		j = taskCallerRead(t)
		e = settled(ctx, &j)
		unlock()
		if e == nil {
			t.Fatal("dead guard with populated domain was considered settled")
		}
		count := len(j.Invocations)
		if _, e = Dispatch(ctx, m.ID, r.Identity); e == nil {
			t.Fatal("dead guard allowed another task")
		}
		if len(taskCallerRead(t).Invocations) != count {
			t.Fatal("failed new launch wrote another authority")
		}
	} else {
		blocked, stop := context.WithTimeout(ctx, 300*time.Millisecond)
		lease, e := packages.Lifecycle(blocked, false)
		stop()
		if e == nil {
			lease()
			t.Fatal("running helper lost lifecycle lease")
		}
	}
	taskCallerPoll(t, ctx, "detached child progress", func() bool {
		now, e := os.ReadFile(filepath.Join(app, "progress"))
		return e == nil && len(now) > 0 && string(now) != string(progress)
	})
	if err = os.WriteFile(filepath.Join(app, "release"), []byte("natural\n"), 0644); err != nil {
		t.Fatal(err)
	}
	taskCallerPoll(t, ctx, "natural payload completion", func() bool {
		_, e := os.Stat(filepath.Join(app, "drained"))
		n, nerr := domainCount(v.Unit)
		return e == nil && nerr == nil && n == 0
	})
	if err = ReconcileInvocations(ctx); err != nil {
		t.Fatal("natural-empty recovery", err)
	}
	if e := Execute(ctx, v.Unit); e == nil {
		t.Fatal("late helper replayed consumed authority")
	}
	if e := ExecuteApplication(ctx, v.Unit); e == nil {
		t.Fatal("late application replayed consumed authority")
	}
	launches, err := os.ReadFile(filepath.Join(app, "launches"))
	if err != nil || string(launches) != "start\n" {
		t.Fatalf("payload did not execute exactly once: %q %v", launches, err)
	}
	retained, err := readRegular(filepath.Join(stateRoot, "history", v.Unit+".json"), 8<<20)
	if err != nil {
		t.Fatal("immutable outcome evidence missing", err)
	}
	var outcome invocation
	if err = json.Unmarshal(retained, &outcome); err != nil {
		t.Fatal(err)
	}
	if outcome.Adapter != executiondomain.PersistentV1 || !outcome.ApplicationStarted || outcome.ServiceIdentity == nil {
		t.Fatal("archive lost consumed execution identity")
	}
	if killHelper {
		if outcome.Status != "interrupted" || outcome.ReconciledFrom != "started" || !strings.Contains(outcome.Error, "unknown") {
			t.Fatal("helper death was mistaken for successful execution", outcome.Status, outcome.Error)
		}
	} else if outcome.Status != "completed" {
		t.Fatal("natural successful guard did not complete", outcome.Status, outcome.Error)
	}
	// Remove only our own exact trigger after real acknowledgement and natural
	// completion; preserve DB row, helper, journals, payload and evidence.
	trigger, err := readRegular(cronPath(m.ID), 1<<20)
	if err != nil || string(trigger) != cronContent(r) {
		t.Fatal("own trigger changed; preserve it", err)
	}
	if err = os.Remove(cronPath(m.ID)); err != nil {
		t.Fatal(err)
	}
	evidence, _ := json.MarshalIndent(struct {
		Manifest     taskCallerManifest
		Invocation   invocation
		Child        taskCallerChild
		HelperKilled bool
	}{m, v, child, killHelper}, "", "  ")
	if err = atomicFile(filepath.Join(root, "evidence.json"), evidence, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("actual task Dispatch→Execute→Application persisted %s; kill=%t; evidence %s", executiondomain.PersistentV1, killHelper, root)
}

func TestVMTaskPersistentCallerSuccess(t *testing.T)     { taskCallerRun(t, false) }
func TestVMTaskPersistentCallerHelperDeath(t *testing.T) { taskCallerRun(t, true) }
