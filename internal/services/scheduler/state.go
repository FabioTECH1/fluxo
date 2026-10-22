// Package scheduler coordinates Fluxo-owned Laravel scheduler invocations with
// PHP maintenance. Its journal is independent of the panel and survives reboot.
package scheduler

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fluxo/internal/syscmd"
	"fmt"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

var stateDir = "/var/lib/fluxo-scheduler"
var cronDir = "/etc/cron.d"
var databasePath = "/var/lib/fluxo/fluxo.db"

func SetDatabasePath(path string) { databasePath = path }

// Node maintenance needs the dispatcher version which holds the lifecycle
// lease for all descendants. Older launchers must be refreshed by the installer.
func NodeMaintenanceReady() error {
	if err := trustedFile(filepath.Join(stateDir, "dispatcher")); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	want, err := fileDigest(exe)
	if err != nil {
		return err
	}
	actual, err := fileDigest(filepath.Join(stateDir, "dispatcher"))
	if err != nil {
		return err
	}
	if actual != want {
		return errors.New("Refresh the managed Scheduler dispatcher with the current Fluxo installer before updating Node.js")
	}
	return nil
}

var branchRE = regexp.MustCompile(`^[0-9]{1,2}\.[0-9]{1,2}$`)
var pathRE = regexp.MustCompile(`^/home/fluxo/[a-zA-Z0-9_./-]+$`)
var ErrUnsupportedHost = errors.New("Automatic Scheduler maintenance needs systemd 250 or newer with cgroup v2")

func supported(ctx context.Context) error {
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		return ErrUnsupportedHost
	}
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", "--property=Version", "--value")
	if err != nil {
		return err
	}
	version := strings.TrimSpace(out)
	end := 0
	for end < len(version) && version[end] >= '0' && version[end] <= '9' {
		end++
	}
	n, err := strconv.Atoi(version[:end])
	if err != nil || n < 250 {
		return ErrUnsupportedHost
	}
	return nil
}

type Entry struct {
	Database       string `json:"database"`
	ID             int    `json:"id"`
	SiteID         int    `json:"site_id"`
	Identity       string `json:"identity"`
	Version        string `json:"version"`
	Directory      string `json:"directory"`
	Command        string `json:"command"`
	Enabled        bool   `json:"enabled"`
	LegacyBoot     string `json:"legacy_boot,omitempty"`
	Ready          bool   `json:"ready"`
	MigrationNonce string `json:"migration_nonce,omitempty"`
}

type invocation struct {
	Unit      string `json:"unit"`
	Entry     Entry  `json:"entry"`
	Boot      string `json:"boot"`
	Submitted bool   `json:"submitted"`
	Guarded   bool   `json:"guarded"`
	Started   bool   `json:"started"`
}
type journal struct {
	LastSweep   map[string]int64           `json:"last_sweep"`
	Schema      int                        `json:"schema"`
	Entries     map[int]Entry              `json:"entries"`
	Invocations map[string]invocation      `json:"invocations"`
	Holds       map[string]string          `json:"holds"`
	Released    map[string]bool            `json:"released"`
	Upgrade     *upgradeLease              `json:"upgrade"`
	Legacy      map[string]string          `json:"legacy"`
	Migrations  map[string]legacyMigration `json:"migrations"`
	PanelProof  *panelProof                `json:"panel_proof"`
}

func owned(path string, directory bool) error {
	s, err := os.Lstat(path)
	if err != nil {
		return err
	}
	var stat unix.Stat_t
	if err = unix.Lstat(path, &stat); err != nil {
		return err
	}
	if stat.Uid != 0 || s.Mode()&os.ModeSymlink != 0 || s.Mode().Perm()&0022 != 0 || s.IsDir() != directory || (!directory && (!s.Mode().IsRegular() || stat.Nlink != 1)) {
		return fmt.Errorf("Unsafe Scheduler state: %s", path)
	}
	return nil
}

func trustedFile(path string) error {
	if err := owned(path, false); err != nil {
		return err
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		if err := owned(parent, true); err != nil {
			return err
		}
		if parent == "/" {
			break
		}
	}
	return nil
}

