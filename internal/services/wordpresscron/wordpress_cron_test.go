package wordpresscron

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/cron"
	"fluxo/internal/services/maintenancetask"
	_ "modernc.org/sqlite"
)

func TestConfigOwnsOnlyItsBlock(t *testing.T) {
	original := []byte("<?php\n// a custom setting\ndefine('DB_NAME', 'preserved');\nrequire_once ABSPATH . 'wp-settings.php';\n")
	on, err := Config(original, true)
	if err != nil || bytes.Equal(on, original) {
		t.Fatal(string(on), err)
	}
	again, err := Config(on, true)
	if err != nil || !bytes.Equal(on, again) {
		t.Fatal("enable was not idempotent", err)
	}
	off, err := Config(on, false)
	if err != nil || !bytes.Equal(off, original) {
		t.Fatal("changed unrelated configuration", err)
	}
	for _, content := range []string{
		"<?php define('DISABLE_WP_CRON', false);", "<?php define('DISABLE_WP_CRON', true);",
		"<?php define('MULTISITE', true);", "<?php define('ALTERNATE_WP_CRON', true);",
		"<?php" + Block + Block, "<?php // BEGIN FLUXO WORDPRESS CRON\n", "not PHP",
	} {
		if _, err := Config([]byte(content), true); err == nil {
			t.Fatal("accepted custom/ambiguous config", content)
		}
	}
}

func TestConfigAtomicWritePreservesPermissionsAndRejectsConcurrentEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wp-config.php")
	before := []byte("<?php\n// unchanged\n")
	if err := os.WriteFile(path, before, 0640); err != nil {
		t.Fatal(err)
	}
	after, _ := Config(before, true)
	if err := writeConfig(path, before, after, func(string) error { return errors.New("lint failed") }); err == nil {
		t.Fatal("ignored lint failure")
	}
	actual, _ := os.ReadFile(path)
	if !bytes.Equal(actual, before) {
		t.Fatal("changed configuration on failed validation")
	}
	if err := writeConfig(path, before, after, func(string) error { return os.WriteFile(path, []byte("<?php // concurrent"), 0640) }); err == nil {
		t.Fatal("overwrote concurrent edit")
	}
	if err := os.WriteFile(path, before, 0640); err != nil {
		t.Fatal(err)
	}
	if err := writeConfig(path, before, after, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0640 {
		t.Fatal("changed config permissions")
	}
	link := filepath.Join(t.TempDir(), "link.php")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfig(link); err == nil {
		t.Fatal("followed configuration symlink")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfig(path); err == nil {
		t.Fatal("accepted hardlinked configuration")
	}
}

type fixture struct {
	s                     *Service
	raw                   []byte
	events                []string
	publishErr, retireErr error
	ids                   []int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(database.BaseSchema + database.WordPressCronSchema); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO sites(id,domain,path,web_root,php_version,app_type,deployment_strategy) VALUES(1,'wp.test','/home/fluxo/wp.test','/public','8.4','wordpress','standard')"); err != nil {
		t.Fatal(err)
	}
	f := &fixture{raw: []byte("<?php\n// preserve me\n")}
	f.s = New(db)
	f.s.read = func(string) ([]byte, error) { return bytes.Clone(f.raw), nil }
	f.s.probe = func(_ context.Context, _ time.Duration, user, dir, bin string, args ...string) (string, error) {
		if user != "fluxo" || dir != "/home/fluxo/wp.test" || bin != "/usr/bin/php8.4" {
			t.Fatalf("wrong execution identity: %s %s %s", user, dir, bin)
		}
		f.events = append(f.events, "probe")
		return "", nil
	}
	f.s.configuration = func(context.Context, maintenancetask.Contract, string) string { return "configured" }
	f.s.write = func(_ string, old, next []byte, lint func(string) error) error {
		if !bytes.Equal(f.raw, old) {
			t.Fatal("lost concurrent edit")
		}
		if err := lint("fixture.php"); err != nil {
			return err
		}
		f.events = append(f.events, "config")
		f.raw = bytes.Clone(next)
		return nil
	}
	f.s.publish = func(_ context.Context, id, siteID int, dir, expression, cmd string, discard func() error) error {
		var saved string
		if err := db.QueryRow("SELECT command FROM crons WHERE id=?", id).Scan(&saved); err != nil || saved != cmd {
			t.Fatal("publication preceded committed contract", err)
		}
		c, ok := maintenancetask.QualifySite(id, siteID, dir, "standard", "wordpress", ".", expression, cmd, "fluxo")
		if !ok || c.Kind != maintenancetask.ApplicationJob {
			t.Fatal("not an existing guarded application job", c)
		}
		f.ids = append(f.ids, id)
		f.events = append(f.events, "publish")
		if errors.Is(f.publishErr, cron.ErrNoGuardedRegistration) && discard != nil {
			if err := discard(); err != nil {
				return err
			}
		}
		return f.publishErr
	}
	f.s.retire = func(context.Context, int) error {
		if bytes.Contains(f.raw, []byte(Block)) {
			t.Fatal("replacement revoked before restoring visitor scheduling")
		}
		f.events = append(f.events, "retire")
		return f.retireErr
	}
	return f
}

