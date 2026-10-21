package phpupdate

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"fluxo/internal/database"
)

var ErrRollbackAcknowledgement = errors.New("explicit acknowledgement is required to update without verified package rollback")

// One generation retains the original package/runtime evidence. Attempts move
// its head forward; recovery never rewinds or overwrites an older attempt.
type recoveryGeneration struct {
	ID                    string          `json:"id"`
	Version               string          `json:"version"`
	PlanID                string          `json:"plan_id"`
	Head                  string          `json:"head"`
	State                 string          `json:"state"`
	Pending               bool            `json:"pending"`
	Goal                  string          `json:"goal"`
	VerifiedGoal          string          `json:"verified_goal,omitempty"`
	Archives              archiveManifest `json:"archives"`
	Original              runtimeSnapshot `json:"original"`
	Attempt               runtimeSnapshot `json:"attempt"`
	AcknowledgeNoRollback bool            `json:"acknowledge_no_rollback"`
}

func createGeneration(g recoveryGeneration) error {
	raw, err := json.Marshal(g)
	if err != nil {
		return err
	}
	_, err = database.DB.Exec("INSERT INTO php_update_generations(id,version,head_operation_id,state,payload,created_at) VALUES (?,?,?,?,?,?)", g.ID, g.Version, g.Head, generationState(g), string(raw), time.Now().Unix())
	return err
}

func getGeneration(id string) (recoveryGeneration, error) {
	var g recoveryGeneration
	var raw string
	err := database.DB.QueryRow("SELECT payload FROM php_update_generations WHERE id=?", id).Scan(&raw)
	if err != nil {
		return g, err
	}
	err = json.Unmarshal([]byte(raw), &g)
	return g, err
}

func generationForOperation(id string) (recoveryGeneration, error) {
	var root string
	err := database.DB.QueryRow("SELECT generation_id FROM php_update_recovery_attempts WHERE operation_id=?", id).Scan(&root)
	if errors.Is(err, sql.ErrNoRows) {
		root = id
	} else if err != nil {
		return recoveryGeneration{}, err
	}
	return getGeneration(root)
}

func saveGeneration(g recoveryGeneration) error {
	raw, err := json.Marshal(g)
	if err != nil {
		return err
	}
	result, err := database.DB.Exec("UPDATE php_update_generations SET state=?,payload=? WHERE id=? AND head_operation_id=?", generationState(g), string(raw), g.ID, g.Head)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrPlanChanged
	}
	return err
}

func generationState(g recoveryGeneration) string {
	if g.Pending {
		return "recovery_pending"
	}
	return g.State
}
