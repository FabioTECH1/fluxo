package database

import (
	"database/sql"
	"fmt"
)

const (
	CertificateBindingOriginManual    = "manual"
	CertificateBindingOriginPreserved = "preserved"
)

// CertificateDomainBindingMutation describes one binding change to apply with
// a site default certificate change. CertificateID 0 removes the binding.
type CertificateDomainBindingMutation struct {
	Domain        string
	CertificateID int
	Origin        string
}

// CreateCertificate inserts a new certificate row and returns its ID.
func CreateCertificate(siteID int, domain, provider, certPath, keyPath, expiresAt string) (int64, error) {
	res, err := DB.Exec(
		"INSERT INTO certificates (site_id, domain, provider, cert_path, key_path, active, expires_at) VALUES (?, ?, ?, ?, ?, 0, ?)",
		siteID, domain, provider, certPath, keyPath, expiresAt,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create certificate: %w", err)
	}
	return res.LastInsertId()
}

// CreateClonedCertificate inserts an independent certificate copy while retaining its source for auditability.
func CreateClonedCertificate(siteID int, domain, certPath, keyPath, expiresAt string, sourceCertificateID int) (int64, error) {
	res, err := DB.Exec(
		"INSERT INTO certificates (site_id, domain, provider, cert_path, key_path, active, expires_at, source_certificate_id) VALUES (?, ?, 'cloned', ?, ?, 0, ?, ?)",
		siteID, domain, certPath, keyPath, expiresAt, sourceCertificateID,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create cloned certificate: %w", err)
	}
	return res.LastInsertId()
}

