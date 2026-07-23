package database

import (
	"database/sql"
	"fmt"
)

// PrimaryDomainSnapshot contains the exact database state needed to undo a
// primary-domain promotion if the generated server configuration cannot load.
type PrimaryDomainSnapshot struct {
	SiteID              int
	AliasID             int
	OldPrimary          string
	NewPrimary          string
	AliasSSLDisabled    bool
	AliasCreatedAt      string
	ActiveCertificate   int
	SiteSSLProvider     string
	SiteSSLActive       bool
	CertificateBindings []CertificateDomainBinding
}

// PromoteDomainAlias swaps an alias with the site's primary domain and applies
// the certificate state planned by the server in the same transaction.
func PromoteDomainAlias(siteID, aliasID, activeCertificateID int, mutations []CertificateDomainBindingMutation) (*PrimaryDomainSnapshot, error) {
	tx, err := DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	snapshot := &PrimaryDomainSnapshot{SiteID: siteID, AliasID: aliasID}
	if err := tx.QueryRow(`
		SELECT domain, COALESCE(ssl_provider, 'none'), COALESCE(ssl_active, 0)
		FROM sites WHERE id = ?`, siteID,
	).Scan(&snapshot.OldPrimary, &snapshot.SiteSSLProvider, &snapshot.SiteSSLActive); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("site not found")
		}
		return nil, err
	}
	if err := tx.QueryRow(`
		SELECT domain, ssl_disabled, created_at
		FROM domain_aliases WHERE id = ? AND site_id = ?`, aliasID, siteID,
	).Scan(&snapshot.NewPrimary, &snapshot.AliasSSLDisabled, &snapshot.AliasCreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("domain alias not found")
		}
		return nil, err
	}
	if err := tx.QueryRow(
		"SELECT id FROM certificates WHERE site_id = ? AND active = 1 LIMIT 1", siteID,
	).Scan(&snapshot.ActiveCertificate); err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	bindings, err := certificateBindingsTx(tx, siteID)
	if err != nil {
		return nil, err
	}
	snapshot.CertificateBindings = bindings

	if activeCertificateID > 0 {
		var owner int
		if err := tx.QueryRow(
			"SELECT site_id FROM certificates WHERE id = ? AND site_id = ?", activeCertificateID, siteID,
		).Scan(&owner); err != nil {
			return nil, fmt.Errorf("certificate not found")
		}
	}

	if result, err := tx.Exec(
		"DELETE FROM domain_aliases WHERE id = ? AND site_id = ?", aliasID, siteID,
	); err != nil {
		return nil, err
	} else if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return nil, fmt.Errorf("domain alias not found")
	}
	if _, err := tx.Exec(
		"UPDATE sites SET domain = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", snapshot.NewPrimary, siteID,
	); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`
		INSERT INTO domain_aliases (id, site_id, domain, ssl_disabled, created_at)
		VALUES (?, ?, ?, 0, ?)`, aliasID, siteID, snapshot.OldPrimary, snapshot.AliasCreatedAt,
	); err != nil {
		return nil, err
	}

	if err := setActiveCertificateTx(tx, siteID, activeCertificateID); err != nil {
		return nil, err
	}
	if err := applyCertificateBindingMutationsTx(tx, siteID, mutations); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// RestorePrimaryDomain restores a snapshot returned by PromoteDomainAlias.
