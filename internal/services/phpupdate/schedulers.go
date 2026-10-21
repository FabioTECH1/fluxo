package phpupdate

import (
	"context"
	"encoding/json"

	"fluxo/internal/database"
	"fluxo/internal/services/scheduler"
)

// ReconcileSchedulerHolds runs before normal startup mutations. The database
// generation is also checked by each dispatcher, closing the final-save window.
func ReconcileSchedulerHolds(ctx context.Context) error {
	rows, err := database.DB.Query("SELECT payload FROM php_update_generations WHERE state NOT IN ('succeeded','rolled_back')")
	if err != nil {
		return err
	}
	var generations []recoveryGeneration
	for rows.Next() {
		var raw string
		var g recoveryGeneration
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal([]byte(raw), &g); err != nil {
			rows.Close()
			return err
		}
		if g.SchedulerManaged {
			generations = append(generations, g)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, g := range generations {
		if err = scheduler.Reclose(ctx, g.Version, g.ID); err != nil {
			return err
		}
	}
	return nil
}
