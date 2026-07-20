package database

import (
	"database/sql"
	"fmt"
)

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
	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var provider string
	if err := tx.QueryRow("SELECT provider FROM certificates WHERE id = ? AND site_id = ?", certID, siteID).Scan(&provider); err != nil {
		return fmt.Errorf("certificate not found")
	}
	if _, err := tx.Exec("UPDATE certificates SET active = 0 WHERE site_id = ?", siteID); err != nil {
		return fmt.Errorf("failed to deactivate certs: %w", err)
	}
	if _, err := tx.Exec("UPDATE certificates SET active = 1 WHERE id = ? AND site_id = ?", certID, siteID); err != nil {
		return fmt.Errorf("failed to activate cert: %w", err)
	}
	if _, err := tx.Exec("UPDATE sites SET ssl_provider = ?, ssl_active = 1 WHERE id = ?", provider, siteID); err != nil {
		return fmt.Errorf("failed to update site SSL state: %w", err)
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
