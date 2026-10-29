package maintenancetask

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"fluxo/internal/releaseinfo"
	"fluxo/internal/services/packages"
	"fluxo/internal/syscmd"
	"golang.org/x/sys/unix"
)

// TestMain mirrors only the fixed production helper entry points so the retained
// executable used by these tests exercises real systemd and cron dispatch.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "system-task-") {
		var err error
		ctx := context.Background()
		switch os.Args[1] {
		case "system-task-execute":
			if len(os.Args) != 3 {
				os.Exit(2)
			}
			err = Execute(ctx, os.Args[2])
		case "system-task-application":
			if len(os.Args) != 3 {
				os.Exit(2)
			}
			err = ExecuteApplication(ctx, os.Args[2])
		case "system-task-dispatch", "system-task-receipt":
			if len(os.Args) != 4 {
				os.Exit(2)
			}
			id, e := strconv.Atoi(os.Args[2])
			if e != nil {
				os.Exit(2)
			}
			if os.Args[1] == "system-task-receipt" {
				err = ReceiveReceipt(ctx, id, os.Args[3])
			} else {
				_, err = Dispatch(ctx, id, os.Args[3])
			}
		case "system-task-apt-refuse":
			os.Exit(1)
		default:
			os.Exit(2)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func liveVM(t *testing.T) {
	t.Helper()
	host, _ := os.Hostname()
	if os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || os.Geteuid() != 0 || host != "fluxo-php-patch-test" {
		t.Skip("guarded local VM only")
	}
	if stateRoot != "/var/lib/fluxo-cron-audit-20261029-live" {
		t.Fatal("private state root required")
	}
}

func TestVMManagedApplicationDrain(t *testing.T) {
	liveVM(t)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	const root = "/var/lib/fluxo-cron-audit-20261029-live-data"
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatal("fresh private data required", err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "fluxo.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE crons(id INTEGER,site_id INTEGER,expression TEXT,command TEXT,user TEXT);
 CREATE TABLE node_update_operations(kind TEXT,status TEXT);CREATE TABLE node_update_generations(state TEXT);
 CREATE TABLE node_update_barriers(unit TEXT);CREATE TABLE node_update_php_barriers(unit TEXT);
 CREATE TABLE php_update_operations(kind TEXT,status TEXT);CREATE TABLE php_update_generations(state TEXT);
 CREATE TABLE php_update_barriers(unit TEXT);CREATE TABLE php_repository_migrations(state TEXT);`)
	if err != nil {
		t.Fatal(err)
	}
	const marker = "/home/fluxo/cron-admission-20261029/events"
	c, ok := QualifyApplication(211901, 0, "", "0 0 1 1 *", "/usr/bin/python3 /home/fluxo/cron-admission-20261029/job.py", "fluxo")
	if !ok {
		t.Fatal("application qualification")
	}
	if _, err = db.Exec("INSERT INTO crons VALUES(?,0,?,?,?)", c.ID, c.Expression, c.Command, c.User); err != nil {
		t.Fatal(err)
	}
	if err = Materialize(ctx, c, path, true); err != nil {
		t.Fatal(err)
	}
	t.Log("published exact fixture; awaiting real cron nonce and legacy-domain drain")
	for {
		_, err = Inspect(ctx, c, path)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(err)
		case <-time.After(time.Second):
		}
	}
	t.Log("real cron receipt and whole legacy-domain empty proof accepted")
	if _, err = db.Exec("INSERT INTO node_update_php_barriers VALUES('own-test')"); err != nil {
		t.Fatal(err)
	}
	msg, err := RunNow(ctx, c, path)
	if err != nil || !strings.Contains(msg, "skipped") {
		t.Fatal(msg, err)
	}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("pending maintenance launched app", err)
	}
	if _, err = db.Exec("DELETE FROM node_update_php_barriers"); err != nil {
		t.Fatal(err)
	}
	if _, err = RunNow(ctx, c, path); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		b, _ := os.ReadFile(marker)
		if strings.Contains(string(b), "child-start") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("application did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}
	lockctx, done := context.WithTimeout(ctx, 600*time.Millisecond)
	release, e := packages.Lifecycle(lockctx, false)
	done()
	if e == nil {
		release()
		t.Fatal("exclusive maintenance passed a running descendant")
	}
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	if _, err = Inspect(ctx, c, path); err != nil {
		t.Fatal("live guarded job must remain reviewable", err)
	}
	t.Log("live child authenticated; exclusive maintenance waits while review remains allowed")
	for {
		j, e := read()
		if e != nil {
			t.Fatal(e)
		}
		complete := false
		for _, v := range j.Invocations {
			if v.Status == "completed" {
				if v.Error != "" {
					t.Fatal(v.Error)
				}
				complete = true
			}
			if v.Status == "failed" || v.Status == "blocked" {
				t.Fatal(v.Error)
			}
		}
		if complete {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("completion timeout")
		case <-time.After(100 * time.Millisecond):
		}
	}
	b, err := os.ReadFile(marker)
	if err != nil || !strings.Contains(string(b), "v24.18.0") || !strings.Contains(string(b), "child-done") {
		t.Fatal(string(b), err)
	}
	release, err = packages.Lifecycle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	err = Quiescent(ctx)
	release()
	if err != nil {
		t.Fatal(err)
	}
	if err = Retire(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	t.Log("completed child under exact Node24; full domain drained; maintenance admitted; fixture trigger retired")
}

func TestVMManagedInterruptedNoReplay(t *testing.T) {
	liveVM(t)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	path := "/var/lib/fluxo-cron-audit-20261029-live-data/fluxo.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c, ok := QualifyApplication(211902, 0, "", "0 0 1 1 *", "/usr/bin/python3 /home/fluxo/cron-admission-20261029/job.py", "fluxo")
	if !ok {
		t.Fatal("contract")
	}
	if _, err = db.Exec("INSERT INTO crons VALUES(?,0,?,?,?)", c.ID, c.Expression, c.Command, c.User); err != nil {
		t.Fatal(err)
	}
	marker := "/home/fluxo/cron-admission-20261029/events"
	original, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	initial := strings.Count(string(original), "child-start")
	if err = Materialize(ctx, c, path, true); err != nil {
		t.Fatal(err)
	}
	for {
		_, err = Inspect(ctx, c, path)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(err)
		case <-time.After(time.Second):
		}
	}
	if _, err = RunNow(ctx, c, path); err != nil {
		t.Fatal(err)
	}
	for {
		b, _ := os.ReadFile(marker)
		if strings.Count(string(b), "child-start") == initial+1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("child start timeout")
		case <-time.After(50 * time.Millisecond):
		}
	}
	j, err := read()
	if err != nil {
		t.Fatal(err)
	}
	var v invocation
	for _, candidate := range j.Invocations {
		if candidate.Registration.Contract.ID == c.ID && candidate.Status == "started" {
			v = candidate
		}
	}
	if v.Unit == "" {
		t.Fatal("missing consumed authority")
	}
	p, err := inspectInvocation(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	if err = liveGuard(v, p); err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(p["MainPID"])
	if err != nil || pid <= 1 {
		t.Fatal("PID", p, err)
	}
	beforeID, err := processIdentity(pid)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	// Revoke only this fixture's future launches while its already admitted
	// descendants remain alive. Removing its SQL row must not erase the orphan.
	if err = Retire(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("DELETE FROM crons WHERE id=?", c.ID); err != nil {
		t.Fatal(err)
	}
	afterID, err := processIdentity(pid)
	if err != nil || beforeID != afterID {
		t.Fatal("guard identity drift", err)
	}
	if err = liveGuard(v, p); err != nil {
		t.Fatal(err)
	}
	if err = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil {
		t.Fatal(err)
	}
	for {
		_, e := processIdentity(pid)
		if os.IsNotExist(e) {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("guard did not exit")
		case <-time.After(50 * time.Millisecond):
		}
	}
	if err = Quiescent(ctx); err == nil {
		t.Fatal("orphan invocation did not block maintenance")
	}
	t.Log("exact fixture guard killed; deleted row still cannot hide unresolved descendants")
	for {
		n, e := domainCount(v.Unit)
		if e != nil {
			t.Fatal(e)
		}
		if n == 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("natural child drain timeout")
		case <-time.After(100 * time.Millisecond):
		}
	}
	if err = Quiescent(ctx); err == nil {
		t.Fatal("empty domain silently erased unknown execution outcome")
	}
	if err = ReconcileInvocations(ctx); err != nil {
		t.Fatal(err)
	}
	if err = Quiescent(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := readRegular(filepath.Join(stateRoot, "history", v.Unit+".json"), 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"status":"interrupted"`) || !strings.Contains(string(raw), `"reconciled_from":"started"`) {
		t.Fatal(string(raw))
	}
	if err = Execute(ctx, v.Unit); err == nil {
		t.Fatal("retired consumed authority replayed")
	}
	b, err := os.ReadFile(marker)
	if err != nil || strings.Count(string(b), "child-start") != initial+1 || strings.Count(string(b), "child-done") != initial+1 {
		t.Fatal(string(b), err)
	}
	t.Log("natural child exit; explicit reconciliation preserved unknown outcome; stale execution rejected without replay")
}

func TestVMFixedRootProfiles(t *testing.T) {
	liveVM(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	release, err := packages.Lifecycle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	releasePkg, err := packages.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer releasePkg()
	releaseNative, err := packages.NativeDPKG(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseNative()
	for _, kind := range []Kind{PackageCleanup, CertificateRenewal} {
		p, e := qualifyProfile(ctx, kind)
		if e != nil {
			t.Fatal(kind, e)
		}
		t.Log(kind, "qualified", len(p.Files), "protected code/config inputs")
	}
	fixture, err := readRegular("/opt/fluxo-cron-audit-20261029-r4/composer.fixture", 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	if digest(fixture) != releaseinfo.ComposerSHA256 {
		t.Fatal("unapproved Composer fixture")
	}
	const target = "/usr/local/bin/composer"
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
	if err != nil {
		t.Fatal("original Composer absence required", err)
	}
	if _, err = f.Write(fixture); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err = f.Sync(); err != nil {
		f.Close()
		t.Fatal(err)
	}
	info, err := f.Stat()
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		now, e := os.Lstat(target)
		if e != nil || !os.SameFile(info, now) {
			t.Error("Composer fixture identity drift; retained for review")
			return
		}
		raw, e := readRegular(target, 8<<20)
		if e != nil || digest(raw) != releaseinfo.ComposerSHA256 {
			t.Error("Composer fixture bytes drift; retained for review")
			return
		}
		if e = os.Remove(target); e != nil {
			t.Error(e)
		}
	}()
	p, err := qualifyProfile(ctx, ComposerUpdate)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("release-authenticated Composer and installed PHP CLI qualified", len(p.Files), "protected inputs; no Composer code executed")
}

func TestVMFixedRootGuardedExecution(t *testing.T) {
	liveVM(t)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	const dbPath = "/var/lib/fluxo-cron-audit-20261029-live-data/fluxo.db"
	const target = "/usr/local/bin/composer"
	dataRoots := []string{"/root/.composer", "/root/.config/composer", "/root/.cache/composer", "/root/.local/share/composer"}
	for _, path := range append(dataRoots, target) {
		if _, e := os.Lstat(path); !os.IsNotExist(e) {
			t.Fatal("original absence required", path, e)
		}
	}
	original, err := readRegular("/opt/fluxo-cron-audit-20261029-r4/composer.fixture", 8<<20)
	if err != nil || digest(original) != releaseinfo.ComposerSHA256 {
		t.Fatal("fixture", err)
	}
	release, err := packages.Lifecycle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	packageRelease, err := packages.Acquire(ctx)
	if err != nil {
		release()
		t.Fatal(err)
	}
	native, err := packages.NativeDPKG(ctx)
	if err != nil {
		packageRelease()
		release()
		t.Fatal(err)
	}
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
	if err == nil {
		_, err = f.Write(original)
		if err == nil {
			err = f.Sync()
		}
		f.Close()
	}
	native()
	packageRelease()
	release()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var contracts []Contract
	for i, command := range []string{PackageCleanupCommand, CertificateRenewalCommand, ComposerUpdateCommand} {
		c, ok := Qualify(211910+i, 0, "0 0 1 1 *", command, "root")
		if !ok {
			t.Fatal("root contract")
		}
		if _, err = db.Exec("INSERT INTO crons VALUES(?,0,?,?,?)", c.ID, c.Expression, c.Command, c.User); err != nil {
			t.Fatal(err)
		}
		if err = Materialize(ctx, c, dbPath, true); err != nil {
			t.Fatal(err)
		}
		contracts = append(contracts, c)
	}
	for _, c := range contracts {
		for {
			_, err = Inspect(ctx, c, dbPath)
			if err == nil {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal(err)
			case <-time.After(time.Second):
			}
		}
	}
	t.Log("all three exact root contracts acknowledged by real cron and eligible for maintenance")
	if _, err = db.Exec("INSERT INTO node_update_barriers VALUES('own-root-test')"); err != nil {
		t.Fatal(err)
	}
	for _, c := range contracts {
		msg, e := RunNow(ctx, c, dbPath)
		if e != nil || !strings.Contains(msg, "skipped") {
			t.Fatal(c.Kind, msg, e)
		}
	}
	if _, err = db.Exec("DELETE FROM node_update_barriers"); err != nil {
		t.Fatal(err)
	}
	// Do not autoclean the VM's retained package archive fixtures. Exercise actual
	// Certbot (no lineages) and Composer through the complete production guard.
	for _, c := range contracts[1:] {
		if c.Kind == CertificateRenewal {
			if es, e := os.ReadDir("/etc/letsencrypt/renewal"); e != nil && !os.IsNotExist(e) || len(es) != 0 {
				t.Fatal("no renewal lineage fixture required", e)
			}
		}
		if _, err = RunNow(ctx, c, dbPath); err != nil {
			t.Fatal(c.Kind, err)
		}
		for {
			j, e := read()
			if e != nil {
				t.Fatal(e)
			}
			done := false
			for _, v := range j.Invocations {
				if v.Registration.Contract.ID != c.ID {
					continue
				}
				if v.Status == "failed" || v.Status == "blocked" {
					t.Fatal(c.Kind, v.Status, v.Error)
				}
				if v.Status == "completed" {
					if v.Error != "" {
						t.Fatal(c.Kind, v.Error)
					}
					done = true
				}
			}
			if done {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal(c.Kind, "completion timeout")
			case <-time.After(200 * time.Millisecond):
			}
		}
		t.Log(c.Kind, "real guarded execution completed and external/profile evidence verified")
	}
	for _, c := range contracts {
		if err = Retire(ctx, c.ID); err != nil {
			t.Fatal(err)
		}
	}
	release, err = packages.Lifecycle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err = Quiescent(ctx); err != nil {
		t.Fatal(err)
	}
	packageRelease, err = packages.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer packageRelease()
	native, err = packages.NativeDPKG(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer native()
	// Preserve every new fixture effect before restoring the original absences.
	saved := map[string][]byte{}
	paths := append(dataRoots, target)
	for _, root := range paths {
		err = filepath.WalkDir(root, func(path string, e os.DirEntry, walkErr error) error {
			if os.IsNotExist(walkErr) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			if e.IsDir() {
				return trustedDatabaseDirectory(path)
			}
			raw, readErr := readRegular(path, 128<<20)
			if readErr != nil {
				return readErr
			}
			saved[path] = raw
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	evidence := "/opt/fluxo-cron-audit-20261029-r4/composer-effects.json"
	if _, err = os.Lstat(evidence); !os.IsNotExist(err) {
		t.Fatal("fresh evidence required")
	}
	if err = atomicFile(evidence, raw, 0600); err != nil {
		t.Fatal(err)
	}
	check, err := readRegular(evidence, 128<<20)
	if err != nil || digest(check) != digest(raw) {
		t.Fatal("effect archive readback", err)
	}
	for path, want := range saved {
		now, e := readRegular(path, 128<<20)
		if e != nil || digest(now) != digest(want) {
			t.Fatal("fixture drift before restore", path, e)
		}
	}
	for _, root := range dataRoots {
		if err = os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.Remove(target); err != nil {
		t.Fatal(err)
	}
	t.Log("own Composer PHAR/data effects archived and original program/home absences restored; no package cache deletion")
}

func TestVMAPTEmptyCleanupAndPreDpkgGuard(t *testing.T) {
	liveVM(t)
	if os.Getenv("FLUXO_CRON_CACHE_NAMESPACE") != "1" {
		t.Fatal("private mount namespace required")
	}
	ownNS, e1 := os.Readlink("/proc/self/ns/mnt")
	initNS, e2 := os.Readlink("/proc/1/ns/mnt")
	if e1 != nil || e2 != nil || ownNS == initNS {
		t.Fatal("distinct mount namespace required", e1, e2)
	}
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	release, err := packages.Lifecycle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	releasePkg, err := packages.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer releasePkg()
	const root = "/var/lib/fluxo-cron-audit-apt-20261029-r2"
	if err = os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(root, "cache")
	if err = os.MkdirAll(filepath.Join(cache, "archives", "partial"), 0755); err != nil {
		t.Fatal(err)
	}
	if err = unix.Mount(cache, "/var/cache/apt", "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		t.Fatal(err)
	}
	// This namespace exists only for the fixture process and its APT children.
	// Never shadow native locks, dpkg status, services, cron or application state.
	before, err := readRegular("/var/lib/dpkg/status", 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	p, err := qualifyProfile(ctx, PackageCleanup)
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	v := invocation{Registration: registration{Contract: Contract{Kind: PackageCleanup}, Helper: self}}
	if err = runFixed(ctx, v); err != nil {
		t.Fatal("empty plan/cache-only production execution", err)
	}
	after, err := readRegular("/var/lib/dpkg/status", 16<<20)
	if err != nil || digest(before) != digest(after) {
		t.Fatal("package state changed", err)
	}
	now, err := qualifyProfile(ctx, PackageCleanup)
	if err != nil || !compatibleProfiles(p, now, false, false) {
		t.Fatal("cleanup profile changed", err)
	}
	t.Log("actual empty autoremove/autoclean passed using production code and an isolated empty cache; host cached fixtures untouched")
	// A same-version reinstall provides a real nonempty dpkg boundary. The first
	// production refusal hook must abort before unpack/configuration or any later
	// pre-install/post-invoke hook. Simulate and restrict the plan first.
	deb := "/opt/fluxo-cron-audit-20261029-r4/python3-acme.deb"
	if raw, err := readRegular(deb, 8<<20); err != nil || digest(raw) != "206abf6d69b0c1e64e8ed47173ca651a3727135fbd557bc0cf350e8527599fb2" {
		t.Fatal("cached .deb identity changed", err)
	}
	archive := filepath.Join(cache, "archives", "python3-acme_2.9.0-1_all.deb")
	data, err := readRegular(deb, 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(archive, data, 0644); err != nil {
		t.Fatal(err)
	}
	var simulation bytes.Buffer
	err = syscmd.RunMaintenance(ctx, "", &simulation, nil, "/usr/bin/apt-get", "--simulate", "--reinstall", "--no-download", "install", "python3-acme=2.9.0-1", "-y")
	if err != nil {
		t.Fatal(err, simulation.String())
	}
	actions := 0
	for _, line := range strings.Split(simulation.String(), "\n") {
		if strings.HasPrefix(line, "Inst ") || strings.HasPrefix(line, "Conf ") {
			if !strings.HasPrefix(line, "Inst python3-acme ") && !strings.HasPrefix(line, "Conf python3-acme ") {
				t.Fatal("unapproved reinstall plan", line)
			}
			actions++
		}
		if strings.HasPrefix(line, "Remv ") || strings.HasPrefix(line, "Purg ") {
			t.Fatal("removal in guard fixture")
		}
	}
	if actions != 2 {
		t.Fatal("exact single-package plan required", simulation.String())
	}
	// Use the already retained original cache archive for this negative only.
	// The cache bind above is empty; bind the exact reviewed .deb supplied by the
	// launcher into it, without modifying the original cached fixture.

	marker := filepath.Join(root, "later-hook")
	args := []string{"-o", "DPkg::Pre-Invoke::=" + self + " system-task-apt-refuse", "-o", "DPkg::Pre-Install-Pkgs::=/usr/bin/touch " + marker, "-o", "DPkg::Post-Invoke::=/usr/bin/touch " + marker, "--reinstall", "--no-download", "install", "python3-acme=2.9.0-1", "-y"}
	var output bytes.Buffer
	err = syscmd.RunMaintenance(context.Background(), "", &output, nil, "/usr/bin/apt-get", args...)
	if err == nil || !strings.Contains(output.String(), "DPkg::Pre-Invoke") {
		t.Fatal("production refusal hook was not reached", err, output.String())
	}
	if _, e := os.Lstat(marker); !os.IsNotExist(e) {
		t.Fatal("later package hook executed", e)
	}
	after, err = readRegular("/var/lib/dpkg/status", 16<<20)
	if err != nil || digest(before) != digest(after) {
		t.Fatal("dpkg boundary was mutated", err)
	}
	t.Log("real APT nonempty reinstall refused at production first Pre-Invoke; later hooks absent and package status exact")
}

func TestVMOrphanBlocksOtherScheduledJobs(t *testing.T) {
	liveVM(t)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	const path = "/var/lib/fluxo-cron-audit-20261029-live-data/fluxo.db"
	const events = "/home/fluxo/cron-admission-20261029/events"
	const secondMarker = "/home/fluxo/cron-admission-20261029/orphan-second"
	if _, e := os.Lstat(secondMarker); !os.IsNotExist(e) {
		t.Fatal("fresh marker required")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	commands := []string{"/usr/bin/python3 /home/fluxo/cron-admission-20261029/job.py", "/bin/echo second >> " + secondMarker}
	var contracts []Contract
	for i, command := range commands {
		c, ok := QualifyApplication(211920+i, 0, "", "0 0 1 1 *", command, "fluxo")
		if !ok {
			t.Fatal("contract")
		}
		if _, err = db.Exec("INSERT INTO crons VALUES(?,0,?,?,?)", c.ID, c.Expression, c.Command, c.User); err != nil {
			t.Fatal(err)
		}
		if err = Materialize(ctx, c, path, true); err != nil {
			t.Fatal(err)
		}
		contracts = append(contracts, c)
	}
	for _, c := range contracts {
		for {
			_, err = Inspect(ctx, c, path)
			if err == nil {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal(err)
			case <-time.After(time.Second):
			}
		}
	}
	before, err := os.ReadFile(events)
	if err != nil {
		t.Fatal(err)
	}
	count := strings.Count(string(before), "child-start")
	if _, err = RunNow(ctx, contracts[0], path); err != nil {
		t.Fatal(err)
	}
	for {
		b, _ := os.ReadFile(events)
		if strings.Count(string(b), "child-start") == count+1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("child start timeout")
		case <-time.After(50 * time.Millisecond):
		}
	}
	j, err := read()
	if err != nil {
		t.Fatal(err)
	}
	var v invocation
	for _, candidate := range j.Invocations {
		if candidate.Registration.Contract.ID == contracts[0].ID && candidate.Status == "started" {
			v = candidate
		}
	}
	if v.Unit == "" {
		t.Fatal("missing own live invocation")
	}
	p, err := inspectInvocation(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	if err = liveGuard(v, p); err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(p["MainPID"])
	if err != nil || pid <= 1 {
		t.Fatal("PID", err)
	}
	id, err := processIdentity(pid)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	fresh, err := inspectInvocation(ctx, v)
	if err != nil || fresh["MainPID"] != p["MainPID"] {
		t.Fatal("guard changed", err)
	}
	if err = liveGuard(v, fresh); err != nil {
		t.Fatal(err)
	}
	again, err := processIdentity(pid)
	if err != nil || again != id {
		t.Fatal("guard identity changed", err)
	}
	if err = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil {
		t.Fatal(err)
	}
	for {
		_, e := processIdentity(pid)
		if os.IsNotExist(e) {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("guard exit timeout")
		case <-time.After(50 * time.Millisecond):
		}
	}
	if _, err = RunNow(ctx, contracts[1], path); err == nil {
		t.Fatal("orphan allowed another scheduled job")
	}
	if _, e := os.Stat(secondMarker); !os.IsNotExist(e) {
		t.Fatal("second job ran", e)
	}
	for {
		n, e := domainCount(v.Unit)
		if e != nil {
			t.Fatal(e)
		}
		if n == 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("child drain timeout")
		case <-time.After(100 * time.Millisecond):
		}
	}
	if _, err = RunNow(ctx, contracts[1], path); err == nil {
		t.Fatal("unknown result silently cleared after child exit")
	}
	if err = ReconcileInvocations(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = RunNow(ctx, contracts[1], path); err != nil {
		t.Fatal(err)
	}
	for {
		j, e := read()
		if e != nil {
			t.Fatal(e)
		}
		done := false
		for _, v := range j.Invocations {
			if v.Registration.Contract.ID == contracts[1].ID && v.Status == "completed" {
				if v.Error != "" {
					t.Fatal(v.Error)
				}
				done = true
			}
		}
		if done {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("second completion timeout")
		case <-time.After(100 * time.Millisecond):
		}
	}
	b, err := os.ReadFile(secondMarker)
	if err != nil || string(b) != "second\n" {
		t.Fatal(string(b), err)
	}
	b, err = os.ReadFile(events)
	if err != nil || strings.Count(string(b), "child-start") != count+1 {
		t.Fatal("first job replayed", err)
	}
	for _, c := range contracts {
		if err = Retire(ctx, c.ID); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("orphan blocked other scheduled jobs during and after natural drain; explicit reconciliation admitted second job once without replaying first")
}

func TestVMChangedRowCanRevokeGuardedJob(t *testing.T) {
	liveVM(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	const path = "/var/lib/fluxo-cron-audit-20261029-live-data/fluxo.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c, ok := QualifyApplication(211940, 0, "", "0 0 1 1 *", "/bin/true", "fluxo")
	if !ok {
		t.Fatal("contract")
	}
	if _, err = db.Exec("INSERT INTO crons VALUES(?,0,?,?,?)", c.ID, c.Expression, c.Command, c.User); err != nil {
		t.Fatal(err)
	}
	if err = Materialize(ctx, c, path, true); err != nil {
		t.Fatal(err)
	}
	// Drift before cron's acknowledgement must not make revocation
	// impossible: the receipt authenticates the old exact trigger, not a job.
	if _, err = db.Exec("UPDATE crons SET command='/bin/false' WHERE id=?", c.ID); err != nil {
		t.Fatal(err)
	}
	for {
		j, e := read()
		if e != nil {
			t.Fatal(e)
		}
		err = readyBackend(ctx, j.Entries[c.ID])
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(err)
		case <-time.After(time.Second):
		}
	}

	if _, err = RunNow(ctx, c, path); err == nil {
		t.Fatal("changed database row executed old authority")
	}
	changed := c
	changed.Command = "/bin/false"
	if _, err = RunNow(ctx, changed, path); err == nil {
		t.Fatal("changed command acquired old launch authority")
	}
	if err = Retire(ctx, c.ID); err != nil {
		t.Fatal("changed row prevented safe revocation", err)
	}
	if _, err = db.Exec("DELETE FROM crons WHERE id=?", c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(cronPath(c.ID)); !os.IsNotExist(err) {
		t.Fatal("retired trigger remains", err)
	}
	if err = Quiescent(ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("changed row rejected execution but exact guarded authority could be retired without restoring/editing history")
}

func TestVMInitializationAndOrphanRegistrationRecovery(t *testing.T) {
	liveVM(t)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	originalRoot := stateRoot
	for _, scenario := range []string{"before-directory", "empty-directory", "saved-empty", "saved-anchor"} {
		stateRoot = "/var/lib/fluxo-cron-init-20261029-" + scenario
		f, err := os.OpenFile(stateRoot+".lock", os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			stateRoot = originalRoot
			t.Fatal(err)
		}
		if err = writeGatePhase(f, initializingGate); err != nil {
			f.Close()
			stateRoot = originalRoot
			t.Fatal(err)
		}
		f.Close()
		if scenario != "before-directory" {
			if err = os.Mkdir(stateRoot, 0700); err != nil {
				stateRoot = originalRoot
				t.Fatal(err)
			}
		}
		if scenario == "saved-empty" || scenario == "saved-anchor" {
			if err = save(journal{1, map[int]registration{}, map[string]invocation{}}); err != nil {
				stateRoot = originalRoot
				t.Fatal(err)
			}
		}
		if scenario == "saved-anchor" {
			if err = atomicFile(stateRoot+".initialized", []byte("1\n"), 0600); err != nil {
				stateRoot = originalRoot
				t.Fatal(err)
			}
		}
		if unlock, e := gate(ctx, false); e == nil {
			unlock()
			stateRoot = originalRoot
			t.Fatal("read-only gate completed interrupted initialization")
		}
		unlock, e := gate(ctx, true)
		if e != nil {
			stateRoot = originalRoot
			t.Fatal(scenario, e)
		}
		unlock()
		j, e := read()
		if e != nil || len(j.Entries) != 0 || len(j.Invocations) != 0 {
			stateRoot = originalRoot
			t.Fatal(scenario, e)
		}
		// A missing completed journal must never be replaced with an empty one.
		if e = os.Rename(filepath.Join(stateRoot, "state.json"), filepath.Join(stateRoot, "saved-empty.json")); e != nil {
			stateRoot = originalRoot
			t.Fatal(e)
		}
		if unlock, e = gate(ctx, true); e == nil {
			unlock()
			stateRoot = originalRoot
			t.Fatal("lost history was silently recreated")
		}
		if e = os.Rename(filepath.Join(stateRoot, "saved-empty.json"), filepath.Join(stateRoot, "state.json")); e != nil {
			stateRoot = originalRoot
			t.Fatal(e)
		}
		stateRoot = originalRoot
	}
	t.Log("four interrupted initialization boundaries resumed only from durable intent; read-only queries and missing-history resets refused")
	const path = "/var/lib/fluxo-cron-audit-20261029-live-data/fluxo.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c, ok := QualifyApplication(211950, 0, "", "0 0 1 1 *", "/bin/true", "fluxo")
	if !ok {
		t.Fatal("contract")
	}
	if _, err = db.Exec("INSERT INTO crons VALUES(?,0,?,?,?)", c.ID, c.Expression, c.Command, c.User); err != nil {
		t.Fatal(err)
	}
	if err = Materialize(ctx, c, path, true); err != nil {
		t.Fatal(err)
	}
	// Model the durable publication/save boundary: the trigger exists but the
	// publication flag was not persisted, then API compensation removed its row.
	unlock, err := gate(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	j, err := read()
	if err != nil {
		unlock()
		t.Fatal(err)
	}
	r := j.Entries[c.ID]
	r.Published = false
	j.Entries[c.ID] = r
	err = save(j)
	unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("DELETE FROM crons WHERE id=?", c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = RunNow(ctx, c, path); err == nil {
		t.Fatal("orphan ran")
	}
	if err = ReconcileInvocations(ctx); err == nil || !strings.Contains(err.Error(), "acknowledgement") {
		t.Fatal("partial orphan did not retain bounded reload checkpoint", err)
	}
	for {
		err = ReconcileInvocations(ctx)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(err)
		case <-time.After(time.Second):
		}
	}
	j, err = read()
	if err != nil || j.Entries[c.ID].Enabled {
		t.Fatal("orphan authority was not revoked", err)
	}
	if _, err = os.Lstat(cronPath(c.ID)); !os.IsNotExist(err) {
		t.Fatal("orphan trigger remains", err)
	}
	if err = Quiescent(ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("partial publication plus absent DB row reconciled through exact reload/natural-drain proof and revocation, without a job launch or record recreation")
}
