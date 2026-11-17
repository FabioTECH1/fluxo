package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const backendProtocolRootPrefix = "/var/lib/fluxo-scheduler-backend-acceptance-"

type backendProtocolFixture struct {
	App, Payload, Database, StaleNonce string
	FullCaller                         bool
}

func backendProtocolHost() string {
	host, _ := os.Hostname()
	switch host {
	case "fluxo-bridge-jammy-20261101":
		return jammyCronCandidateID
	case "fluxo-bridge-resolute-20261101":
		return resoluteCronCandidateID
	case "fluxo-bridge-prod-20261101", "fluxo-bridge-noble-final-20261102":
		return legacyNobleCronContract
	}
	return ""
}

// Private acceptance selection preserves the native adapter matrix. Production
// continues to use its ordinary selector without any hostname override.
func backendFullCallerAdapter() executiondomain.AdapterID {
	if backendProtocolHost() == jammyCronCandidateID {
		return executiondomain.PersistentV1
	}
	return executiondomain.CgroupExitV1
}

func backendProtocolConfig(root string) (backendProtocolFixture, error) {
	var c backendProtocolFixture
	if backendProtocolHost() == "" || os.Geteuid() != 0 || !strings.HasPrefix(root, backendProtocolRootPrefix) {
		return c, errors.New("guarded private backend protocol fixture required")
	}
	id := strings.TrimPrefix(root, backendProtocolRootPrefix)
	if _, err := uuid.Parse(id); err != nil || root != backendProtocolRootPrefix+id {
		return c, errors.New("invalid backend protocol fixture")
	}
	path := filepath.Join(root, "protocol-fixture.json")
	if err := trustedFile(path); err != nil {
		return c, err
	}
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &c) != nil {
		return c, errors.New("invalid backend protocol fixture data")
	}
	wantDB := filepath.Join(root, "registration.db")
	if c.FullCaller {
		wantDB = "/var/lib/fluxo/fluxo.db"
	}
	if c.App != "/home/fluxo/cron-backend-acceptance-"+id || c.Payload != "/var/lib/fluxo-cron-backend-bin-"+id+"/payload" || c.Database != wantDB {
		return c, errors.New("backend protocol fixture paths disagree")
	}
	return c, nil
}