func lock(ctx context.Context) (func(), error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("Scheduler management requires root")
	}
	if err := owned(stateDir+".initialized", false); err != nil {
		return nil, fmt.Errorf("Scheduler journal requires reconciliation: %w", err)
	}
	if err := owned(stateDir, true); err != nil {
		return nil, err
	}
	fd, err := unix.Open(filepath.Join(stateDir, "gate.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err = owned(filepath.Join(stateDir, "gate.lock"), false); err != nil {
		unix.Close(fd)
		return nil, err
	}
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() { unix.Flock(fd, unix.LOCK_UN); unix.Close(fd) }, nil
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN {
			unix.Close(fd)
			return nil, err
		}
		select {
		case <-ctx.Done():
			unix.Close(fd)
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func initialize(ctx context.Context) error {
	if os.Geteuid() != 0 {
		return errors.New("Scheduler initialization requires root")
	}
	fd, err := unix.Open(stateDir+".init.lock", unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err = owned(stateDir+".init.lock", false); err != nil {
		return err
	}
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if err != unix.EAGAIN && err != unix.EWOULDBLOCK {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	if _, err := os.Lstat(stateDir + ".initialized"); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Lstat(filepath.Join(stateDir, "state.json")); err == nil {
		return errors.New("Scheduler initialization marker is missing")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return err
	}
	if err := owned(stateDir, true); err != nil {
		return err
	}
	j := journal{LastSweep: map[string]int64{}, Schema: 2, Entries: map[int]Entry{}, Invocations: map[string]invocation{}, Holds: map[string]string{}, Legacy: map[string]string{}, Released: map[string]bool{}, Migrations: map[string]legacyMigration{}}
	if err := save(j); err != nil {
		return err
	}
	return atomicFile(stateDir+".initialized", []byte("1\n"), 0600)
}
func absent() bool {
	_, a := os.Lstat(stateDir)
	_, b := os.Lstat(stateDir + ".initialized")
	return errors.Is(a, os.ErrNotExist) && errors.Is(b, os.ErrNotExist)
}
func read() (journal, error) {
	var j journal
	p := filepath.Join(stateDir, "state.json")
	if err := owned(p, false); err != nil {
		return j, fmt.Errorf("Scheduler journal requires reconciliation: %w", err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return j, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return j, err
	}
	for _, key := range []string{"schema", "entries", "invocations", "holds", "released", "legacy", "upgrade", "last_sweep"} {
		if _, ok := fields[key]; !ok {
			return j, errors.New("Incomplete Scheduler journal")
		}
	}
	var nested struct {
		Entries     map[string]json.RawMessage `json:"entries"`
		Invocations map[string]struct {
			Entry json.RawMessage `json:"entry"`
		} `json:"invocations"`
	}
	if err = json.Unmarshal(raw, &nested); err != nil {
		return j, err
	}
	validateEntryFields := func(raw json.RawMessage) error {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		for _, key := range []string{"database", "id", "site_id", "identity", "version", "directory", "command", "enabled", "ready"} {
			if value, ok := fields[key]; !ok || string(value) == "null" {
				return errors.New("Incomplete Scheduler entry")
			}
		}
		return nil
	}
	for _, raw := range nested.Entries {
		if err = validateEntryFields(raw); err != nil {
			return j, err
		}
	}
	for _, inv := range nested.Invocations {
		if err = validateEntryFields(inv.Entry); err != nil {
			return j, err
		}
	}
	if err = json.Unmarshal(raw, &j); err != nil {
		return j, err
	}
	if (j.Schema != 1 && j.Schema != 2) || j.Entries == nil || j.Invocations == nil || j.Holds == nil || j.Legacy == nil || j.Released == nil || j.LastSweep == nil {
		return j, errors.New("Incomplete or unsupported Scheduler journal")
	}
	if j.Schema == 2 {
		if _, ok := fields["panel_proof"]; !ok || j.Migrations == nil {
			return j, errors.New("Incomplete Scheduler migration journal")
		}
	} else {
		j.Migrations = map[string]legacyMigration{}
	}
	for branch, m := range j.Migrations {
		if !branchRE.MatchString(branch) || m.Replacements == nil || m.Deletions == nil || m.MarkerPhase < 0 || m.MarkerPhase > 2 {
			return j, errors.New("Invalid Scheduler migration journal")
		}
		if m.Host != nil {
			if _, err := uuid.Parse(m.Host.Boot); err != nil || m.Host.PID <= 1 || m.Host.Start == "" || m.Host.GroupInode == 0 {
				return j, errors.New("Invalid Scheduler migration host")
			}
		}
		for id, target := range m.Replacements {
			if _, err := uuid.Parse(target.Nonce); err != nil || id <= 0 || len(target.Hash) != 64 {
				return j, errors.New("Invalid Scheduler migration trigger")
			}
		}
		if len(m.Deletions) > 0 {
			if _, err := uuid.Parse(m.Marker); err != nil || m.MarkerPhase == 0 {
				return j, errors.New("Invalid Scheduler deletion migration")
			}
		}
	}
	if err = validateUpgrade(j.Upgrade); err != nil {
		return j, err
	}
	for id, e := range j.Entries {
		if e.ID != id || !filepath.IsAbs(e.Database) {
			return j, errors.New("Invalid Scheduler registration journal")
		}
		if _, err = uuid.Parse(e.Identity); err != nil {
			return j, err
		}
		if canonical, ok := Qualify(id, e.SiteID, e.Directory, "* * * * *", e.Command, "fluxo"); !ok || canonical.Version != e.Version {
			return j, errors.New("Invalid Scheduler entry")
		}
		if e.LegacyBoot != "" {
			if _, err = uuid.Parse(e.LegacyBoot); err != nil {
				return j, err
			}
		}
	}
	for name, inv := range j.Invocations {
		if name != inv.Unit || !strings.HasPrefix(name, "fluxo-scheduler-") || !strings.HasSuffix(name, ".service") {
			return j, errors.New("Invalid Scheduler invocation")
		}
		if _, err = uuid.Parse(strings.TrimSuffix(strings.TrimPrefix(name, "fluxo-scheduler-"), ".service")); err != nil {
			return j, err
		}
		if _, err = uuid.Parse(inv.Boot); err != nil {
			return j, err
		}
		e := inv.Entry
		if canonical, ok := Qualify(e.ID, e.SiteID, e.Directory, "* * * * *", e.Command, "fluxo"); !ok || canonical.Version != e.Version || !filepath.IsAbs(e.Database) {
			return j, errors.New("Invalid Scheduler invocation entry")
		}
		if _, err = uuid.Parse(e.Identity); err != nil {
			return j, err
		}
	}
	for branch, owner := range j.Holds {
		if !branchRE.MatchString(branch) {
			return j, errors.New("Invalid Scheduler hold branch")
		}
		if _, err = uuid.Parse(owner); err != nil {
			return j, err
		}
	}
	for branch, at := range j.LastSweep {
		if !branchRE.MatchString(branch) || at < 0 {
			return j, errors.New("Invalid Scheduler sweep journal")
		}
	}
	for key, done := range j.Released {
		branch, id, ok := strings.Cut(key, ":")
		if !ok || !done || !branchRE.MatchString(branch) {
			return j, errors.New("Invalid Scheduler release receipt")
		}
		if _, err = uuid.Parse(id); err != nil {
			return j, err
		}
	}
	for branch, boot := range j.Legacy {
		if !branchRE.MatchString(branch) {
			return j, errors.New("Invalid Scheduler legacy branch")
		}
		if _, err = uuid.Parse(boot); err != nil {
			return j, err
		}
	}
	j.Schema = 2
	return j, nil
}

// The launcher opens only the recorded root-owned database in read-only mode.
// An uncommitted, deleted, changed or unavailable registration never launches.
var ErrMaintenance = errors.New("Scheduler is paused for PHP maintenance")

func committed(ctx context.Context, e Entry, maintenance ...bool) error {
	if !filepath.IsAbs(e.Database) {
		return errors.New("Invalid Scheduler database path")
	}
	if err := trustedFile(e.Database); err != nil {
		return err
	}
	uri := url.URL{Scheme: "file", Path: e.Database}
	db, err := sql.Open("sqlite", uri.String()+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return err
	}
	defer db.Close()
	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM crons c JOIN sites s ON s.id=c.site_id WHERE c.id=? AND c.site_id=? AND c.command=? AND c.expression='* * * * *' AND c.user='fluxo' AND s.app_type='laravel'`, e.ID, e.SiteID, e.Command).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	if len(maintenance) > 0 && maintenance[0] {
		var nodeSchema int
		if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='node_update_generations'").Scan(&nodeSchema); err != nil {
			return err
		}
		if nodeSchema > 0 {
			var nodePending bool
			if err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM node_update_operations WHERE kind!='check' AND status IN ('queued','running')) OR EXISTS(SELECT 1 FROM node_update_generations WHERE state NOT IN ('succeeded','rolled_back')) OR EXISTS(SELECT 1 FROM node_update_barriers)`).Scan(&nodePending); err != nil {
				return err
			}
			if nodePending {
				return ErrMaintenance
			}
		}
		var exists int
		if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='php_update_generations'").Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM php_update_generations WHERE version=? AND state NOT IN ('succeeded','rolled_back')", e.Version).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				return ErrMaintenance
			}
		}
	}
	return nil
}

