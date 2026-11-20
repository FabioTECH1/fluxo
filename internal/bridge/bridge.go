// Package bridge qualifies an existing installation without replaying historical
// provisioning. It deliberately retains current runtime recovery protocols.
package bridge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/packages"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/mod/semver"
)

const Contract = "fluxo.bridge.v1"
const receiptKey = "bridge_completion_v1"

type Check struct {
	Component string  `json:"component"`
	Passed    bool    `json:"passed"`
	Action    string  `json:"action,omitempty"`
	Blocked   bool    `json:"blocked,omitempty"`
	Details   []Check `json:"details,omitempty"`
}

type Report struct {
	Contract       string  `json:"contract"`
	Ready          bool    `json:"ready"`
	InstallationID string  `json:"installation_id,omitempty"`
	Checks         []Check `json:"checks"`
	RecoveryPolicy string  `json:"recovery_policy"`
	Recorded       bool    `json:"recorded"`
	keyIdentity    string
}

type receipt struct {
	Contract       string `json:"contract"`
	InstallationID string `json:"installation_id"`
	KeySHA256      string `json:"key_sha256"`
	BinarySHA256   string `json:"binary_sha256"`
	Version        string `json:"version"`
	CompletedAt    string `json:"completed_at"`
	RecoveryPolicy string `json:"recovery_policy"`
}

func newReport() Report {
	return Report{Contract: Contract, Checks: []Check{}, RecoveryPolicy: "retain-current-and-historical-live-recovery"}
}
func (r *Report) check(component string, err error) {
	c := Check{Component: component, Passed: err == nil}
	if err != nil {
		c.Action = err.Error()
	}
	r.Checks = append(r.Checks, c)
}
func (r *Report) finish() {
	r.Ready = len(r.Checks) > 0
	for _, c := range r.Checks {
		r.Ready = r.Ready && checkPassed(c)
	}
}

func checkPassed(c Check) bool {
	if !c.Passed || c.Blocked {
		return false
	}
	for _, detail := range c.Details {
		if !checkPassed(detail) {
			return false
		}
	}
	return true
}

// Inspect never changes managed state. SQLite reads a private stable snapshot
// (including WAL); any temporary SHM/journal is confined to that snapshot.
// Its result is advisory: Certify repeats checks under exclusive admission.
func Inspect(ctx context.Context, dataDir string) (Report, error) {
	return inspectSnapshot(ctx, dataDir, inspectAll)
}

