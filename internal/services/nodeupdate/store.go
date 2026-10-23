package nodeupdate

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fluxo/internal/database"
	"time"
)

func scanJSON(row interface{ Scan(...any) error }, v any) error {
	var raw string
	if err := row.Scan(&raw); err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), v)
}
func GetOperation(id string) (Operation, error) {
	var o Operation
	err := scanJSON(database.DB.QueryRow("SELECT payload FROM node_update_operations WHERE id=?", id), &o)
	return o, err
}
func GetPlan(id string) (Plan, error) {
	var p Plan
	err := scanJSON(database.DB.QueryRow("SELECT payload FROM node_update_plans WHERE id=?", id), &p)
	return p, err
}
func getGeneration(id string) (Generation, error) {
	var g Generation
	err := scanJSON(database.DB.QueryRow("SELECT payload FROM node_update_generations WHERE id=?", id), &g)
	return g, err
}
func savePlan(p Plan) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = database.DB.Exec("INSERT INTO node_update_plans(id,payload,created_at) VALUES(?,?,?)", p.ID, string(b), p.CreatedAt)
	return err
}
func saveGeneration(g Generation) error {
	b, err := json.Marshal(g)
	if err != nil {
		return err
	}
	result, err := database.DB.Exec(`INSERT INTO node_update_generations(id,head,state,payload) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,payload=excluded.payload WHERE node_update_generations.head=excluded.head`, g.ID, g.Head, g.State, string(b))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrChanged
	}
	return err
}
func updateOperation(id, status, phase, message string) error {
	o, err := GetOperation(id)
	if err != nil {
		return err
	}
	o.Status, o.Phase, o.Message, o.UpdatedAt = status, phase, message, time.Now().Unix()
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	_, err = database.DB.Exec("UPDATE node_update_operations SET status=?,updated_at=?,payload=? WHERE id=?", status, o.UpdatedAt, string(b), id)
	return err
}
func readSnapshot() (Snapshot, error) {
	s := Snapshot{State: "unchecked"}
	err := scanJSON(database.DB.QueryRow("SELECT payload FROM node_update_snapshot WHERE id=1"), &s)
	if s.State == "" {
		s.State = "unchecked"
	}
	return s, err
}
func saveSnapshot(s Snapshot, next int64) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = database.DB.Exec("UPDATE node_update_snapshot SET payload=?,next_check=? WHERE id=1", string(b), next)
	return err
}
func activeOperation() (*Operation, error) {
	var o Operation
	err := scanJSON(database.DB.QueryRow("SELECT payload FROM node_update_operations WHERE status IN ('queued','running') LIMIT 1"), &o)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &o, err
}
func ReadStatus() (Status, error) {
	var result Status
	var err error
	result.Snapshot, err = readSnapshot()
	if err != nil {
		return result, err
	}
	result.Operations = []Operation{}
	result.Recoveries = []Generation{}
	result.ActiveOperation, err = activeOperation()
	if err != nil {
		return result, err
	}
	rows, err := database.DB.Query("SELECT payload FROM node_update_operations ORDER BY rowid DESC LIMIT 30")
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var o Operation
		if err = scanJSON(rows, &o); err != nil {
			rows.Close()
			return result, err
		}
		result.Operations = append(result.Operations, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = database.DB.Query("SELECT payload FROM node_update_generations ORDER BY rowid DESC LIMIT 1")
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var g Generation
		if err = scanJSON(rows, &g); err != nil {
			rows.Close()
			return result, err
		}
		result.Recoveries = append(result.Recoveries, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	result.Locked, err = database.NodeMaintenancePending()
	s := &result.Snapshot
	s.CandidateID = digest(s.Candidate)
	s.UpdateAvailable = s.State == "available" && s.Candidate != nil && time.Now().Unix()-s.CheckedAt < FreshSeconds
	if (s.State == "available" || s.State == "current") && time.Now().Unix()-s.CheckedAt >= FreshSeconds {
		s.State = "stale"
	}
	var dismissed string
	if e := database.DB.QueryRow("SELECT dismissed_candidate FROM node_update_snapshot WHERE id=1").Scan(&dismissed); e != nil {
		return result, e
	}
	s.Notice = s.UpdateAvailable && digest(s.Candidate) != dismissed
	return result, err
}
func Dismiss(candidate string) error {
	s, err := readSnapshot()
	if err != nil {
		return err
	}
	if s.Candidate == nil || candidate != digest(s.Candidate) {
		return ErrChanged
	}
	_, err = database.DB.Exec("UPDATE node_update_snapshot SET dismissed_candidate=? WHERE id=1", candidate)
	return err
}
func CandidateID(s Snapshot) string { return digest(s.Candidate) }
