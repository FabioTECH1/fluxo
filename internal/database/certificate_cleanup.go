package database

import "fmt"

func PendingCertificateCleanups(limit int) ([]CertificateCleanup, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := DB.Query(`
		SELECT id, certificate_id, former_site_id, domain, provider,
		       COALESCE(cert_path, ''), COALESCE(key_path, ''),
		       COALESCE(source_certificate_id, 0), COALESCE(cleanup_status, 'pending'),
		       COALESCE(cleanup_error, ''), COALESCE(cleanup_attempts, 0)
		FROM orphaned_certificates
		WHERE COALESCE(cleanup_origin, 'legacy') = 'site_deletion'
		AND COALESCE(cleanup_status, 'pending') IN ('pending', 'failed')
		ORDER BY COALESCE(cleanup_attempts, 0), archived_at, id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]CertificateCleanup, 0)
	for rows.Next() {
		var item CertificateCleanup
		if err := rows.Scan(
			&item.ID, &item.CertificateID, &item.FormerSiteID, &item.Domain,
			&item.Provider, &item.CertPath, &item.KeyPath, &item.SourceCertificateID,
			&item.CleanupStatus, &item.CleanupError, &item.CleanupAttempts,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func CertificateStorageHasLiveReferences(certPath, keyPath string) (bool, error) {
	var references int
	if err := DB.QueryRow(`
		SELECT COUNT(*) FROM certificates c
		JOIN sites s ON s.id = c.site_id
		WHERE ((? != '' AND (c.cert_path = ? OR c.key_path = ?)) OR
		       (? != '' AND (c.cert_path = ? OR c.key_path = ?)))`,
		certPath, certPath, certPath,
		keyPath, keyPath, keyPath,
	).Scan(&references); err != nil {
		return false, err
	}
	return references > 0, nil
}

func CompleteCertificateCleanup(id int, status, message string) error {
	if status != "cleaned" && status != "retained" {
		return fmt.Errorf("invalid certificate cleanup status %q", status)
	}
	_, err := DB.Exec(`UPDATE orphaned_certificates
		SET cleanup_status = ?, cleanup_error = ?, cleanup_attempts = COALESCE(cleanup_attempts, 0) + 1,
		    cleaned_at = CURRENT_TIMESTAMP
		WHERE id = ?`, status, message, id)
	return err
}

func FailCertificateCleanup(id int, message string) error {
	_, err := DB.Exec(`UPDATE orphaned_certificates
		SET cleanup_status = 'failed', cleanup_error = ?,
		    cleanup_attempts = COALESCE(cleanup_attempts, 0) + 1, cleaned_at = NULL
		WHERE id = ?`, message, id)
	return err
}
