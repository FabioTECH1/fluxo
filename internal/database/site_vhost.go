package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SiteVhostOverride is the administrator-managed Nginx configuration for a site.
// Its absence means Fluxo should render the configuration from the site's current settings.
type SiteVhostOverride struct {
	SiteID    int       `json:"site_id"`
	Config    string    `json:"config"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GetSiteVhostOverride returns nil when the site is still using its generated vhost.
func GetSiteVhostOverride(siteID int) (*SiteVhostOverride, error) {
	var override SiteVhostOverride
	err := DB.QueryRow(`
		SELECT site_id, config, created_at, updated_at
		FROM site_vhost_overrides
		WHERE site_id = ?`, siteID,
	).Scan(&override.SiteID, &override.Config, &override.CreatedAt, &override.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get site vhost override: %w", err)
	}
	return &override, nil
}

// SaveSiteVhostOverride creates or replaces the durable custom vhost for a site.
func SaveSiteVhostOverride(siteID int, config string) error {
	result, err := DB.Exec(`
		INSERT INTO site_vhost_overrides (site_id, config)
		VALUES (?, ?)
		ON CONFLICT(site_id) DO UPDATE SET
			config = excluded.config,
			updated_at = CURRENT_TIMESTAMP`,
		siteID, config,
	)
	if err != nil {
		return fmt.Errorf("save site vhost override: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		if err != nil {
			return fmt.Errorf("save site vhost override: %w", err)
		}
		return fmt.Errorf("save site vhost override: site was not updated")
	}
	return nil
}

// DeleteSiteVhostOverride returns the site to Fluxo-managed vhost generation.
func DeleteSiteVhostOverride(siteID int) error {
	if _, err := DB.Exec("DELETE FROM site_vhost_overrides WHERE site_id = ?", siteID); err != nil {
		return fmt.Errorf("delete site vhost override: %w", err)
	}
	return nil
}
