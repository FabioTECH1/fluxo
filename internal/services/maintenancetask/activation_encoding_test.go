package maintenancetask

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCurrentActivationAcceptsExactPriorMergeMapOrderingWithoutRewrite(t *testing.T) {
	// Actual prior schema4/3 activation output on both retained native fixtures.
	source := []byte(`{"entries":{},"invocations":{},"schema":3}`)
	preserved := string(source)
	var j journal
	if err := json.Unmarshal(source, &j); err != nil {
		t.Fatal(err)
	}
	if err := validateActivationReceiptEncoding(source, j); err != nil {
		t.Fatal(err)
	}
	if string(source) != preserved {
		t.Fatal("encoding check rewrote immutable source")
	}
	pretty := []byte("{\n \"invocations\": {}, \"schema\": 3, \"entries\": {}\n}")
	if err := validateActivationReceiptEncoding(pretty, j); err != nil {
		t.Fatal(err)
	}
	// This is a meaning comparison, not permission to drop opaque history or to
	// use a locally changed snapshot after qualification.
	if validateActivationReceiptEncoding([]byte(`{"entries":{},"invocations":{},"schema":3,"retained_history":{"id":9223372036854775807}}`), j) == nil {
		t.Fatal("unrepresented history was silently normalized away")
	}
	j.Schema = 2
	if validateActivationReceiptEncoding(source, j) == nil {
		t.Fatal("changed semantic snapshot accepted")
	}
}
func TestLegacyFirstConversionKeepsExactReceiptEncodingFence(t *testing.T) {
	j := journal{Schema: 1, Entries: map[int]registration{}, Invocations: map[string]invocation{}}
	canonical, _ := json.Marshal(j)
	if err := validateActivationReceiptEncoding(canonical, j); err != nil {
		t.Fatal(err)
	}
	if validateActivationReceiptEncoding([]byte(`{"entries":{},"invocations":{},"schema":1}`), j) == nil {
		t.Fatal("legacy first-conversion replay fence relaxed")
	}
}

func TestCurrentActivationRejectsDuplicateKeysBeforeMeaningComparison(t *testing.T) {
	empty := journal{Schema: 3, Entries: map[int]registration{}, Invocations: map[string]invocation{}}
	for _, raw := range []string{
		`{"schema":3,"schema":3,"entries":{},"invocations":{}}`,
		`{"schema":1,"schema":3,"entries":{},"invocations":{}}`,
		`{"schema":3,"entries":{},"entries":{},"invocations":{}}`,
		`{"schema":3,"\u0065ntries":{"9":{}},"entries":{},"invocations":{}}`,
	} {
		if err := validateActivationReceiptEncoding([]byte(raw), empty); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatal("duplicate authority was interpreted with last-key-wins", err)
		}
	}
	r := registration{}
	current := journal{Schema: 3, Entries: map[int]registration{7: r}, Invocations: map[string]invocation{}}
	canonical, _ := json.Marshal(current)
	row, _ := json.Marshal(r)
	for _, first := range [][]byte{row, []byte(`{"future_authority":true}`)} {
		duplicate := bytes.Replace(canonical, append([]byte(`"7":`), row...), []byte(`"7":`+string(first)+`,"7":`+string(row)), 1)
		if err := validateActivationReceiptEncoding(duplicate, current); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatal("nested duplicate entry identity accepted", err)
		}
	}
	if _, err := normalizeActivationReceiptJSON(bytes.Repeat([]byte(" "), (8<<20)+1)); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatal("bounded receipt normalization missing", err)
	}
}
