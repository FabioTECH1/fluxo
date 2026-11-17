package scheduler

import (
	"encoding/json"
	"testing"
)

func TestSchema4ProjectionDoesNotAcquireSchema5Field(t *testing.T) {
	j := emptyExecutionJournal()
	j.Schema = 4
	// Frozen schema4 projection shape used by immutable format2 activation plans.
	type oldWire struct {
		LastSweep   map[string]int64           `json:"last_sweep"`
		Schema      int                        `json:"schema"`
		Entries     map[int]Entry              `json:"entries"`
		Invocations map[string]invocation      `json:"invocations"`
		Holds       map[string]string          `json:"holds"`
		Released    map[string]bool            `json:"released"`
		Upgrade     *upgradeLease              `json:"upgrade"`
		Legacy      map[string]string          `json:"legacy"`
		Migrations  map[string]legacyMigration `json:"migrations"`
		PanelProof  *panelProof                `json:"panel_proof"`
	}
	want, e := json.Marshal(oldWire{j.LastSweep, j.Schema, j.Entries, j.Invocations, j.Holds, j.Released, j.Upgrade, j.Legacy, j.Migrations, j.PanelProof})
	if e != nil {
		t.Fatal(e)
	}
	got, e := json.Marshal(j)
	if e != nil || string(got) != string(want) {
		t.Fatalf("historical projection changed: %v\ngot %s\nwant%s", e, got, want)
	}
	j.Schema = 5
	got, e = json.Marshal(j)
	if e != nil {
		t.Fatal(e)
	}
	var fields map[string]json.RawMessage
	if e = json.Unmarshal(got, &fields); e != nil || string(fields["loader_epochs_established"]) != "false" {
		t.Fatal("schema5 lacks durable false fence", e, string(got))
	}
}
