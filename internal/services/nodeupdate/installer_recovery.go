package nodeupdate

// Installer recovery admission is read-only. The installer can make a verified
// compatible worker available without stopping the panel, removing holds, or
// approving a runtime transition on the user's behalf.
import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

type InstallerRecovery struct {
	Generation  string `json:"generation"`
	Head        string `json:"head"`
	Fingerprint string `json:"fingerprint"`
}
type InstallerRuntimeStatus struct {
	Pending          bool               `json:"pending"`
	ActiveOperations int                `json:"active_operations"`
	PHPBlocked       bool               `json:"php_blocked"`
	Recovery         *InstallerRecovery `json:"recovery,omitempty"`
}

// InstallerRuntimeState never initializes/migrates the database. Unknown or
// unsupported retained history is refused rather than silently adopted.
func InstallerRuntimeState(ctx context.Context, dataDir string) (InstallerRuntimeStatus, error) {
	var result InstallerRuntimeStatus
	if !filepath.IsAbs(dataDir) || filepath.Clean(dataDir) != dataDir {
		return result, errors.New("Installer runtime checks require an absolute data directory")
	}
	path := filepath.Join(dataDir, "fluxo.db")
	if err := trustedPath(path); err != nil {
		return result, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() {
		return result, errors.New("Installer database is not a regular file")
	}
	uri := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", uri.String()+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return result, err
	}
	defer db.Close()
	return readInstallerRuntimeState(ctx, db)
}

func readInstallerRuntimeState(ctx context.Context, db *sql.DB) (InstallerRuntimeStatus, error) {
	var result InstallerRuntimeStatus
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	count := func(table, predicate string) (int, error) {
		var exists, n int
		if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&exists); e != nil {
			return 0, e
		}
		if exists == 0 {
			return 0, nil
		}
		e := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE "+predicate).Scan(&n)
		return n, e
	}
	for table, predicate := range map[string]string{"node_update_operations": "status IN ('queued','running')", "php_update_operations": "status IN ('queued','running')"} {
		n, e := count(table, predicate)
		if e != nil {
			return result, e
		}
		result.ActiveOperations += n
	}
	for table, predicate := range map[string]string{"php_update_operations": "status IN ('queued','running')", "php_update_generations": "state NOT IN ('succeeded','rolled_back')", "php_update_barriers": "1", "php_repository_migrations": "state='pending'"} {
		n, e := count(table, predicate)
		if e != nil {
			return result, e
		}
		result.PHPBlocked = result.PHPBlocked || n > 0
	}
	n, err := count("node_update_generations", "state NOT IN ('succeeded','rolled_back')")
	if err != nil {
		return result, err
	}
	result.Pending = result.PHPBlocked || result.ActiveOperations > 0 || n > 0
	for _, table := range []string{"node_update_barriers", "node_update_php_barriers"} {
		holds, e := count(table, "1")
		if e != nil {
			return result, e
		}
		result.Pending = result.Pending || holds > 0
	}
	if result.ActiveOperations > 0 || result.PHPBlocked {
		return result, nil
	}
	if n == 0 {
		if result.Pending {
			return result, errors.New("Runtime holds have no pending generation; preserve them and inspect recovery before upgrading")
		}
		return result, nil
	}
	if n != 1 {
		return result, errors.New("Multiple Node recoveries require review before upgrading Fluxo")
	}
	var id, head, state, raw string
	if err = tx.QueryRowContext(ctx, "SELECT id,head,state,payload FROM node_update_generations WHERE state NOT IN ('succeeded','rolled_back')").Scan(&id, &head, &state, &raw); err != nil {
		return result, err
	}
	var g Generation
	if err = json.Unmarshal([]byte(raw), &g); err != nil {
		return result, err
	}
	for _, s := range []string{id, head} {
		parsed, e := uuid.Parse(s)
		if e != nil || parsed.String() != s || parsed == uuid.Nil {
			return result, errors.New("Invalid retained Node recovery identity")
		}
	}
	if g.ID != id || g.Head != head || g.State != state || state != "recovery_required" || g.Plan.ConsumerPolicy != consumerPolicy || (g.PHPState != nil && g.PHPState.Schema != 1) {
		return result, errors.New("The selected Fluxo release cannot safely interpret this Node recovery; choose a compatible release")
	}
	var latest string
	if err = tx.QueryRowContext(ctx, "SELECT id FROM node_update_generations ORDER BY rowid DESC LIMIT 1").Scan(&latest); err != nil {
		return result, err
	}
	if latest != id {
		return result, errors.New("Pending Node recovery is not the current generation")
	}
	var opStatus string
	if err = tx.QueryRowContext(ctx, "SELECT status FROM node_update_operations WHERE id=?", head).Scan(&opStatus); err != nil {
		return result, err
	}
	if opStatus != "failed" && opStatus != "interrupted" && opStatus != "recovery_required" {
		return result, errors.New("The previous Node attempt must finish before upgrading its worker")
	}
	// Fingerprint all owned hold rows as well as the immutable generation. A fresh
	// candidate check immediately before replacement rejects concurrent drift.
	material := []string{raw, opStatus}
	for _, table := range []string{"node_update_barriers", "node_update_php_barriers"} {
		foreign, e := count(table, "generation_id != '"+id+"'")
		if e != nil {
			return result, e
		}
		if foreign > 0 {
			return result, errors.New("Another generation owns runtime holds")
		}
		var exists int
		if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&exists); e != nil {
			return result, e
		}
		if exists == 0 {
			continue
		}
		columns := "unit,generation_id"
		if table == "node_update_php_barriers" {
			columns += ",payload"
		}
		rows, e := tx.QueryContext(ctx, "SELECT "+columns+" FROM "+table+" ORDER BY unit")
		if e != nil {
			return result, e
		}
		for rows.Next() {
			var unit, owner, payload string
			if table == "node_update_php_barriers" {
				e = rows.Scan(&unit, &owner, &payload)
			} else {
				e = rows.Scan(&unit, &owner)
			}
			if e != nil {
				rows.Close()
				return result, e
			}
			material = append(material, table, unit, owner, payload)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return result, e
		}
	}
	data, _ := json.Marshal(material)
	sum := sha256.Sum256(data)
	result.Recovery = &InstallerRecovery{Generation: id, Head: head, Fingerprint: hex.EncodeToString(sum[:])}
	return result, nil
}