func ReconcileRegistrations(ctx context.Context) error {
	if absent() {
		return nil
	}
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	for id, e := range j.Entries {
		if !e.Enabled {
			continue
		}
		if err = committed(ctx, e); errors.Is(err, sql.ErrNoRows) {
			e.Enabled = false
			j.Entries[id] = e
		} else if err != nil {
			return err
		} else if !e.Ready && e.LegacyBoot != "" {
			boot, eerr := bootID()
			if eerr != nil {
				return eerr
			}
			if e.LegacyBoot != boot {
				if eerr = verifyRegistration(ctx, e); eerr != nil {
					return eerr
				}
				e.Ready = true
				j.Entries[id] = e
			}
		}
	}
	return save(j)
}

func atomicFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".scheduler-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func save(j journal) error {
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return atomicFile(filepath.Join(stateDir, "state.json"), raw, 0600)
}
func bootID() (string, error) {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(b))
	_, err = uuid.Parse(id)
	return id, err
}
func CronPath(id int) string { return filepath.Join(cronDir, "fluxo-cron-"+strconv.Itoa(id)) }
func CronContent(e Entry) string {
	nonce := ""
	if e.MigrationNonce != "" {
		nonce = " " + e.MigrationNonce
	}
	return fmt.Sprintf("# Fluxo Cron ID: %d\n* * * * * root %s scheduler-dispatch %d%s\n", e.ID, filepath.Join(stateDir, "dispatcher"), e.ID, nonce)
}

