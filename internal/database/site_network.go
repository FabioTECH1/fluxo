package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fluxo/internal/sitepolicy"
	"fmt"
	"strings"
)

// Base-only additive current feature storage. These are not bridge conversions;
// an old supported database may lack the entire extension, never a partial one.
const SiteNetworkSchema = `
CREATE TABLE IF NOT EXISTS site_network_settings (
 site_id INTEGER PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
 settings TEXT NOT NULL CHECK(json_valid(settings))
);
CREATE TABLE IF NOT EXISTS site_network_operations (
 id TEXT PRIMARY KEY,
 site_id INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
 previous_config TEXT NOT NULL,
 source_revision TEXT NOT NULL,
 phase TEXT NOT NULL CHECK(phase IN ('pending','committed','rolled_back')),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS site_network_pending ON site_network_operations(site_id) WHERE phase='pending';
`

func absentSiteNetworkSchema(ctx context.Context, q SchemaQueryer) (bool, error) {
	var n int
	e := q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name IN ('site_network_settings','site_network_operations','site_network_pending')").Scan(&n)
	return n == 0, e
}
func siteNetworkObject(name string) bool {
	return name == "site_network_settings" || name == "site_network_operations" || name == "site_network_pending"
}

type SiteRedirect = sitepolicy.SiteRedirect
type SiteNetworkSettings = sitepolicy.SiteNetworkSettings

func EmptySiteNetwork() SiteNetworkSettings {
	return SiteNetworkSettings{Format: 1, Redirects: []SiteRedirect{}, Wildcards: []string{}}
}
func GetSiteNetwork(siteID int) (SiteNetworkSettings, error) {
	value := EmptySiteNetwork()
	absent, e := absentSiteNetworkSchema(context.Background(), DB)
	if e != nil || absent {
		return value, e
	}
	var raw string
	e = DB.QueryRow("SELECT settings FROM site_network_settings WHERE site_id=?", siteID).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return value, nil
	}
	if e != nil {
		return value, e
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&value); e != nil {
		return value, errors.New("Saved network settings need recovery")
	}
	if value.Format != 1 || len(value.Redirects) > 100 || len(value.Wildcards) > 100 {
		return value, errors.New("Unsupported saved network settings")
	}
	return value, nil
}
func SaveSiteNetworkTx(tx *sql.Tx, siteID int, v SiteNetworkSettings) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = tx.Exec("INSERT INTO site_network_settings(site_id,settings) VALUES(?,?) ON CONFLICT(site_id) DO UPDATE SET settings=excluded.settings", siteID, string(b))
	return e
}

type SiteNetworkOperation struct{ ID, PreviousConfig, SourceRevision string }

func PendingSiteNetwork(siteID int) (*SiteNetworkOperation, error) {
	absent, e := absentSiteNetworkSchema(context.Background(), DB)
	if e != nil || absent {
		return nil, e
	}
	var op SiteNetworkOperation
	e = DB.QueryRow("SELECT id,previous_config,source_revision FROM site_network_operations WHERE site_id=? AND phase='pending'", siteID).Scan(&op.ID, &op.PreviousConfig, &op.SourceRevision)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	return &op, e
}
func BeginSiteNetworkOperation(id string, siteID int, previous, revision string) error {
	// Reciprocal to ClaimPendingDeployment: a single SQLite write arbitrates
	// config activation against both existing and newly queued deployments.
	r, e := DB.Exec(`INSERT INTO site_network_operations(id,site_id,previous_config,source_revision,phase)
		SELECT ?,?,?,?,'pending'
		WHERE EXISTS(SELECT 1 FROM sites WHERE id=? AND COALESCE(deletion_status,'')='')
		AND NOT EXISTS(SELECT 1 FROM deployments WHERE site_id=? AND status IN ('pending','running'))`, id, siteID, previous, revision, siteID, siteID)
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return errors.New("Wait for this site's deployments or deletion to finish before changing network settings")
	}
	return nil
}
func FinishSiteNetworkOperation(id, phase string) error {
	if phase != "committed" && phase != "rolled_back" {
		return errors.New("Invalid network operation outcome")
	}
	result, e := DB.Exec("UPDATE site_network_operations SET phase=? WHERE id=? AND phase='pending'", phase, id)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil || n != 1 {
		return fmt.Errorf("Network operation changed during completion")
	}
	return nil
}