func inspectSnapshot(ctx context.Context, dataDir string, inspectState func(context.Context, queryer, string) Report) (Report, error) {
	r := newReport()
	path, cleanup, err := snapshot(ctx, dataDir)
	if err != nil {
		return r, err
	}
	defer cleanup()
	db, err := openSQLite(ctx, path, false)
	if err != nil {
		return r, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	return inspectState(ctx, tx, dataDir), nil
}

// Certify requires an explicitly stopped panel. It never stops services or
// repairs packages automatically, and records completion only after all checks.
func Certify(ctx context.Context, dataDir, version string) (r Report, resultErr error) {
	r = newReport()
	defer func() {
		if resultErr != nil {
			r.Ready = false
		}
	}()
	if os.Geteuid() != 0 {
		return r, errors.New("bridge certification requires root")
	}
	if !semver.IsValid("v" + version) {
		return r, errors.New("certification requires a versioned bridge build")
	}
	if err := trustedDirectory(dataDir); err != nil {
		return r, err
	}
	if _, err := executiondomain.InspectCompatibility(dataDir); err != nil {
		r.check("execution_readers", err)
		return r, err
	}
	unlock, err := packages.Lifecycle(ctx, false)
	if err != nil {
		return r, err
	}
	defer unlock()
	unlockPackages, err := packages.Acquire(ctx)
	if err != nil {
		return r, err
	}
	defer unlockPackages()
	unlockNative, err := packages.NativeDPKG(ctx)
	if err != nil {
		return r, err
	}
	defer unlockNative()
	unlockCoordination, err := lockCoordination(ctx)
	if err != nil {
		return r, err
	}
	defer unlockCoordination()
	if err = panelStopped(ctx); err != nil {
		return r, err
	}
	if err = validateNginx(ctx); err != nil {
		return r, err
	}
	db, err := open(ctx, dataDir, true)
	if err != nil {
		return r, err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return r, err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return r, err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	r = inspect(ctx, conn, dataDir)
	if !r.Ready {
		return r, errors.New("bridge qualification is blocked; resolve the reported actions and retry")
	}
	// The panel and package locks remain held; verify the external identity
	// boundary again before the only persistent write in this command.
	if err = panelStopped(ctx); err != nil {
		return r, err
	}
	key, err := readKey(dataDir)
	if err != nil {
		return r, err
	}
	if digest(key) != r.keyIdentity {
		return r, errors.New("encryption key changed during qualification")
	}
	binary, err := readCertifyingExecutable()
	if err != nil {
		return r, err
	}
	entry := receipt{Contract: Contract, InstallationID: r.InstallationID, KeySHA256: digest(key), BinarySHA256: digest(binary), Version: version, CompletedAt: time.Now().UTC().Format(time.RFC3339Nano), RecoveryPolicy: r.RecoveryPolicy}
	raw, err := json.Marshal(entry)
	if err != nil {
		return r, err
	}
	if _, err = conn.ExecContext(ctx, "INSERT INTO system_metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=CURRENT_TIMESTAMP", receiptKey, string(raw)); err != nil {
		return r, err
	}
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return r, err
	}
	r.Recorded = true
	return r, nil
}

// Require is the admission contract for a future baseline consumer. A receipt
// is never sufficient on its own: every current readiness check is repeated.
// The consumer must hold its own exclusive lifecycle boundary before effects.
func Require(ctx context.Context, dataDir string) (r Report, resultErr error) {
	defer func() {
		if resultErr != nil {
			r.Ready = false
		}
	}()
	// Admission retains the strict sequence; the advisory multi-failure report
	// must never decide whether a future baseline consumer may proceed.
	r, err := inspectSnapshot(ctx, dataDir, inspect)
	if err != nil {
		return r, err
	}
	if !r.Ready {
		return r, errors.New("current bridge readiness is blocked")
	}
	path, cleanup, err := snapshot(ctx, dataDir)
	if err != nil {
		return r, err
	}
	defer cleanup()
	db, err := openSQLite(ctx, path, false)
	if err != nil {
		return r, err
	}
	defer db.Close()
	var raw string
	if err = db.QueryRowContext(ctx, "SELECT value FROM system_metadata WHERE key=?", receiptKey).Scan(&raw); err != nil {
		return r, errors.New("complete bridge certification before installing the baseline release")
	}
	var saved receipt
	if json.Unmarshal([]byte(raw), &saved) != nil || saved.Contract != Contract || saved.InstallationID != r.InstallationID || saved.RecoveryPolicy != r.RecoveryPolicy || saved.Version == "" || len(saved.BinarySHA256) != 64 {
		return r, errors.New("bridge receipt is invalid or incompatible")
	}
	key, err := readKey(dataDir)
	if err != nil {
		return r, err
	}
	if saved.KeySHA256 != digest(key) || digest(key) != r.keyIdentity {
		return r, errors.New("bridge encryption-key identity changed")
	}
	if _, err = time.Parse(time.RFC3339Nano, saved.CompletedAt); err != nil {
		return r, errors.New("invalid bridge completion time")
	}
	r.Recorded = true
	// Nginx testing can open its configured log files. It runs only after the
	// read-only receipt/identity admission has passed, never inside Inspect.
	if err = validateNginx(ctx); err != nil {
		return r, err
	}
	return r, nil
}

func open(ctx context.Context, dataDir string, write bool) (*sql.DB, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("bridge inspection requires root")
	}
	if err := trustedDirectory(dataDir); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "fluxo.db")
	if _, err := protectedInfo(path); err != nil {
		return nil, fmt.Errorf("existing database is unavailable: %w", err)
	}
	return openSQLite(ctx, path, write)
}

