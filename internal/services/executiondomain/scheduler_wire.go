package executiondomain

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"path/filepath"
)

func inspectHistoricalRetirementWire(raw []byte, schema int, floor *ReaderFloor) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	value, present := fields["historical_retirement"]
	if !present {
		return nil
	}
	var h struct {
		Contract   string `json:"contract"`
		Operation  string `json:"operation"`
		Source     string `json:"source_sha256"`
		Phase      string `json:"phase"`
		Completion string `json:"completion_sha256,omitempty"`
	}
	d := json.NewDecoder(bytes.NewReader(value))
	d.DisallowUnknownFields()
	if err := d.Decode(&h); err != nil {
		return err
	}
	id, err := uuid.Parse(h.Operation)
	source, digestErr := hex.DecodeString(h.Source)
	completion, completionErr := hex.DecodeString(h.Completion)
	if schema != 5 || h.Contract != SchedulerHistoricalRetirementContract || err != nil || id.String() != h.Operation ||
		digestErr != nil || len(source) != 32 || hex.EncodeToString(source) != h.Source ||
		h.Phase != "pending" && h.Phase != "complete" || h.Phase == "pending" && h.Completion != "" ||
		h.Phase == "complete" && (completionErr != nil || len(completion) != 32 || hex.EncodeToString(completion) != h.Completion) {
		return errors.New("unknown or incomplete historical retirement wire contract")
	}
	if floor == nil || floor.SchedulerSupport < 7 || !floorRoleContains(*floor, SchedulerHistoricalRetirementRole, SchedulerHistoricalRetirementContract) ||
		h.Phase == "pending" && floor.ActivationID != h.Operation {
		return errors.New("historical retirement lacks its reader/operation floor")
	}
	return nil
}

// This leaf admission reader recognizes wire semantics before configuration or
// SQLite initialization. Complete domain/seal evidence remains the subsystem
// reader's responsibility; an unknown contract never reaches that first write.
func inspectSchedulerWire(raw []byte, schema int, invocations map[string]json.RawMessage) error {
	var core map[string]json.RawMessage
	if err := json.Unmarshal(raw, &core); err != nil {
		return err
	}
	if schema >= SchedulerLoaderSchema {
		v, ok := core["loader_epochs_established"]
		if !ok || !bytes.Equal(bytes.TrimSpace(v), []byte("true")) && !bytes.Equal(bytes.TrimSpace(v), []byte("false")) {
			return errors.New("schema5 requires explicit loader establishment evidence")
		}
	}
	for _, rawInvocation := range invocations {
		var v struct {
			Contract string          `json:"execution_contract"`
			Helper   string          `json:"helper"`
			SHA      string          `json:"helper_sha256"`
			Seal     json.RawMessage `json:"bootstrap_seal"`
		}
		if err := json.Unmarshal(rawInvocation, &v); err != nil {
			return err
		}
		hasSeal := len(v.Seal) > 0 && !bytes.Equal(bytes.TrimSpace(v.Seal), []byte("null"))
		if v.Contract == "" {
			if v.Helper != "" || v.SHA != "" || hasSeal {
				return errors.New("bootstrap evidence lacks its versioned execution contract")
			}
			continue // explicit historical invocation semantics, including schema5 history
		}
		sha, err := hex.DecodeString(v.SHA)
		if schema < SchedulerLoaderSchema || v.Contract != SchedulerBootstrapContract || err != nil || len(sha) != 32 || v.Helper != filepath.Join("/var/lib/fluxo-scheduler/helpers", v.SHA) {
			return errors.New("unknown or unfenced Scheduler bootstrap contract")
		}
	}
	return nil
}