func RestorePrimaryDomain(snapshot *PrimaryDomainSnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("primary domain snapshot is required")
	}
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentPrimary, currentAlias string
	if err := tx.QueryRow("SELECT domain FROM sites WHERE id = ?", snapshot.SiteID).Scan(&currentPrimary); err != nil {
		return err
	}
	if err := tx.QueryRow(
		"SELECT domain FROM domain_aliases WHERE id = ? AND site_id = ?", snapshot.AliasID, snapshot.SiteID,
	).Scan(&currentAlias); err != nil {
		return err
	}
	if currentPrimary != snapshot.NewPrimary || currentAlias != snapshot.OldPrimary {
		return fmt.Errorf("primary domain state changed after promotion")
	}

	if _, err := tx.Exec(
		"DELETE FROM domain_aliases WHERE id = ? AND site_id = ?", snapshot.AliasID, snapshot.SiteID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		"UPDATE sites SET domain = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", snapshot.OldPrimary, snapshot.SiteID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO domain_aliases (id, site_id, domain, ssl_disabled, created_at)
		VALUES (?, ?, ?, ?, ?)`, snapshot.AliasID, snapshot.SiteID, snapshot.NewPrimary,
		snapshot.AliasSSLDisabled, snapshot.AliasCreatedAt,
	); err != nil {
		return err
	}

	if _, err := tx.Exec("DELETE FROM certificate_domain_bindings WHERE site_id = ?", snapshot.SiteID); err != nil {
		return err
	}
	for _, binding := range snapshot.CertificateBindings {
		if _, err := tx.Exec(`
			INSERT INTO certificate_domain_bindings (site_id, domain, certificate_id, origin)
			VALUES (?, ?, ?, ?)`, snapshot.SiteID, binding.Domain, binding.CertificateID, binding.Origin,
		); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("UPDATE certificates SET active = 0 WHERE site_id = ?", snapshot.SiteID); err != nil {
		return err
	}
	if snapshot.ActiveCertificate > 0 {
		result, err := tx.Exec(
			"UPDATE certificates SET active = 1 WHERE id = ? AND site_id = ?", snapshot.ActiveCertificate, snapshot.SiteID,
		)
		if err != nil {
			return err
		}
		if rows, err := result.RowsAffected(); err != nil || rows != 1 {
			return fmt.Errorf("snapshot certificate not found")
		}
	}
	if _, err := tx.Exec(`
		UPDATE sites SET ssl_provider = ?, ssl_active = ? WHERE id = ?`,
		snapshot.SiteSSLProvider, snapshot.SiteSSLActive, snapshot.SiteID,
	); err != nil {
		return err
	}

	return tx.Commit()
}

func certificateBindingsTx(tx *sql.Tx, siteID int) ([]CertificateDomainBinding, error) {
	rows, err := tx.Query(`
		SELECT b.site_id, b.domain, b.certificate_id, c.provider, COALESCE(b.origin, 'manual'),
		       COALESCE(c.cert_path, ''), COALESCE(c.key_path, '')
		FROM certificate_domain_bindings b
		JOIN certificates c ON c.id = b.certificate_id AND c.site_id = b.site_id
		WHERE b.site_id = ? ORDER BY b.domain COLLATE NOCASE`, siteID)
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

func setActiveCertificateTx(tx *sql.Tx, siteID, certificateID int) error {
	provider := "none"
	if certificateID > 0 {
		if err := tx.QueryRow(
			"SELECT provider FROM certificates WHERE id = ? AND site_id = ?", certificateID, siteID,
		).Scan(&provider); err != nil {
			return fmt.Errorf("certificate not found")
		}
	}
	if _, err := tx.Exec("UPDATE certificates SET active = 0 WHERE site_id = ?", siteID); err != nil {
		return err
	}
	if certificateID > 0 {
		if _, err := tx.Exec(
			"UPDATE certificates SET active = 1 WHERE id = ? AND site_id = ?", certificateID, siteID,
		); err != nil {
			return err
		}
	}
	_, err := tx.Exec(
		"UPDATE sites SET ssl_provider = ?, ssl_active = ? WHERE id = ?", provider, certificateID > 0, siteID,
	)
	return err
}

func applyCertificateBindingMutationsTx(tx *sql.Tx, siteID int, mutations []CertificateDomainBindingMutation) error {
	for _, mutation := range mutations {
		if mutation.CertificateID == 0 {
			if _, err := tx.Exec(
				"DELETE FROM certificate_domain_bindings WHERE site_id = ? AND domain = ? COLLATE NOCASE",
				siteID, mutation.Domain,
			); err != nil {
				return err
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
			  AND EXISTS (SELECT 1 FROM domain_aliases WHERE site_id = ? AND domain = ? COLLATE NOCASE)
			ON CONFLICT(site_id, domain) DO UPDATE SET
				certificate_id = excluded.certificate_id,
				origin = excluded.origin,
				updated_at = CURRENT_TIMESTAMP`,
			siteID, mutation.Domain, mutation.CertificateID, mutation.Origin,
			mutation.CertificateID, siteID, siteID, mutation.Domain,
		)
		if err != nil {
			return err
		}
		if rows, err := result.RowsAffected(); err != nil || rows != 1 {
			return fmt.Errorf("certificate or domain not found")
		}
	}
	return nil
}
