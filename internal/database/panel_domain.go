package database

import "fmt"

// PanelDomainConfig is the single active hostname and certificate used by the
// Fluxo administration panel. Certificate paths are never serialized directly
// by the HTTP API.
type PanelDomainConfig struct {
	Domain              string
	SSLProvider         string
	CertPath            string
	KeyPath             string
	SourceCertificateID int
	CreatedAt           string
	UpdatedAt           string
}

// GetPanelDomainConfig returns the singleton panel-domain configuration.
func GetPanelDomainConfig() (PanelDomainConfig, error) {
	var config PanelDomainConfig
	err := DB.QueryRow(`
		SELECT domain, ssl_provider, cert_path, key_path, source_certificate_id,
		       created_at, updated_at
		FROM panel_domain WHERE id = 1`).Scan(
		&config.Domain, &config.SSLProvider, &config.CertPath, &config.KeyPath,
		&config.SourceCertificateID, &config.CreatedAt, &config.UpdatedAt,
	)
	if err != nil {
		return PanelDomainConfig{}, fmt.Errorf("load panel domain: %w", err)
	}
	return config, nil
}

// SetPanelDomainConfig atomically activates a panel hostname and certificate.
// Database triggers prevent the hostname from colliding with a managed site.
func SetPanelDomainConfig(config PanelDomainConfig) error {
	result, err := DB.Exec(`
		UPDATE panel_domain
		SET domain = ?, ssl_provider = ?, cert_path = ?, key_path = ?,
		    source_certificate_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = 1`,
		config.Domain, config.SSLProvider, config.CertPath, config.KeyPath,
		config.SourceCertificateID,
	)
	if err != nil {
		return fmt.Errorf("save panel domain: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		return fmt.Errorf("save panel domain: singleton row is unavailable")
	}
	return nil
}

// ClearPanelDomainConfig removes the public panel hostname while preserving
// the singleton row for future configuration.
func ClearPanelDomainConfig() error {
	return SetPanelDomainConfig(PanelDomainConfig{})
}

// PanelDomainConflicts reports whether a hostname is already owned by a site
// or alias. Re-selecting the currently active panel hostname is allowed.
func PanelDomainConflicts(domain string) (bool, error) {
	var conflict int
	err := DB.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM sites WHERE domain = ? COLLATE NOCASE
			UNION ALL
			SELECT 1 FROM domain_aliases WHERE domain = ? COLLATE NOCASE
		)`, domain, domain).Scan(&conflict)
	if err != nil {
		return false, fmt.Errorf("check panel domain ownership: %w", err)
	}
	return conflict == 1, nil
}