func openSQLite(ctx context.Context, path string, write bool) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: path}
	params := url.Values{}
	mode := "ro"
	if write {
		mode = "rw"
	}
	params.Set("mode", mode)
	params.Add("_pragma", "busy_timeout(5000)")
	u.RawQuery = params.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func inspect(ctx context.Context, q queryer, dataDir string) Report {
	r := newReport()
	_, err := executiondomain.InspectCompatibility(dataDir)
	r.check("execution_readers", err)
	if err != nil {
		r.finish()
		return r
	}
	err = checkSchema(ctx, q)
	r.check("schema", err)
	if err != nil {
		r.finish()
		return r
	}
	var id string
	err = q.QueryRowContext(ctx, "SELECT value FROM system_metadata WHERE key='installation_id'").Scan(&id)
	decoded, e := hex.DecodeString(id)
	if err != nil || e != nil || len(decoded) != 16 || id != strings.ToLower(id) {
		r.check("installation_identity", errors.New("preserve or recover the existing installation identity"))
	} else {
		r.InstallationID = id
		r.check("installation_identity", nil)
	}
	key, err := readKey(dataDir)
	if err == nil && len(key) != 32 {
		err = errors.New("installation encryption key must contain exactly 32 bytes")
	}
	r.check("encryption_key", err)
	if err == nil {
		r.keyIdentity = digest(key)
		r.check("credentials", checkSecrets(ctx, q, key))
	}
	r.check("historical_conversions", checkConversions(ctx, q))
	r.check("pending_operations", checkPending(ctx, q))
	r.check("host", checkHost(ctx, q, dataDir, key))
	r.finish()
	return r
}

