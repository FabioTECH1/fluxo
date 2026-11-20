package database

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
)

func deploymentTimingColumn(name string) bool {
	return name == "started_at" || name == "finished_at"
}

// ensureDeploymentTimingColumns extends only the supported deployment table.
// No row is backfilled: creation/update times are not execution boundaries.
func ensureDeploymentTimingColumns(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	columns, err := pragmaRows(context.Background(), tx, "table_xinfo", "deployments")
	if err != nil {
		return err
	}
	for _, name := range []string{"started_at", "finished_at"} {
		found := false
		for _, row := range columns {
			if row[1] != name {
				continue
			}
			row[0] = "" // Physical column order may differ after ADD COLUMN.
			if !reflect.DeepEqual(row, []string{"", name, "DATETIME", "0", "", "0", "0"}) {
				return fmt.Errorf("deployment timing column %s has unsupported metadata", name)
			}
			found = true
		}
		if !found {
			if _, err := tx.Exec("ALTER TABLE deployments ADD COLUMN " + quote(name) + " DATETIME"); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
