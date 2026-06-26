package database

import (
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

// GetCertificatesBySite returns all certificates for a given site.
func GetCertificatesBySite(siteID int) ([]Certificate, error) {
	rows, err := DB.Query("SELECT id, site_id, domain, provider, cert_path, key_path, active, expires_at, created_at FROM certificates WHERE site_id = ? ORDER BY created_at DESC", siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var certs []Certificate
	for rows.Next() {
		var c Certificate
		var active int
		if err := rows.Scan(&c.ID, &c.SiteID, &c.Domain, &c.Provider, &c.CertPath, &c.KeyPath, &active, &c.ExpiresAt, &c.CreatedAt); err != nil {
			continue
		}
		c.Active = active == 1
		certs = append(certs, c)
	}
	return certs, nil
}

// GetActiveCertificate returns the currently active certificate for a site, or nil if none.
func GetActiveCertificate(siteID int) (*Certificate, error) {
	var c Certificate
	var active int
	err := DB.QueryRow("SELECT id, site_id, domain, provider, cert_path, key_path, active, expires_at, created_at FROM certificates WHERE site_id = ? AND active = 1 LIMIT 1", siteID).
		Scan(&c.ID, &c.SiteID, &c.Domain, &c.Provider, &c.CertPath, &c.KeyPath, &active, &c.ExpiresAt, &c.CreatedAt)
	if err != nil {
		return nil, nil // no active cert — not an error
	}
	c.Active = true
	return &c, nil
}

// ActivateCertificate sets the given certificate as active and deactivates all others for the same site.
func ActivateCertificate(certID, siteID int) error {
	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("UPDATE certificates SET active = 0 WHERE site_id = ?", siteID); err != nil {
		return fmt.Errorf("failed to deactivate certs: %w", err)
	}
	if _, err := tx.Exec("UPDATE certificates SET active = 1 WHERE id = ? AND site_id = ?", certID, siteID); err != nil {
		return fmt.Errorf("failed to activate cert: %w", err)
	}

	return tx.Commit()
}

// DeactivateCertificate deactivates the given certificate.
func DeactivateCertificate(certID, siteID int) error {
	_, err := DB.Exec("UPDATE certificates SET active = 0 WHERE id = ? AND site_id = ?", certID, siteID)
	return err
}

// DeleteCertificate removes a certificate row and returns its paths for filesystem cleanup.
func DeleteCertificate(certID, siteID int) (certPath, keyPath string, err error) {
	err = DB.QueryRow("SELECT cert_path, key_path FROM certificates WHERE id = ? AND site_id = ?", certID, siteID).Scan(&certPath, &keyPath)
	if err != nil {
		return "", "", fmt.Errorf("certificate not found")
	}
	_, err = DB.Exec("DELETE FROM certificates WHERE id = ? AND site_id = ?", certID, siteID)
	return certPath, keyPath, err
}