func TestEnableDisableUsesOneGuardedJobAndRestoresOnlyOwnedConfig(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	status, err := f.s.Status(ctx, 1)
	if err != nil || status.Enabled || status.Phase != "off" || len(f.events) != 0 {
		t.Fatal(status, err, f.events)
	}
	before := bytes.Clone(f.raw)
	status, err = f.s.Set(ctx, 1, true)
	if err != nil || !status.Enabled {
		t.Fatal(status, err)
	}
	if strings.Join(f.events, ",") != "probe,publish,probe,config" {
		t.Fatal("incorrect transition", f.events)
	}
	if _, err = f.s.Set(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	var n int
	f.s.db.QueryRow("SELECT count(*) FROM crons").Scan(&n)
	if n != 1 || f.ids[0] != f.ids[1] {
		t.Fatal("duplicated managed job", n, f.ids)
	}
	status, err = f.s.Status(ctx, 1)
	if err != nil || status.Phase != "enabled" {
		t.Fatal(status, err)
	}
	f.raw = append(f.raw, []byte("// later editor change\n")...)
	status, err = f.s.Set(ctx, 1, false)
	if err != nil || status.Enabled || !bytes.Equal(f.raw, append(before, []byte("// later editor change\n")...)) {
		t.Fatal("lost config or could not disable", status, err, string(f.raw))
	}
	f.s.db.QueryRow("SELECT count(*) FROM crons").Scan(&n)
	if n != 0 {
		t.Fatal("retained database job after successful retirement")
	}
}

func TestInterruptedPublicationRetainsRowAndVisitorSchedulingForRetry(t *testing.T) {
	f := newFixture(t)
	before := bytes.Clone(f.raw)
	f.publishErr = errors.New("backend unavailable")
	if _, err := f.s.Set(context.Background(), 1, true); err == nil {
		t.Fatal("reported successful publication")
	}
	if !bytes.Equal(f.raw, before) {
		t.Fatal("disabled visitor scheduling before publication")
	}
	status, err := f.s.Status(context.Background(), 1)
	if err != nil || status.Phase != "enabling" || status.CronID == 0 {
		t.Fatal(status, err)
	}
	f.publishErr = nil
	if _, err = f.s.Set(context.Background(), 1, true); err != nil {
		t.Fatal(err)
	}
	if len(f.ids) != 2 || f.ids[0] != f.ids[1] {
		t.Fatal("retry created a second job", f.ids)
	}
	f.retireErr = errors.New("retirement unavailable")
	if _, err = f.s.Set(context.Background(), 1, false); err == nil {
		t.Fatal("claimed completed retirement")
	}
	if bytes.Contains(f.raw, []byte(Block)) {
		t.Fatal("did not restore visitor fallback")
	}
	status, err = f.s.Status(context.Background(), 1)
	if err != nil || status.Phase != "disabling" {
		t.Fatal(status, err)
	}
	if _, err = f.s.Set(context.Background(), 1, true); err == nil {
		t.Fatal("reenabled during incomplete retirement")
	}
	f.retireErr = nil
	if _, err = f.s.Set(context.Background(), 1, false); err != nil {
		t.Fatal(err)
	}
}

func TestCustomSchedulesAndUnknownConfigArePreserved(t *testing.T) {
	for _, custom := range []bool{true, false} {
		f := newFixture(t)
		if custom {
			f.raw = []byte("<?php define('DISABLE_WP_CRON',true);")
		} else {
			f.s.db.Exec("INSERT INTO crons(site_id,expression,command,user) VALUES(1,'*/5 * * * *','wp cron event run --due-now','fluxo')")
		}
		before := bytes.Clone(f.raw)
		if _, err := f.s.Set(context.Background(), 1, true); err == nil {
			t.Fatal("adopted custom scheduling")
		}
		if !bytes.Equal(f.raw, before) || len(f.ids) != 0 {
			t.Fatal("changed custom config/job")
		}
	}
}

func TestProvenUnpublishedRefusalDoesNotLeaveAnUnknownMaintenanceConsumer(t *testing.T) {
	f := newFixture(t)
	before := bytes.Clone(f.raw)
	f.publishErr = cron.ErrNoGuardedRegistration
	if _, err := f.s.Set(context.Background(), 1, true); err == nil {
		t.Fatal("reported successful registration")
	}
	state, err := f.s.Status(context.Background(), 1)
	if err != nil || state.Phase != "off" || !bytes.Equal(f.raw, before) {
		t.Fatal(state, err)
	}
	var rows int
	if err := f.s.db.QueryRow("SELECT count(*) FROM crons").Scan(&rows); err != nil || rows != 0 {
		t.Fatal("refused optional feature left an unknown runtime consumer", rows, err)
	}
}

func TestGeneratedCommandUsesSelectedPHPAndActuallySkipsOverlap(t *testing.T) {
	if _, err := os.Stat("/usr/bin/php8.4"); err != nil {
		t.Skip("PHP 8.4 CLI unavailable")
	}
	dir := t.TempDir()
	root := filepath.Join(dir, "public")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	// Synthetic application, no WordPress database/plugin or host services.
	script := `<?php file_put_contents(__DIR__.'/started', 'x', FILE_APPEND); usleep(400000);`
	if err := os.WriteFile(filepath.Join(root, "wp-cron.php"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := command(site{path: dir, root: root, php: "8.4"})
	a := exec.Command("/bin/sh", "-c", cmd)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if a.Process != nil {
			a.Process.Kill()
		}
	})
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first synthetic job did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if out, err := exec.Command("/bin/sh", "-c", cmd).CombinedOutput(); err != nil {
		t.Fatal("overlap was not a clean skipped tick", err, string(out))
	}
	if err := a.Wait(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "started"))
	if err != nil || string(raw) != "x" {
		t.Fatal("overlapping execution", string(raw), err)
	}
}