func verifyRegistration(ctx context.Context, e Entry) error {
	if err := committed(ctx, e); err != nil {
		return err
	}
	if err := owned(CronPath(e.ID), false); err != nil {
		return err
	}
	raw, err := os.ReadFile(CronPath(e.ID))
	if err != nil {
		return err
	}
	if string(raw) != CronContent(e) {
		return errors.New("Scheduler trigger changed after review")
	}
	return nil
}

// Qualify accepts only the exact generated, branch-pinned application command.
func Qualify(id, siteID int, dir, expression, command, user string) (Entry, bool) {
	f := strings.Fields(command)
	if id <= 0 || siteID <= 0 || user != "fluxo" || expression != "* * * * *" || len(f) != 3 || !strings.HasPrefix(f[0], "php") || !branchRE.MatchString(strings.TrimPrefix(f[0], "php")) || f[2] != "schedule:run" || !pathRE.MatchString(dir) || filepath.Clean(dir) != dir || f[1] != filepath.Join(dir, "artisan") || command != strings.Join(f, " ") {
		return Entry{}, false
	}
	return Entry{Database: databasePath, ID: id, SiteID: siteID, Version: strings.TrimPrefix(f[0], "php"), Directory: dir, Command: command, Enabled: true}, true
}

func identity(e Entry) string {
	e.Identity = ""
	e.LegacyBoot = ""
	e.Ready = false
	e.MigrationNonce = ""
	e.Enabled = true
	b, _ := json.Marshal(e)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Materialize never adopts a missing legacy file during startup. The caller
// supplies exact expected legacy bytes; operator-modified files fail closed.
func Materialize(ctx context.Context, e Entry, legacy string, create bool) error {
	if err := initialize(ctx); err != nil {
		return err
	}
	if err := supported(ctx); err != nil {
		return err
	}
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	if len(j.Holds) > 0 {
		return errors.New("Scheduler is held for PHP maintenance")
	}
	old, exists := j.Entries[e.ID]
	raw, err := os.ReadFile(CronPath(e.ID))
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return err
	}
	if missing && !create {
		return nil
	}
	if !missing {
		if err = owned(CronPath(e.ID), false); err != nil {
			return err
		}
	}
	guarded := exists && string(raw) == CronContent(old)
	if !missing && !guarded && string(raw) != legacy {
		return errors.New("Scheduler cron was modified; automatic adoption refused")
	}
	if exists && identity(old) != identity(e) && old.Enabled {
		return errors.New("Scheduler configuration changed; disable and enable Scheduler to recreate it")
	}
	if !exists || !old.Enabled {
		e.Identity = uuid.NewString()
		e.Ready = true
		if exists {
			e.LegacyBoot = old.LegacyBoot
			e.Ready = old.Ready || old.LegacyBoot == ""
		}
	} else {
		e = old
	}
	if (!missing && !guarded) || (e.LegacyBoot != "" && !e.Ready && e.MigrationNonce == "") {
		if err = addLegacyReplacement(ctx, &j, &e); err != nil {
			return err
		}
	}
	e.Enabled = true
	if e.LegacyBoot != "" && !e.Ready {
		boot, err := bootID()
		if err != nil {
			return err
		}
		if boot != e.LegacyBoot {
			e.Ready = true
		}
	}
	// A dedicated copy keeps existing cron launchers functional if a panel
	// upgrade is rolled back to an older binary without this CLI subcommand.
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	if err = trustedFile(exe); err != nil {
		return err
	}
	binary, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	if err = atomicFile(filepath.Join(stateDir, "dispatcher"), binary, 0700); err != nil {
		return err
	}
	j.Entries[e.ID] = e
	if err = save(j); err != nil {
		return err
	}
	return publishCron(CronPath(e.ID), []byte(CronContent(e)))
}

