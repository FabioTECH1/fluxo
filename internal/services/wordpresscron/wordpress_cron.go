package wordpresscron

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"fluxo/internal/safeinput"
	"fluxo/internal/services/cron"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/syscmd"
)

type Status struct {
	Enabled bool   `json:"enabled"`
	Phase   string `json:"phase"`
	CronID  int    `json:"cron_id,omitempty"`
	Message string `json:"message,omitempty"`
}

type Service struct {
	db            *sql.DB
	publish       func(context.Context, int, int, string, string, string, func() error) error
	retire        func(context.Context, int) error
	configuration func(context.Context, maintenancetask.Contract, string) string
	read          func(string) ([]byte, error)
	write         func(string, []byte, []byte, func(string) error) error
	probe         func(context.Context, time.Duration, string, string, string, ...string) (string, error)
}

func New(db *sql.DB) *Service {
	return &Service{db: db, publish: cron.CreateGuardedApplication, retire: cron.RetireGuardedApplication,
		configuration: maintenancetask.ConfigurationState, read: readConfig, write: writeConfig,
		probe: syscmd.RunApplicationProbe}
}

type site struct{ path, root, php string }
type saved struct {
	id            int
	phase, source string
}

func (s *Service) load(id int) (site, saved, error) {
	var v site
	var app, strategy, webRoot, deletion string
	err := s.db.QueryRow(`SELECT path,web_root,php_version,app_type,deployment_strategy,COALESCE(deletion_status,'') FROM sites WHERE id=?`, id).
		Scan(&v.path, &webRoot, &v.php, &app, &strategy, &deletion)
	if err != nil {
		return v, saved{}, err
	}
	if app != "wordpress" || strategy != "standard" || deletion != "" {
		return v, saved{}, errors.New("WordPress Cron is available only for an active, in-place WordPress site")
	}
	if v.path, err = safeinput.NormalizeManagedSitePath(v.path); err != nil {
		return v, saved{}, err
	}
	if v.root, err = safeinput.NormalizeWebRoot(v.path, webRoot); err != nil {
		return v, saved{}, err
	}
	if !safeinput.ValidatePHPVersion(v.php) {
		return v, saved{}, errors.New("Invalid site PHP version")
	}
	var state saved
	err = s.db.QueryRow("SELECT cron_id,phase,source_sha FROM wordpress_cron_settings WHERE site_id=?", id).Scan(&state.id, &state.phase, &state.source)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return v, state, err
}

func quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

// flock prevents overlap between system ticks; wp-cron.php retains WordPress's
// own doing_cron lock when an earlier visitor-triggered run is finishing.
func command(v site) string {
	return "/usr/bin/flock -n -E 0 " + quote(filepath.Join(v.path, ".fluxo-wordpress-cron.lock")) +
		" /usr/bin/php" + v.php + " -f " + quote(filepath.Join(v.root, "wp-cron.php"))
}

func fingerprint(raw []byte) string { return fmt.Sprintf("%x", sha256.Sum256(raw)) }

func (s *Service) Status(ctx context.Context, id int) (Status, error) {
	v, state, err := s.load(id)
	if err != nil {
		return Status{}, err
	}
	result := Status{Enabled: state.phase == "enabled", Phase: "off", CronID: state.id}
	if state.id == 0 {
		return result, nil
	}
	result.Phase = state.phase
	if state.phase != "enabled" {
		result.Message = "WordPress Cron setup is incomplete. Retry the change or turn it off to restore visitor-triggered scheduling."
		return result, nil
	}
	raw, err := s.read(filepath.Join(v.root, "wp-config.php"))
	if err != nil {
		result.Phase = "attention"
		result.Message = "WordPress configuration could not be read"
		return result, nil
	}
	want, e := Config(raw, true)
	if e != nil || !bytes.Equal(want, raw) {
		result.Phase = "attention"
		result.Message = "WordPress cron configuration changed. Review it before changing this toggle."
		return result, nil
	}
	var actual, expression, user string
	err = s.db.QueryRow("SELECT command,expression,user FROM crons WHERE id=? AND site_id=?", state.id, id).Scan(&actual, &expression, &user)
	if err != nil || actual != command(v) || expression != "* * * * *" || user != "fluxo" {
		result.Phase = "attention"
		result.Message = "The managed WordPress job changed or is missing. Review its configuration."
		return result, nil
	}
	var dbPath string
	if err = s.db.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&dbPath); err != nil {
		return Status{}, err
	}
	c, ok := maintenancetask.QualifyApplication(state.id, id, v.path, expression, actual, user)
	if !ok || s.configuration(ctx, c, dbPath) != "configured" {
		result.Phase = "attention"
		result.Message = "The managed WordPress trigger needs review. Retry setup or turn WordPress Cron off."
	}
	return result, nil
}