// Only the test executable has these private protocol roles. Production has no
// hostname, environment or marker override for distribution backend selection.
func init() {
	if len(os.Args) == 2 && os.Args[1] == "--execution-capabilities" {
		_ = json.NewEncoder(os.Stdout).Encode(executiondomain.Capabilities())
		os.Exit(0)
	}
	if len(os.Args) >= 3 && os.Args[1] == "distribution-backend-payload" {
		if backendProtocolHost() == "" || os.Geteuid() == 0 || !regexp.MustCompile(`^/home/fluxo/cron-backend-acceptance-[0-9a-f-]{36}$`).MatchString(os.Args[2]) {
			os.Exit(2)
		}
		app := os.Args[2]
		_ = writeAcceptanceFileAsFluxo(filepath.Join(app, "old-child-pid"), []byte(strconv.Itoa(os.Getpid())), 0600)
		deadline := time.Now().Add(10 * time.Minute)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(filepath.Join(app, "release")); err == nil {
				os.Exit(0)
			}
			_ = writeAcceptanceFileAsFluxo(filepath.Join(app, "old-child-progress"), []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0600)
			time.Sleep(50 * time.Millisecond)
		}
		os.Exit(1)
	}
	if len(os.Args) < 3 {
		return
	}
	exe, _ := os.Executable()
	root := filepath.Dir(exe)
	if filepath.Base(root) == "helpers" && strings.HasPrefix(filepath.Dir(root), backendProtocolRootPrefix) {
		root = filepath.Dir(root)
	}
	role := os.Args[1]
	if role == "distribution-backend-old-launch" || role == "distribution-backend-inspect" {
		root = os.Args[2]
	} else if !strings.HasPrefix(role, "scheduler-") || !strings.HasPrefix(root, backendProtocolRootPrefix) {
		return
	}
	c, err := backendProtocolConfig(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	stateDir, cronDir, databasePath = root, "/etc/cron.d", c.Database
	distributionCronQualificationEnabled = true
	if c.FullCaller {
		invocationAdapterSelector = func(context.Context) (executiondomain.AdapterID, error) { return backendFullCallerAdapter(), nil }
	}
	ctx := context.Background()
	switch role {
	case "distribution-backend-old-launch":
		f, e := os.OpenFile(filepath.Join(root, "old-launch.once"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(e) {
			os.Exit(0)
		}
		if e != nil {
			err = e
			break
		}
		_ = f.Close()
		credential, e := syscmd.ResolveCredential("fluxo")
		if e != nil || credential.Uid == 0 || credential.Gid == 0 {
			err = errors.New("nonroot fixture identity required")
			break
		}
		command := exec.Command(c.Payload, "distribution-backend-payload", c.App)
		command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
		command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Credential: &syscall.Credential{Uid: credential.Uid, Gid: credential.Gid, Groups: []uint32{}}}
		err = command.Start()
		if err == nil {
			_ = command.Process.Release()
		}
	case "scheduler-dispatch":
		if len(os.Args) != 4 {
			err = errors.New("fixture needs exact replacement receipt")
			break
		}
		id, e := strconv.Atoi(os.Args[2])
		if e != nil {
			err = e
			break
		}
		// This test qualifies the real provider receipt/migration protocol. It
		// intentionally does not execute a synthetic PHP application command.
		err = EntryReceipt(ctx, id, os.Args[3])
		if c.FullCaller {
			if err != nil {
				_ = distributionAtomic(filepath.Join(root, "caller-receipt-denied.json"), map[string]any{"id": id, "nonce": os.Args[3], "denied": true}, 0600)
			} else {
				_, err = Dispatch(ctx, id)
			}
		}
	case "scheduler-execute":
		if !c.FullCaller || len(os.Args) != 3 {
			err = errors.New("private full-caller execution required")
			break
		}
		j, e := read()
		if e != nil {
			err = e
			break
		}
		inv, ok := j.Invocations[os.Args[2]]
		if !ok || inv.Adapter != backendFullCallerAdapter() || !inv.Guarded {
			err = errors.New("adapter authority was not durably recorded before service execution")
			break
		}
		if err = distributionAtomic(filepath.Join(root, "caller-before-execute.json"), map[string]any{"unit": inv.Unit, "adapter": inv.Adapter, "started": inv.Started, "authority_before_application": true}, 0600); err == nil {
			err = Execute(ctx, os.Args[2])
		}
	case "scheduler-bootstrap":
		if !c.FullCaller || len(os.Args) != 3 {
			err = errors.New("private full-caller bootstrap required")
			break
		}
		j, e := read()
		if e != nil {
			err = e
			break
		}
		inv, ok := j.Invocations[os.Args[2]]
		if !ok || inv.Adapter != backendFullCallerAdapter() || !inv.Guarded || inv.ExecutionContract != executiondomain.SchedulerBootstrapContract {
			err = errors.New("versioned adapter/bootstrap authority was not saved before submission")
			break
		}
		if err = distributionAtomic(filepath.Join(root, "caller-before-execute.json"), map[string]any{"unit": inv.Unit, "adapter": inv.Adapter, "started": inv.Started, "authority_before_application": true, "execution_contract": inv.ExecutionContract}, 0600); err == nil {
			err = Bootstrap(ctx, os.Args[2])
		}
	case "scheduler-worker":
		if !c.FullCaller || len(os.Args) != 5 {
			err = errors.New("private full-caller worker required")
			break
		}
		fd, e := strconv.Atoi(os.Args[4])
		if e != nil {
			err = e
			break
		}
		err = ExecuteWorker(ctx, os.Args[2], os.Args[3], fd)
	case "scheduler-migration":
		if len(os.Args) != 4 {
			err = errors.New("fixture needs exact deletion receipt")
			break
		}
		err = Receipt(ctx, os.Args[2], os.Args[3], 0)
		if err != nil && c.StaleNonce != "" && os.Args[3] == c.StaleNonce {
			h, e := QualifyDistributionCronBackend(ctx, backendProtocolHost())
			if e == nil && cronReceiptOrigin(h) == nil {
				_ = distributionAtomic(filepath.Join(root, "stale-rejected.json"), map[string]any{"nonce": c.StaleNonce, "genuine_cron_origin": true, "rejected": true}, 0600)
			}
		}
	case "distribution-backend-inspect":
		p, e := readBackendProofs()
		err = e
		if err == nil {
			err = json.NewEncoder(os.Stdout).Encode(p)
		}
	default:
		err = errors.New("unknown private backend protocol role")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func backendProtocolWait(t *testing.T, ctx context.Context, label string, ready func() bool) {
	t.Helper()
	for !ready() {
		select {
		case <-ctx.Done():
			t.Fatal(label, ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// Real cron supplies replacement and deletion A/B receipts. Only the unrelated
// manual-panel proof is modeled inside this unique private journal; no global
// floor, installation receipt, account or production registration is fabricated.
func TestVMDistributionBackendMigrationProtocol(t *testing.T) {
	id := backendProtocolHost()
	if !newDistributionBackend(id) || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("explicit root opt-in on the dedicated Jammy/Resolute VM only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if err := executiondomain.GuardBackendWrite("/var/lib/fluxo", id, executiondomain.Capabilities()); err != nil {
		t.Fatal("first activate the actual compatible installed global formats; this test does not fabricate them", err)
	}
	oldState, oldCron, oldDB, oldEnabled := stateDir, cronDir, databasePath, distributionCronQualificationEnabled
	t.Cleanup(func() {
		stateDir, cronDir, databasePath, distributionCronQualificationEnabled = oldState, oldCron, oldDB, oldEnabled
		InvalidateBackendStatus()
	})
	fixtureID := uuid.NewString()
	stateDir, cronDir = backendProtocolRootPrefix+fixtureID, "/etc/cron.d"
	app, public := "/home/fluxo/cron-backend-acceptance-"+fixtureID, "/var/lib/fluxo-cron-backend-bin-"+fixtureID
	for path, mode := range map[string]os.FileMode{stateDir: 0700, public: 0755} {
		if err := os.Mkdir(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	credential, err := syscmd.ResolveCredential("fluxo")
	if err != nil || credential.Uid == 0 {
		t.Fatal("fixture identity", err)
	}
	if err = makeAcceptanceDirectoryAsFluxo(app); err != nil {
		t.Fatal(err)
	}
	databasePath = filepath.Join(stateDir, "registration.db")
	c := backendProtocolFixture{App: app, Payload: filepath.Join(public, "payload"), Database: databasePath}
	if err = distributionAtomic(filepath.Join(stateDir, "protocol-fixture.json"), c, 0600); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	image, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{filepath.Join(stateDir, "dispatcher"): 0700, c.Payload: 0755} {
		if err = os.WriteFile(path, image, mode); err != nil {
			t.Fatal(err)
		}
	}
	distributionCronQualificationEnabled = true
	host, err := QualifyDistributionCronBackend(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	// A saved receipt belongs to the exact daemon generation and interpreted
	// configuration, even when the provider contract label is unchanged.
	for name, changed := range map[string]legacyHost{
		"changed-daemon":      func() legacyHost { h := host; h.PID++; return h }(),
		"configuration-drift": func() legacyHost { h := host; h.Configuration = strings.Repeat("0", 64); return h }(),
		"unknown-contract":    func() legacyHost { h := host; h.Backend = "unknown-backend"; return h }(),
	} {
		if VerifyCronReceiptOrigin(ctx, changed) == nil {
			t.Fatal("receipt admitted incompatible recorded identity", name)
		}
	}
	if err = initialize(ctx); err != nil {
		t.Fatal(err)
	}
	// A synthetic unused PHP branch keeps deletion marker names away from
	// every installed application. The test helper executes receipts only.
	branch := "99.99"
	entryID := int(time.Now().UnixNano()%1000000000) + 1000000000
	if _, err = os.Lstat(markerPath(branch)); !os.IsNotExist(err) {
		t.Fatal("private branch marker already exists; preserve it", err)
	}
	for _, number := range []int{entryID, entryID + 1} {
		if _, err = os.Lstat(CronPath(number)); !os.IsNotExist(err) {
			t.Fatal("fixture cron path already exists", err)
		}
	}
	helper := filepath.Join(stateDir, "dispatcher")
	oldTrigger := []byte(fmt.Sprintf("# Private backend migration old workload\n* * * * * root %s distribution-backend-old-launch %s\n", helper, stateDir))
	if err = publishCron(CronPath(entryID), oldTrigger); err != nil {
		t.Fatal(err)
	}
	latePath := filepath.Join(cronDir, "fluxo-backend-late-"+fixtureID)
	if _, err = os.Lstat(latePath); !os.IsNotExist(err) {
		t.Fatal("late-receipt fixture path already exists", err)
	}
	t.Cleanup(func() {
		_ = writeAcceptanceFileAsFluxo(filepath.Join(app, "release"), []byte("release\n"), 0644)
		// All paths were proved absent before this test published its own files.
		// Removing them retires only test-generated launchers, never evidence.
		_ = os.Remove(CronPath(entryID))
		_ = os.Remove(CronPath(entryID + 1))
		_ = os.Remove(markerPath(branch))
		_ = os.Remove(latePath)
	})
	backendProtocolWait(t, ctx, "genuine old cron descendant", func() bool { _, e := os.Stat(filepath.Join(app, "old-child-pid")); return e == nil })
	pidRaw, _ := os.ReadFile(filepath.Join(app, "old-child-pid"))
	pid, _ := strconv.Atoi(string(pidRaw))
	group, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	session, sessionErr := unix.Getsid(pid)
	if err != nil || strings.TrimSpace(string(group)) != "0::"+host.controlGroup() || sessionErr != nil || session != pid {
		t.Fatal("old descendant was not independently sessioned inside the original daemon hierarchy", err, sessionErr)
	}
	command := "php" + branch + " " + filepath.Join(app, "artisan") + " schedule:run"
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE sites(id INTEGER PRIMARY KEY,app_type TEXT,path TEXT,deployment_strategy TEXT);CREATE TABLE crons(id INTEGER PRIMARY KEY,site_id INTEGER,command TEXT,expression TEXT,user TEXT);INSERT INTO sites VALUES(1,'laravel','','standard')`)
	if err == nil {
		_, err = db.Exec("UPDATE sites SET path=? WHERE id=1", app)
	}
	if err == nil {
		_, err = db.Exec("INSERT INTO crons VALUES(?,1,?,'* * * * *','fluxo')", entryID, command)
	}
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}
	e, ok := Qualify(entryID, 1, app, "* * * * *", command, "fluxo")
	if !ok {
		t.Fatal("synthetic private registration refused")
	}
	e.Identity, e.LegacyBoot, e.MigrationNonce = uuid.NewString(), host.Boot, uuid.NewString()
	unregister, err := lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	j, err := read()
	if err != nil {
		unregister()
		t.Fatal(err)
	}
	// This isolated protocol fixture deliberately models a current core. The
	// actual global committed floor was checked above; no private floor or
	// installation readiness evidence is manufactured by this test.
	j.Schema = executiondomain.CurrentSchedulerSchema
	j.Entries[e.ID] = e
	j.Legacy[branch] = host.Boot
	j.Migrations[branch] = legacyMigration{Replacements: map[int]legacyTrigger{e.ID: {Nonce: e.MigrationNonce, Hash: hashBytes([]byte(CronContent(e)))}}, Deletions: map[int]bool{entryID + 1: true}, Marker: uuid.NewString(), MarkerPhase: 1}
	j.PanelProof = &panelProof{Boot: host.Boot, Binary: hashBytes(image)}
	err = save(j)
	unregister()
	if err != nil {
		t.Fatal(err)
	}
	// Core records the known replacement predecessor before normal backend
	// binding; this is private fixture setup, never a fabricated receipt.
	if err = publishCron(CronPath(e.ID), []byte(CronContent(e))); err != nil {
		t.Fatal(err)
	}
	if err = ReconcileBackendMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	backendProtocolWait(t, ctx, "replacement plus deletion receipt A", func() bool {
		u, e := lock(ctx)
		if e != nil {
			return false
		}
		defer u()
		p, e := readBackendProofs()
		a := p.Branches[branch]
		return e == nil && a.Migration.Replacements[entryID].Received && a.Migration.MarkerReceived && a.Migration.MarkerPhase == 1
	})
	p, err := readBackendProofs()
	if err != nil {
		t.Fatal(err)
	}
	first := p.Branches[branch]
	if first.Certified {
		t.Fatal("receipt A certified old live work")
	}
	c.StaleNonce = first.Migration.Marker
	if err = distributionAtomic(filepath.Join(stateDir, "protocol-fixture.json"), c, 0600); err != nil {
		t.Fatal(err)
	}
	if err = ReconcileBackendMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	p, err = readBackendProofs()
	if err != nil || p.Branches[branch].Migration.MarkerPhase != 2 || p.Branches[branch].Migration.Marker == c.StaleNonce {
		t.Fatal("deletion B did not acquire distinct durable authority", err)
	}
	if err = publishCron(latePath, []byte(fmt.Sprintf("# Private stale receipt\n* * * * * root %s scheduler-migration %s %s\n", helper, branch, c.StaleNonce))); err != nil {
		t.Fatal(err)
	}
	// A fresh helper process must read the same durable attempt after A/B
	// transition. No process-local registration state substitutes for evidence.
	out, err := syscmd.Run(ctx, 10*time.Second, helper, "distribution-backend-inspect", stateDir)
	var restarted backendProofs
	if err != nil || json.Unmarshal([]byte(out), &restarted) != nil || restarted.Branches[branch].ID != first.ID {
		t.Fatal("fresh helper lost its migration attempt", err)
	}
	backendProtocolWait(t, ctx, "genuine deletion B and stale A rejection", func() bool {
		p, e := readBackendProofs()
		_, stale := os.Stat(filepath.Join(stateDir, "stale-rejected.json"))
		return e == nil && p.Branches[branch].Migration.MarkerReceived && stale == nil
	})
	if err = BranchReady(ctx, branch); err == nil {
		t.Fatal("live old descendant failed to block readiness after receipts")
	}
	if err = writeAcceptanceFileAsFluxo(filepath.Join(app, "release"), []byte("release\n"), 0644); err != nil {
		t.Fatal(err)
	}
	backendProtocolWait(t, ctx, "natural hierarchical drainage", func() bool { return legacyDomainDrained(ctx, host) == nil })
	if err = ReconcileBackendMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	j, err = read()
	if err != nil || j.Entries[entryID].Identity != e.Identity || !j.Entries[entryID].Ready || j.Legacy[branch] != "" {
		t.Fatal("completed migration lost registration identity or readiness", err)
	}
	p, err = readBackendProofs()
	if err != nil || p.Format != 2 || !p.Branches[branch].Certified || p.Branches[branch].Host != host {
		t.Fatal("completed proof lost its exact distribution identity", err)
	}
	if _, err = os.Stat(CronPath(entryID + 1)); !os.IsNotExist(err) {
		t.Fatal("deleted application trigger reappeared", err)
	}
	if err = Disable(ctx, entryID); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(CronPath(entryID)); err != nil {
		t.Fatal(err)
	}
	evidence := map[string]any{"status": "PASS", "backend": id, "registration_identity_preserved": true, "real_replacement_receipt": true, "real_deletion_A_B": true, "stale_A_from_real_cron_rejected": true, "fresh_helper_recovery": true, "old_detached_work_blocked_readiness": true, "natural_drain": true, "proof_format": p.Format, "panel_boundary_is_private_fixture_model": true, "application_execution_qualified": false, "normal_product_selection_enabled": false}
	if err = distributionAtomic(filepath.Join(stateDir, "protocol-evidence.json"), evidence, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("real provider protocol evidence retained at %s; application dispatch and actual panel boundary remain separate gates", stateDir)
}
