package database

import "fluxo/internal/services/scheduler"

// PHPMaintenancePending is the shared admission gate for PHP maintenance and
// application mutations. Terminal operation status alone does not release the
// gate: interrupted generations and unreleased service barriers remain durable
// evidence that maintenance needs recovery. Callers must also block on errors.
func PHPMaintenancePending() (bool, error) {
	if pending, err := scheduler.Pending(); err != nil || pending {
		return pending, err
	}
	var pending bool
	err := DB.QueryRow(`SELECT
		EXISTS (SELECT 1 FROM php_update_operations
			WHERE kind IN ('apply','verify','repair','rollback','repository_review','repository_apply') AND status IN ('queued','running'))
		OR EXISTS (SELECT 1 FROM php_repository_migrations WHERE state='pending')
		OR EXISTS (SELECT 1 FROM php_update_generations WHERE state NOT IN ('succeeded','rolled_back'))
		OR EXISTS (SELECT 1 FROM php_update_barriers)`).Scan(&pending)
	return pending, err
}