// Set is called under the existing site reservation and runtime mutation lease.
// A committed pending row is retained on error so a retry never creates a second
// job or loses published execution authority. No WP config contents are stored.
func (s *Service) Set(ctx context.Context, id int, enable bool) (Status, error) {
	v, state, err := s.load(id)
	if err != nil {
		return Status{}, err
	}
	if !enable && state.id == 0 {
		return Status{Phase: "off"}, nil
	}
	configPath := filepath.Join(v.root, "wp-config.php")
	raw, err := s.read(configPath)
	if err != nil {
		return Status{}, errors.New("Could not read wp-config.php; existing cron settings were preserved")
	}
	next, err := Config(raw, enable)
	if err != nil {
		return Status{}, err
	}
	lint := func(path string) error {
		_, err := s.probe(ctx, 10*time.Second, "fluxo", v.path, "/usr/bin/php"+v.php, "-l", path)
		if err != nil {
			return errors.New("WordPress configuration could not be validated; it was preserved")
		}
		return nil
	}
	if !enable {
		if _, err = s.db.Exec("UPDATE wordpress_cron_settings SET phase='disabling' WHERE site_id=?", id); err != nil {
			return Status{}, err
		}
		// Restore visitor scheduling before revoking the replacement. WordPress's
		// own lock protects the brief transition; running guarded work is retained.
		if !bytes.Equal(raw, next) {
			if err = s.write(configPath, raw, next, lint); err != nil {
				return Status{}, err
			}
		}
		if err = s.retire(ctx, state.id); err != nil {
			return Status{}, fmt.Errorf("WordPress visitor scheduling was restored; retry disabling the managed job: %w", err)
		}
		if err = s.forget(ctx, id, state.id); err != nil {
			return Status{}, err
		}
		return Status{Phase: "off"}, nil
	}
	if state.phase == "disabling" {
		return Status{}, errors.New("Finish disabling WordPress Cron before enabling it again")
	}
	if state.id == 0 {
		if bytes.Contains(raw, []byte(Block)) {
			return Status{}, errors.New("An existing Fluxo WordPress cron setting needs review; it was preserved")
		}
		// Never adopt/delete an administrator's existing site job. Their config
		// must be reviewed before introducing a second trigger.
		var duplicates int
		if err = s.db.QueryRow(`SELECT count(*) FROM crons WHERE site_id=? AND (lower(command) LIKE '%wp-cron.php%' OR lower(command) LIKE '%wp%cron%')`, id).Scan(&duplicates); err != nil {
			return Status{}, err
		}
		if duplicates != 0 {
			return Status{}, errors.New("This site already has a WordPress cron job. Review it before enabling another")
		}
		if _, err = s.read(filepath.Join(v.root, "wp-cron.php")); err != nil {
			return Status{}, errors.New("WordPress wp-cron.php is missing or unsafe; its configuration was preserved")
		}
		if _, err = s.probe(ctx, 15*time.Second, "fluxo", v.path, "/usr/bin/php"+v.php, "/usr/local/bin/wp", "core", "is-installed", "--skip-plugins", "--skip-themes", "--path="+v.root); err != nil {
			return Status{}, errors.New("Finish WordPress installation and verify its database connection before enabling WordPress Cron")
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return Status{}, err
		}
		defer tx.Rollback()
		res, err := tx.Exec("INSERT INTO crons(site_id,name,expression,command,user) VALUES(?,'WordPress Cron','* * * * *',?,'fluxo')", id, command(v))
		if err != nil {
			return Status{}, err
		}
		cronID, err := res.LastInsertId()
		if err != nil {
			return Status{}, err
		}
		state = saved{int(cronID), "enabling", fingerprint(raw)}
		if _, err = tx.Exec("INSERT INTO wordpress_cron_settings(site_id,cron_id,phase,source_sha) VALUES(?,?,'enabling',?)", id, state.id, state.source); err != nil {
			return Status{}, err
		}
		if err = tx.Commit(); err != nil {
			return Status{}, err
		}
	}
	if state.phase == "enabling" && fingerprint(raw) != state.source && !bytes.Contains(raw, []byte(Block)) {
		return Status{}, errors.New("WordPress configuration changed during setup. Turn WordPress Cron off and review it before retrying")
	}
	var discard func() error
	if !bytes.Contains(raw, []byte(Block)) {
		discard = func() error {
			current, err := s.read(configPath)
			if err != nil || !bytes.Equal(current, raw) {
				return errors.New("WordPress configuration changed during setup; review the retained setup before retrying")
			}
			return s.forget(ctx, id, state.id)
		}
	}
	if err = s.publish(ctx, state.id, id, v.path, "* * * * *", command(v), discard); err != nil {
		if errors.Is(err, cron.ErrNoGuardedRegistration) {
			return Status{}, fmt.Errorf("WordPress Cron is unavailable. Visitor-triggered scheduling was preserved: %w", err)
		}
		return Status{}, fmt.Errorf("Guarded WordPress Cron registration needs attention. Retry setup or turn it off: %w", err)
	}
	if !bytes.Equal(raw, next) {
		if err = s.write(configPath, raw, next, lint); err != nil {
			return Status{}, err
		}
	}
	if _, err = s.db.Exec("UPDATE wordpress_cron_settings SET phase='enabled' WHERE site_id=?", id); err != nil {
		return Status{}, err
	}
	return Status{Enabled: true, Phase: "enabled", CronID: state.id}, nil
}

func (s *Service) forget(ctx context.Context, siteID, cronID int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM crons WHERE id=? AND site_id=?", cronID, siteID); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM wordpress_cron_settings WHERE site_id=? AND cron_id=?", siteID, cronID); err != nil {
		return err
	}
	return tx.Commit()
}