// GetCertificatesBySite returns all certificates for a given site.
func GetCertificatesBySite(siteID int) ([]Certificate, error) {
	rows, err := DB.Query("SELECT id, site_id, domain, provider, COALESCE(cert_path, ''), COALESCE(key_path, ''), active, COALESCE(expires_at, ''), COALESCE(source_certificate_id, 0), created_at FROM certificates WHERE site_id = ? ORDER BY created_at DESC", siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var certs []Certificate
	for rows.Next() {
		var c Certificate
		var active int
		if err := rows.Scan(&c.ID, &c.SiteID, &c.Domain, &c.Provider, &c.CertPath, &c.KeyPath, &active, &c.ExpiresAt, &c.SourceCertificateID, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to read certificate %d: %w", c.ID, err)
		}
		c.Active = active == 1
		certs = append(certs, c)
	}
	return certs, rows.Err()
}

// UpdateCertificateExpiry records the expiry currently present in the certificate file.
func UpdateCertificateExpiry(certID int, expiresAt string) error {
	_, err := DB.Exec("UPDATE certificates SET expires_at = ? WHERE id = ?", expiresAt, certID)
	return err
}

// GetActiveCertificate returns the currently active certificate for a site, or nil if none.
func GetActiveCertificate(siteID int) (*Certificate, error) {
	var c Certificate
	var active int
	err := DB.QueryRow("SELECT id, site_id, domain, provider, cert_path, key_path, active, COALESCE(expires_at, ''), COALESCE(source_certificate_id, 0), created_at FROM certificates WHERE site_id = ? AND active = 1 LIMIT 1", siteID).
		Scan(&c.ID, &c.SiteID, &c.Domain, &c.Provider, &c.CertPath, &c.KeyPath, &active, &c.ExpiresAt, &c.SourceCertificateID, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.Active = true
	return &c, nil
}

// GetCertificate returns a certificate owned by a site.
func GetCertificate(certID, siteID int) (*Certificate, error) {
	var c Certificate
	var active int
	err := DB.QueryRow("SELECT id, site_id, domain, provider, cert_path, key_path, active, COALESCE(expires_at, ''), COALESCE(source_certificate_id, 0), created_at FROM certificates WHERE id = ? AND site_id = ?", certID, siteID).
		Scan(&c.ID, &c.SiteID, &c.Domain, &c.Provider, &c.CertPath, &c.KeyPath, &active, &c.ExpiresAt, &c.SourceCertificateID, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("certificate not found")
	}
	if err != nil {
		return nil, err
	}
	c.Active = active == 1
	return &c, nil
}

// GetCertificateDomainBindings returns hostname-specific certificate overrides for a site.
func GetCertificateDomainBindings(siteID int) ([]CertificateDomainBinding, error) {
	rows, err := DB.Query(`
		SELECT b.site_id, b.domain, b.certificate_id, c.provider, COALESCE(b.origin, 'manual'),
		       COALESCE(c.cert_path, ''), COALESCE(c.key_path, '')
		FROM certificate_domain_bindings b
		JOIN certificates c ON c.id = b.certificate_id AND c.site_id = b.site_id
		WHERE b.site_id = ?
		ORDER BY b.domain COLLATE NOCASE`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bindings := make([]CertificateDomainBinding, 0)
	for rows.Next() {
		var binding CertificateDomainBinding
		if err := rows.Scan(
			&binding.SiteID, &binding.Domain, &binding.CertificateID, &binding.Provider, &binding.Origin,
			&binding.CertPath, &binding.KeyPath,
		); err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	return bindings, rows.Err()
}

// GetCertificateDomainBinding returns a hostname-specific certificate override, if present.
func GetCertificateDomainBinding(siteID int, domain string) (*CertificateDomainBinding, error) {
	var binding CertificateDomainBinding
	err := DB.QueryRow(`
		SELECT b.site_id, b.domain, b.certificate_id, c.provider, COALESCE(b.origin, 'manual'),
		       COALESCE(c.cert_path, ''), COALESCE(c.key_path, '')
		FROM certificate_domain_bindings b
		JOIN certificates c ON c.id = b.certificate_id AND c.site_id = b.site_id
		WHERE b.site_id = ? AND b.domain = ? COLLATE NOCASE`, siteID, domain).Scan(
		&binding.SiteID, &binding.Domain, &binding.CertificateID, &binding.Provider, &binding.Origin,
		&binding.CertPath, &binding.KeyPath,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

// SetCertificateDomainBinding activates a certificate for one hostname without changing the site default.
func SetCertificateDomainBinding(siteID int, domain string, certID int) error {
	return SetCertificateDomainBindingWithOrigin(siteID, domain, certID, CertificateBindingOriginManual)
}

// SetCertificateDomainBindingWithOrigin activates a certificate for one hostname and records why it was assigned.
func SetCertificateDomainBindingWithOrigin(siteID int, domain string, certID int, origin string) error {
	if origin != CertificateBindingOriginManual && origin != CertificateBindingOriginPreserved {
		return fmt.Errorf("invalid certificate binding origin")
	}
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		INSERT INTO certificate_domain_bindings (site_id, domain, certificate_id, origin)
		SELECT ?, ?, ?, ?
		WHERE EXISTS (SELECT 1 FROM certificates WHERE id = ? AND site_id = ?)
		  AND EXISTS (
			SELECT 1 FROM domain_aliases
			WHERE site_id = ? AND domain = ? COLLATE NOCASE
		  )
		ON CONFLICT(site_id, domain) DO UPDATE SET
			certificate_id = excluded.certificate_id,
			origin = excluded.origin,
			updated_at = CURRENT_TIMESTAMP`,
		siteID, domain, certID, origin,
		certID, siteID,
		siteID, domain,
	)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		return fmt.Errorf("certificate or domain not found")
	}
	if err := setDomainSSLDisabledTx(tx, siteID, domain, false); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteCertificateDomainBinding removes a hostname-specific certificate override.
func DeleteCertificateDomainBinding(siteID int, domain string) error {
	_, err := DB.Exec(
		"DELETE FROM certificate_domain_bindings WHERE site_id = ? AND domain = ? COLLATE NOCASE",
		siteID, domain,
	)
	return err
}

// IsDomainSSLDisabled reports whether an alias must remain HTTP-only even when
// the site's default certificate covers it.
func IsDomainSSLDisabled(siteID int, domain string) (bool, error) {
	var disabled bool
	err := DB.QueryRow(
		"SELECT ssl_disabled FROM domain_aliases WHERE site_id = ? AND domain = ? COLLATE NOCASE",
		siteID, domain,
	).Scan(&disabled)
	return disabled, err
}

// SetDomainSSLDisabled persists an explicit alias SSL preference. Disabling
// also removes any hostname-specific certificate binding atomically.
func SetDomainSSLDisabled(siteID int, domain string, disabled bool) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if disabled {
		if _, err := tx.Exec(
			"DELETE FROM certificate_domain_bindings WHERE site_id = ? AND domain = ? COLLATE NOCASE",
			siteID, domain,
		); err != nil {
			return err
		}
	}
	if err := setDomainSSLDisabledTx(tx, siteID, domain, disabled); err != nil {
		return err
	}
	return tx.Commit()
}

func setDomainSSLDisabledTx(tx *sql.Tx, siteID int, domain string, disabled bool) error {
	result, err := tx.Exec(
		"UPDATE domain_aliases SET ssl_disabled = ? WHERE site_id = ? AND domain = ? COLLATE NOCASE",
		disabled, siteID, domain,
	)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		return fmt.Errorf("domain not found")
	}
	return nil
}

// GetCloneableCertificates returns custom certificates owned by other sites. Cryptographic compatibility is checked by the SSL service.
func GetCloneableCertificates(targetSiteID int) ([]CloneableCertificate, error) {
	rows, err := DB.Query(`
		SELECT c.id, c.site_id, c.domain, c.provider, c.cert_path, c.key_path, c.active,
		       COALESCE(c.expires_at, ''), COALESCE(c.source_certificate_id, 0), c.created_at, s.domain
		FROM certificates c
		JOIN sites s ON s.id = c.site_id
		WHERE c.site_id != ? AND c.provider = 'custom'
		ORDER BY c.active DESC, c.created_at DESC`, targetSiteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	certs := make([]CloneableCertificate, 0)
	for rows.Next() {
		var item CloneableCertificate
		var active int
		if err := rows.Scan(
			&item.ID, &item.SiteID, &item.Domain, &item.Provider, &item.CertPath, &item.KeyPath,
			&active, &item.ExpiresAt, &item.SourceCertificateID, &item.CreatedAt, &item.SiteDomain,
		); err != nil {
			continue
		}
		item.Active = active == 1
		certs = append(certs, item)
	}
	return certs, rows.Err()
}

// GetCloneSourceCertificate loads a custom source certificate that belongs to a different site.
func GetCloneSourceCertificate(certID, targetSiteID int) (*CloneableCertificate, error) {
	var item CloneableCertificate
	var active int
	err := DB.QueryRow(`
		SELECT c.id, c.site_id, c.domain, c.provider, c.cert_path, c.key_path, c.active,
		       COALESCE(c.expires_at, ''), COALESCE(c.source_certificate_id, 0), c.created_at, s.domain
		FROM certificates c
		JOIN sites s ON s.id = c.site_id
		WHERE c.id = ? AND c.site_id != ? AND c.provider = 'custom'`, certID, targetSiteID).Scan(
		&item.ID, &item.SiteID, &item.Domain, &item.Provider, &item.CertPath, &item.KeyPath,
		&active, &item.ExpiresAt, &item.SourceCertificateID, &item.CreatedAt, &item.SiteDomain,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("source certificate not found")
		}
		return nil, err
	}
	item.Active = active == 1
	return &item, nil
}

// ActivateCertificate sets the given certificate as active and deactivates all others for the same site.
func ActivateCertificate(certID, siteID int) error {
	return SetActiveCertificateWithBindings(siteID, certID, nil)
}

// SetActiveCertificateWithBindings updates a site's default certificate and
// hostname-specific bindings in one transaction. activeCertID 0 disables the
// site default certificate.
func SetActiveCertificateWithBindings(siteID, activeCertID int, mutations []CertificateDomainBindingMutation) error {
	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	provider := "none"
	if activeCertID > 0 {
		if err := tx.QueryRow(
			"SELECT provider FROM certificates WHERE id = ? AND site_id = ?", activeCertID, siteID,
		).Scan(&provider); err != nil {
			return fmt.Errorf("certificate not found")
		}
	}
	if _, err := tx.Exec("UPDATE certificates SET active = 0 WHERE site_id = ?", siteID); err != nil {
		return fmt.Errorf("failed to deactivate certs: %w", err)
	}
	if activeCertID > 0 {
		result, err := tx.Exec(
			"UPDATE certificates SET active = 1 WHERE id = ? AND site_id = ?", activeCertID, siteID,
		)
		if err != nil {
			return fmt.Errorf("failed to activate cert: %w", err)
		}
		if rows, err := result.RowsAffected(); err != nil || rows != 1 {
			return fmt.Errorf("certificate not found")
		}
	}
	sslActive := 0
	if activeCertID > 0 {
		sslActive = 1
	}
	if _, err := tx.Exec(
		"UPDATE sites SET ssl_provider = ?, ssl_active = ? WHERE id = ?", provider, sslActive, siteID,
	); err != nil {
		return fmt.Errorf("failed to update site SSL state: %w", err)
	}

	for _, mutation := range mutations {
		if mutation.CertificateID == 0 {
			if _, err := tx.Exec(
				"DELETE FROM certificate_domain_bindings WHERE site_id = ? AND domain = ? COLLATE NOCASE",
				siteID, mutation.Domain,
			); err != nil {
				return fmt.Errorf("failed to remove certificate binding: %w", err)
			}
			continue
		}
		if mutation.Origin != CertificateBindingOriginManual && mutation.Origin != CertificateBindingOriginPreserved {
			return fmt.Errorf("invalid certificate binding origin")
		}
		result, err := tx.Exec(`
			INSERT INTO certificate_domain_bindings (site_id, domain, certificate_id, origin)
			SELECT ?, ?, ?, ?
			WHERE EXISTS (SELECT 1 FROM certificates WHERE id = ? AND site_id = ?)
			  AND EXISTS (
				SELECT 1 FROM domain_aliases
				WHERE site_id = ? AND domain = ? COLLATE NOCASE
			  )
			ON CONFLICT(site_id, domain) DO UPDATE SET
				certificate_id = excluded.certificate_id,
				origin = excluded.origin,
				updated_at = CURRENT_TIMESTAMP`,
			siteID, mutation.Domain, mutation.CertificateID, mutation.Origin,
			mutation.CertificateID, siteID,
			siteID, mutation.Domain,
		)
		if err != nil {
			return fmt.Errorf("failed to set certificate binding: %w", err)
		}
		if rows, err := result.RowsAffected(); err != nil || rows != 1 {
			return fmt.Errorf("certificate or domain not found")
		}
		if err := setDomainSSLDisabledTx(tx, siteID, mutation.Domain, false); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// DeactivateCertificate deactivates the given certificate.
func DeactivateCertificate(certID, siteID int) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec("UPDATE certificates SET active = 0 WHERE id = ? AND site_id = ?", certID, siteID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return fmt.Errorf("certificate not found")
	}
	if err := syncSiteSSLState(tx, siteID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteCertificate removes a certificate row and returns its paths for filesystem cleanup.
func DeleteCertificate(certID, siteID int) (certPath, keyPath string, err error) {
	tx, err := DB.Begin()
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()

	var active int
	err = tx.QueryRow("SELECT cert_path, key_path, active FROM certificates WHERE id = ? AND site_id = ?", certID, siteID).Scan(&certPath, &keyPath, &active)
	if err != nil {
		return "", "", fmt.Errorf("certificate not found")
	}
	if active == 1 {
		return "", "", fmt.Errorf("deactivate the certificate before deleting it")
	}
	var bindingCount int
	if err := tx.QueryRow(
		"SELECT COUNT(*) FROM certificate_domain_bindings WHERE certificate_id = ? AND site_id = ?",
		certID, siteID,
	).Scan(&bindingCount); err != nil {
		return "", "", err
	}
	if bindingCount > 0 {
		return "", "", fmt.Errorf("deactivate the certificate for its domains before deleting it")
	}
	if _, err = tx.Exec("DELETE FROM certificates WHERE id = ? AND site_id = ?", certID, siteID); err != nil {
		return "", "", err
	}
	if err := syncSiteSSLState(tx, siteID); err != nil {
		return "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", err
	}
	return certPath, keyPath, nil
}

func syncSiteSSLState(tx *sql.Tx, siteID int) error {
	var provider string
	err := tx.QueryRow("SELECT provider FROM certificates WHERE site_id = ? AND active = 1 LIMIT 1", siteID).Scan(&provider)
	if err == sql.ErrNoRows {
		_, err = tx.Exec("UPDATE sites SET ssl_provider = 'none', ssl_active = 0 WHERE id = ?", siteID)
		return err
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE sites SET ssl_provider = ?, ssl_active = 1 WHERE id = ?", provider, siteID)
	return err
}