func checkSecrets(ctx context.Context, q queryer, key []byte) error {
	for _, item := range []struct{ table, columns string }{
		{"users", "fluxo_sudo_password,fluxo_mysql_password,fluxo_postgres_password,fluxo_db_password,github_pat,webhook_secret"},
		{"github_accounts", "token"}, {"databases", "password"}, {"backup_destinations", "access_key,secret_key"}, {"backup_plans", "encryption_password"},
	} {
		rows, err := q.QueryContext(ctx, "SELECT "+item.columns+" FROM "+item.table)
		if err != nil {
			return err
		}
		columns := strings.Split(item.columns, ",")
		for rows.Next() {
			values := make([]sql.NullString, len(columns))
			dest := make([]any, len(values))
			for i := range dest {
				dest[i] = &values[i]
			}
			if err = rows.Scan(dest...); err != nil {
				rows.Close()
				return err
			}
			for i, v := range values {
				if _, err = config.DecryptSecretWithKey(v.String, key); err != nil {
					rows.Close()
					return fmt.Errorf("%s.%s needs verified encryption conversion or key recovery", item.table, columns[i])
				}
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	rows, err := q.QueryContext(ctx, "SELECT username,token_hash FROM users")
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var name, hash string
		if err = rows.Scan(&name, &hash); err != nil {
			return err
		}
		count++
		if name == "__bootstrap__" {
			return errors.New("Complete administrator first login in Fluxo, then retry")
		}
		if _, err = bcrypt.Cost([]byte(hash)); err != nil {
			return errors.New("sign in or use explicit account recovery to convert legacy password storage")
		}
	}
	if count != 1 {
		return errors.New("bridge requires one unambiguous configured administrator")
	}
	return rows.Err()
}

func checkPending(ctx context.Context, q queryer) error {
	return checkPendingMode(ctx, q, false)
}

func checkPendingMode(ctx context.Context, q queryer, collectAll bool) error {
	var failures []error
	if err := packages.TinkerDomainsIdle(); err != nil {
		failure := fmt.Errorf("finish Tinker cleanup before certification: %w", err)
		if !collectAll {
			return failure
		}
		failures = append(failures, failure)
	}
	checks := []struct{ table, predicate string }{
		{"php_update_operations", "status IN ('queued','running')"}, {"node_update_operations", "status IN ('queued','running')"},
		{"php_update_generations", "state NOT IN ('succeeded','rolled_back')"}, {"node_update_generations", "state NOT IN ('succeeded','rolled_back')"},
		{"php_repository_migrations", "state='pending'"}, {"php_update_barriers", "1"}, {"node_update_barriers", "1"}, {"node_update_php_barriers", "1"},
		{"deployments", "status IN ('pending','running')"}, {"commands", "status='running'"}, {"backup_runs", "status IN ('queued','running','deleting')"},
		{"sites", "COALESCE(deletion_status,'')!=''"}, {"daemons", "status LIKE 'pending%'"}, {"managed_database_users", "state!='active'"},
	}
	for _, c := range checks {
		var n int
		if err := q.QueryRowContext(ctx, "SELECT count(*) FROM "+c.table+" WHERE "+c.predicate).Scan(&n); err != nil {
			return errors.Join(append(failures, err)...)
		}
		if n > 0 {
			failure := fmt.Errorf("finish or recover pending %s work before certification", c.table)
			if !collectAll {
				return failure
			}
			failures = append(failures, failure)
		}
	}
	return errors.Join(failures...)
}

func checkConversions(ctx context.Context, q queryer) error {
	return checkConversionsMode(ctx, q, false)
}

func checkConversionsMode(ctx context.Context, q queryer, collectAll bool) error {
	return checkConversionState(ctx, q, collectAll, false)
}

// The base retains synchronization for tokens written by the current settings
// API. Ciphertext/key checks still run before this compatible field is admitted.
func checkBaseConversions(ctx context.Context, q queryer) error {
	return checkConversionState(ctx, q, false, true)
}

const settingsPATConversionQuery = "SELECT count(*) FROM users WHERE COALESCE(github_pat,'')!=''"

func checkConversionState(ctx context.Context, q queryer, collectAll, allowSettingsPAT bool) error {
	var failures []error
	checks := []struct{ query, action string }{
		{"SELECT count(*) FROM sqlite_master WHERE type='table' AND (name LIKE '%_migrate' OR name='databases_next')", "resolve saved conversion tables without discarding their rows"},
		{settingsPATConversionQuery, "resolve the legacy settings PAT into GitHub accounts before certification"},
		{"SELECT count(*) FROM backup_destinations WHERE server_id!=(SELECT value FROM system_metadata WHERE key='installation_id')", "align backup destinations without rewriting historical object keys"},
		{"SELECT count(*) FROM sites s WHERE COALESCE(ssl_provider,'none') NOT IN ('','none') AND NOT EXISTS(SELECT 1 FROM certificates c WHERE c.site_id=s.id AND c.provider=s.ssl_provider AND c.domain=s.domain)", "complete historical certificate import"},
		{"SELECT count(*) FROM sites WHERE deploy_script LIKE '%sudo systemctl reload php$FLUXO_PHP_VERSION-fpm%'", "review the historical PHP reload fragment before certifying custom scripts"},
	}
	for _, c := range checks {
		if allowSettingsPAT && c.query == settingsPATConversionQuery {
			continue
		}
		var n int
		if err := q.QueryRowContext(ctx, c.query).Scan(&n); err != nil {
			return errors.Join(append(failures, err)...)
		}
		if n > 0 {
			if !collectAll {
				return errors.New(c.action)
			}
			failures = append(failures, errors.New(c.action))
		}
	}
	return errors.Join(failures...)
}

func digest(b []byte) string { v := sha256.Sum256(b); return hex.EncodeToString(v[:]) }
