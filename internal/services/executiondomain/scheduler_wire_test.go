package executiondomain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEarlySchedulerWireCannotInferNewExecutionSemantics(t *testing.T) {
	sha := strings.Repeat("a", 64)
	good := json.RawMessage(`{"execution_contract":"scheduler-bootstrap-drop-exec-v1","helper":"/var/lib/fluxo-scheduler/helpers/` + sha + `","helper_sha256":"` + sha + `"}`)
	core := []byte(`{"loader_epochs_established":false}`)
	if err := inspectSchedulerWire(core, 5, map[string]json.RawMessage{"new": good, "old": json.RawMessage(`{"started":true}`)}); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		core   []byte
		schema int
		v      json.RawMessage
	}{
		"missing explicit history": {[]byte(`{}`), 5, good},
		"null establishment":       {[]byte(`{"loader_epochs_established":null}`), 5, good},
		"integer establishment":    {[]byte(`{"loader_epochs_established":0}`), 5, good},
		"old schema new contract":  {core, 4, good},
		"unknown contract":         {core, 5, json.RawMessage(`{"execution_contract":"future"}`)},
		"legacy gained helper":     {core, 5, json.RawMessage(`{"helper":"/var/lib/fluxo-scheduler/dispatcher"}`)},
		"legacy gained seal":       {core, 5, json.RawMessage(`{"bootstrap_seal":{}}`)},
		"mutable dispatcher":       {core, 5, json.RawMessage(`{"execution_contract":"scheduler-bootstrap-drop-exec-v1","helper":"/var/lib/fluxo-scheduler/dispatcher","helper_sha256":"` + sha + `"}`)},
	} {
		t.Run(name, func(t *testing.T) {
			if inspectSchedulerWire(tc.core, tc.schema, map[string]json.RawMessage{"v": tc.v}) == nil {
				t.Fatal("new or malformed wire authority reached initialization")
			}
		})
	}
}
func TestHistoricalReaderMinimumsDoNotMoveWithCurrent(t *testing.T) {
	for schema, support := range map[int]int{2: 1, 3: 4, 4: 5, 5: 6} {
		if MinimumSchedulerReaderSupport(schema) != support {
			t.Fatal("historical scheduler floor reinterpreted", schema)
		}
	}
	for schema, support := range map[int]int{1: 1, 2: 2, 3: 3} {
		if MinimumSystemTaskReaderSupport(schema) != support {
			t.Fatal("historical task floor reinterpreted", schema)
		}
	}
}
