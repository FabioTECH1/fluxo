package maintenancetask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/google/uuid"
)

// ValidateHistoricalCompactionLocked admits only the old schema1 compact/blocked
// projection. It never imports a receipt or grants a submitted unit authority.
func ValidateHistoricalCompactionLocked(ctx context.Context, original []byte) error {
	live, err := read()
	if err != nil {
		return err
	}
	current, err := readRegular(filepath.Join(stateRoot, "state.json"), 8<<20)
	if err != nil {
		return err
	}
	if err = validateActivationReceiptEncoding(current, live); err != nil {
		return err
	}
	var old journal
	if err = json.Unmarshal(original, &old); err != nil {
		return err
	}
	if err = validateActivationReceiptEncoding(original, old); err != nil {
		return err
	}
	if err = activationStateReady(); err != nil {
		return err
	}
	if err = validateHistoricalCompaction(old, live, func(name string) ([]byte, error) {
		return readRegular(filepath.Join(stateRoot, "history", name+".json"), 8<<20)
	}); err != nil {
		return err
	}
	// Include removed authorities as well as the live journal. A history file
	// cannot establish that the corresponding unit has naturally drained.
	// A disabled registration or a compacted invocation can still name
	// a retained helper that is no longer in the enabled trigger set.
	helpers := map[string]registration{}
	for _, r := range old.Entries {
		helpers[r.Helper] = r
	}
	for _, v := range old.Invocations {
		helpers[v.Registration.Helper] = v.Registration
	}
	for _, r := range helpers {
		if e := verifyRetainedHelper(r); e != nil {
			return e
		}
	}
	if err = registrationRequalificationDomainsEmpty(ctx, old); err != nil {
		return err
	}
	return installerHistoricalTasksQuiescent(ctx, live)
}

func validateHistoricalCompaction(old, live journal, receipt func(string) ([]byte, error)) error {
	if old.Schema != 1 || live.Schema != 1 || old.Entries == nil || len(old.Invocations) == 0 || live.Invocations == nil || !reflect.DeepEqual(old.Entries, live.Entries) {
		return errors.New("historical task registrations or schema changed")
	}
	if err := historicalInstallerInvocationsSettled(old); err != nil {
		return err
	}
	// compact walks the whole map under one gate and saves once. Every
	// archived terminal domain was already empty at planning. The narrowly
	// admitted addition is one subsequent, unconsumed Certbot submission.
	if len(live.Invocations) > 1 {
		return errors.New("unproven historical invocation set")
	}
	for name, v := range old.Invocations {
		if !validUnit(name) || name != v.Unit {
			return errors.New("invalid archived task unit")
		}
		if _, ok := live.Invocations[name]; ok {
			return errors.New("historical compaction did not retire the archived terminal set")
		}
		raw, err := receipt(name)
		want, e := json.Marshal(v)
		if err != nil || e != nil || !bytes.Equal(raw, want) {
			return errors.New("missing or changed historical compaction receipt")
		}
	}
	for name, v := range live.Invocations {
		unitID, e := uuid.Parse(strings.TrimSuffix(strings.TrimPrefix(name, "fluxo-system-task-"), ".service"))
		if e != nil || unitID.Version() != 4 || unitID.Variant() != uuid.RFC4122 || name != "fluxo-system-task-"+unitID.String()+".service" {
			return errors.New("invalid legacy submission identity")
		}
		// The old Execute can time out at the shared lifecycle lock before
		// consuming its launch. Bind even its inert saved profile to an exact
		// archived dispatch of this registration; ambiguous new work stays closed.
		r, ok := old.Entries[v.Registration.Contract.ID]
		if !ok || !r.Enabled || !r.Received || !r.Published || r.Contract.Kind != CertificateRenewal {
			return errors.New("new invocation lacks an acknowledged Certbot registration")
		}
		proven := false
		for _, prior := range old.Invocations {
			want := invocation{Unit: name, Registration: r, Boot: r.Boot, Status: "blocked",
				Error: "wait for maintenance lock: context deadline exceeded", Profile: prior.Profile}
			if reflect.DeepEqual(prior.Registration, r) && reflect.DeepEqual(v, want) {
				proven = true
				break
			}
		}
		if !proven {
			return errors.New("new blocked task has no matching archived execution profile")
		}
	}
	return nil
}
