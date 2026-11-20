package deploy

import (
	"database/sql"
	"time"
)

type deploymentResult struct {
	status, output, failureReason string
	metadata                      *deploymentMetadata
}

type deploymentMetadata struct {
	commitHash, commitMessage, commitAuthor, branch string
}

type deploymentResultExecutor interface {
	Exec(string, ...any) (sql.Result, error)
}

// persistDeploymentResult receives an already observed terminal time. Retrying
// the write must never include persistence delay in the execution duration.
func persistDeploymentResult(db deploymentResultExecutor, deployID int64, result deploymentResult, finishedAt time.Time) error {
	query := `UPDATE deployments SET status = ?, output = ?, failure_reason = ?,
		finished_at = CASE WHEN status = 'running' AND started_at IS NOT NULL THEN ? ELSE NULL END,
		updated_at = CURRENT_TIMESTAMP`
	args := []any{result.status, result.output, result.failureReason, finishedAt.UTC()}
	if m := result.metadata; m != nil {
		query += `, commit_hash = ?, commit_message = ?, commit_author = ?, branch = ?`
		args = append(args, m.commitHash, m.commitMessage, m.commitAuthor, m.branch)
	}
	// A terminal record belongs to its original attempt. Late writes must not
	// alter its timing or overwrite a restart interruption/dismissal history.
	query += ` WHERE id = ? AND status IN ('pending', 'running')`
	args = append(args, deployID)
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
		}
		if _, err = db.Exec(query, args...); err == nil {
			return nil
		}
	}
	return err
}
