package deploy

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"fluxo/internal/database"
)

func deploymentTimingDB(t *testing.T) {
	t.Helper()
	previous := database.DB
	if err := database.InitBaseDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.DB.Close(); database.DB = previous })
	if _, err := database.DB.Exec(`INSERT INTO sites(id,domain,path,repository,branch,php_version,app_type,web_root)
		VALUES(1,'timing.test','/home/fluxo/timing.test','','main','8.4','html','/public');
		INSERT INTO deployments(id,site_id,status,created_at,updated_at) VALUES
		(1,1,'pending','2001-01-01 00:00:00','2001-01-01 00:00:00')`); err != nil {
		t.Fatal(err)
	}
}

func TestDeploymentTimingQueueFinalizesObservedScriptResult(t *testing.T) {
	t.Setenv("FLUXO_ENV", "test")
	for _, tc := range []struct {
		name, mode string
		err        error
	}{
		{"managed success", ScriptModeManaged, nil},
		{"managed failure", ScriptModeManaged, errors.New("script failed")},
		{"legacy success", "legacy", nil},
		{"legacy failure", "legacy", errors.New("script failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deploymentTimingDB(t)
			if _, err := database.DB.Exec("UPDATE sites SET deploy_script_mode=? WHERE id=1", tc.mode); err != nil {
				t.Fatal(err)
			}
			var scriptReturned time.Time
			called := false
			processDeploymentWithRunner(1, 1, func(_ context.Context, _ int, _ int64, _, _, _ string, _ map[string]string, _ LogBroadcaster) (string, error) {
				called = true
				var started time.Time
				var finished sql.NullTime
				var status string
				if err := database.DB.QueryRow("SELECT status,started_at,finished_at FROM deployments WHERE id=1").Scan(&status, &started, &finished); err != nil || status != "running" || started.IsZero() || finished.Valid {
					t.Fatal("script ran before admission or finish captured too soon", status, started, finished, err)
				}
				scriptReturned = time.Now().UTC()
				return "script output", tc.err
			})
			var status, output, reason, branch string
			var started, finished time.Time
			if err := database.DB.QueryRow("SELECT status,output,failure_reason,branch,started_at,finished_at FROM deployments WHERE id=1").Scan(&status, &output, &reason, &branch, &started, &finished); err != nil {
				t.Fatal(err)
			}
			want := "success"
			if tc.err != nil {
				want = "failed"
				if reason != tc.err.Error() {
					t.Fatal("failure reason lost", reason)
				}
			}
			if !called || status != want || output == "" || branch != "main" || finished.Before(scriptReturned) || finished.Before(started) || finished.Location() != time.UTC {
				t.Fatal("invalid terminal result", called, status, output, branch, started, finished)
			}
			if claimed, err := database.ClaimPendingDeployment(1, 1); err != nil || claimed {
				t.Fatal("terminal attempt readmitted", claimed, err)
			}
		})
	}
}

type failingDeploymentWrite struct {
	db       *sql.DB
	calls    int
	finishes []time.Time
	failAll  bool
}

func (w *failingDeploymentWrite) Exec(query string, args ...any) (sql.Result, error) {
	w.calls++
	w.finishes = append(w.finishes, args[3].(time.Time))
	if w.calls == 1 || w.failAll {
		return nil, errors.New("injected result persistence failure")
	}
	return w.db.Exec(query, args...)
}

func TestDeploymentTimingResultRetryPreservesObservedFinish(t *testing.T) {
	deploymentTimingDB(t)
	if claimed, err := database.ClaimPendingDeployment(1, 1); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	finishedAt := time.Now().UTC()
	writer := &failingDeploymentWrite{db: database.DB}
	result := deploymentResult{status: "success", output: "done", metadata: &deploymentMetadata{commitHash: "abc123", branch: "main"}}
	if err := persistDeploymentResult(writer, 1, result, finishedAt); err != nil {
		t.Fatal(err)
	}
	if writer.calls != 2 || !writer.finishes[0].Equal(finishedAt) || !writer.finishes[1].Equal(finishedAt) {
		t.Fatal("retry replaced observed finish with persistence time", writer.calls, writer.finishes)
	}
	var stored time.Time
	var hash string
	if err := database.DB.QueryRow("SELECT finished_at,commit_hash FROM deployments WHERE id=1").Scan(&stored, &hash); err != nil || !stored.Equal(finishedAt) || hash != "abc123" {
		t.Fatal("incorrect persisted result", stored, hash, err)
	}
	// A stale completion cannot rewrite a terminal attempt, even after a later
	// trigger has created a separate pending record.
	if _, err := database.DB.Exec("INSERT INTO deployments(id,site_id,status) VALUES(2,1,'pending')"); err != nil {
		t.Fatal(err)
	}
	if err := persistDeploymentResult(database.DB, 1, deploymentResult{status: "failed", output: "late write"}, finishedAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.DB.QueryRow("SELECT count(*) FROM deployments WHERE (id=1 AND status='success' AND finished_at=?) OR (id=2 AND status='pending' AND started_at IS NULL AND finished_at IS NULL)", finishedAt).Scan(&count); err != nil || count != 2 {
		t.Fatal("retry changed attempt history", count, err)
	}
}

func TestDeploymentTimingResultWriteExhaustionDoesNotInventFinish(t *testing.T) {
	deploymentTimingDB(t)
	if claimed, err := database.ClaimPendingDeployment(1, 1); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	writer := &failingDeploymentWrite{db: database.DB, failAll: true}
	finishedAt := time.Now().UTC()
	if err := persistDeploymentResult(writer, 1, deploymentResult{status: "failed", failureReason: "observed failure"}, finishedAt); err == nil || writer.calls != 3 {
		t.Fatal("write failure hidden or retries unbounded", writer.calls, err)
	}
	for _, finish := range writer.finishes {
		if !finish.Equal(finishedAt) {
			t.Fatal("finish changed on retry", writer.finishes)
		}
	}
	if err := database.InterruptRunningDeployments(); err != nil {
		t.Fatal(err)
	}
	var started time.Time
	var finished sql.NullTime
	if err := database.DB.QueryRow("SELECT started_at,finished_at FROM deployments WHERE id=1").Scan(&started, &finished); err != nil || started.IsZero() || finished.Valid {
		t.Fatal("restart substituted detection time after exhausted persistence", started, finished, err)
	}
}

func TestDeploymentTimingPrestartFailureHasNoExecutionBoundaries(t *testing.T) {
	deploymentTimingDB(t)
	failDeployment(1, "cancelled before admission", "")
	var status string
	var started, finished sql.NullTime
	if err := database.DB.QueryRow("SELECT status,started_at,finished_at FROM deployments WHERE id=1").Scan(&status, &started, &finished); err != nil || status != "failed" || started.Valid || finished.Valid {
		t.Fatal("prestart failure invented an execution duration", status, started, finished, err)
	}
}