func Disable(ctx context.Context, id int) error {
	if absent() {
		return nil
	}
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	if len(j.Holds) > 0 {
		return errors.New("Scheduler is held for PHP maintenance")
	}
	if e, ok := j.Entries[id]; ok {
		if e.LegacyBoot != "" && !e.Ready && j.Legacy[e.Version] != "" {
			if err = addLegacyDeletion(ctx, &j, e.Version, id); err != nil {
				return err
			}
		}
		e.Enabled = false
		j.Entries[id] = e
		return save(j)
	}
	return nil
}

func Inspect(ctx context.Context, id int) (Entry, error) {
	if err := supported(ctx); err != nil {
		return Entry{}, err
	}
	unlock, err := lock(ctx)
	if err != nil {
		return Entry{}, err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return Entry{}, err
	}
	e, ok := j.Entries[id]
	if !ok {
		return e, errors.New("Scheduler needs migration before automatic PHP maintenance")
	}
	if !e.Enabled {
		return e, errors.New("Scheduler is disabled")
	}
	err = reconcileLegacy(ctx, &j, e.Version)
	if saveErr := save(j); saveErr != nil {
		return e, saveErr
	}
	e = j.Entries[id]
	if err != nil {
		return e, err
	}
	if err = owned(CronPath(id), false); err != nil {
		return e, err
	}
	raw, err := os.ReadFile(CronPath(id))
	if err != nil {
		return e, err
	}
	if string(raw) != CronContent(e) {
		return e, errors.New("Scheduler trigger differs from its verified configuration")
	}
	if !e.Ready {
		return e, errors.New("Scheduler migration needs review before PHP updates can pause it safely")
	}
	return e, nil
}

// RetireLegacy records the possibility of an already-spawned plain cron shell
// before its trigger is removed. Disabling/re-enabling must not erase that proof.
func RetireLegacy(ctx context.Context, content string) error {
	re := regexp.MustCompile(`\bphp([0-9]{1,2}\.[0-9]{1,2}) /home/fluxo/[a-zA-Z0-9_./-]+/artisan schedule:run\b`)
	matches := re.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	if err := initialize(ctx); err != nil {
		return err
	}
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	if len(j.Holds) > 0 {
		return errors.New("Scheduler is held for PHP maintenance")
	}
	boot, err := bootID()
	if err != nil {
		return err
	}
	for _, match := range matches {
		idMatch := regexp.MustCompile(`(?m)^# Fluxo Cron ID: ([0-9]+)$`).FindStringSubmatch(content)
		if len(idMatch) != 2 {
			return errors.New("Earlier Scheduler trigger needs manual migration review")
		}
		id, e := strconv.Atoi(idMatch[1])
		if e != nil {
			return e
		}
		j.Legacy[match[1]] = boot
		if err = addLegacyDeletion(ctx, &j, match[1], id); err != nil {
			return err
		}
	}
	return save(j)
}

func Pending() (bool, error) {
	if absent() {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlock, err := lock(ctx)
	if err != nil {
		return false, err
	}
	defer unlock()
	j, err := read()
	return len(j.Holds) > 0 || j.Upgrade != nil, err
}

func BranchReady(ctx context.Context, branch string) error {
	if absent() {
		return nil
	}
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	err = reconcileLegacy(ctx, &j, branch)
	if saveErr := save(j); saveErr != nil {
		return saveErr
	}
	return err
}

// Managed distinguishes a verified plain legacy cron from a registered wrapper.
// A lost wrapper journal is an error, never permission to execute around a hold.
func Managed(ctx context.Context, id int, legacy string) (bool, error) {
	if err := owned(CronPath(id), false); err != nil {
		return false, err
	}
	raw, err := os.ReadFile(CronPath(id))
	if err != nil {
		return false, err
	}
	if absent() {
		if string(raw) == legacy {
			return false, nil
		}
		return false, errors.New("Scheduler management state is missing")
	}
	unlock, err := lock(ctx)
	if err != nil {
		return false, err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return false, err
	}
	e, ok := j.Entries[id]
	if ok {
		if !e.Enabled || string(raw) != CronContent(e) {
			return false, errors.New("Scheduler management state differs from its trigger")
		}
		return true, nil
	}
	if string(raw) == legacy {
		return false, nil
	}
	return false, errors.New("Scheduler trigger is not recognized")
}
