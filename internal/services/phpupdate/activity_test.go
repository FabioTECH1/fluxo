package phpupdate

import (
	"fluxo/internal/database"
	"testing"
)

func TestActivityOmitsDiscoveryButKeepsDurableOutcomes(t *testing.T) {
	testStore(t)
	for _, kind := range []string{"check", "repository_review", "apply", "repository_apply", "repair", "rollback"} {
		for _, status := range []string{"succeeded", "failed", "recovery_required"} {
			id := kind + "-" + status
			if _, e := database.DB.Exec("INSERT INTO php_update_operations(id,kind,version,status,phase,message,created_at,updated_at) VALUES(?,?,'8.4','running','working','',1,1)", id, kind); e != nil {
				t.Fatal(e)
			}
			if e := progress(id, "working", "Original bounded diagnostic"); e != nil {
				t.Fatal(e)
			}
			if e := finish(id, status, "Original bounded diagnostic"); e != nil {
				t.Fatal(e)
			}
			var got, message string
			var events, activity int
			if e := database.DB.QueryRow("SELECT status,message FROM php_update_operations WHERE id=?", id).Scan(&got, &message); e != nil {
				t.Fatal(e)
			}
			database.DB.QueryRow("SELECT count(*) FROM php_update_events WHERE operation_id=?", id).Scan(&events)
			database.DB.QueryRow("SELECT count(*) FROM activity WHERE summary LIKE ?", kind+" PHP 8.4: "+status+"%").Scan(&activity)
			expected := 1
			if kind == "check" || kind == "repository_review" {
				expected = 0
			}
			if got != status || message != "Original bounded diagnostic" || events != 1 || activity != expected {
				t.Fatal(kind, status, got, message, events, activity)
			}
		}
	}
}
