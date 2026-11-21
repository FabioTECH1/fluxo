package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

func TestInstallerLaunchWaitRetainsUnconsumedAuthority(t *testing.T) {
	for _, sealed := range []bool{false, true} {
		v := sealedInvocationFixture()
		if !sealed {
			v.Bootstrap, v.ServiceIdentity = nil, nil
		}
		before, _ := json.Marshal(v)
		err := invocationQuiescent(context.Background(), v)
		if err == nil || !InstallerLaunchPending(err) {
			t.Fatalf("current unconsumed launch was not refused with a wait observation: %v", err)
		}
		after, _ := json.Marshal(v)
		if string(before) != string(after) {
			t.Fatal("classification consumed or removed launch authority")
		}
		if !InstallerLaunchPending(fmt.Errorf("installer admission: %w", err)) ||
			InstallerLaunchPending(errors.New(err.Error())) ||
			InstallerLaunchPending(errors.Join(err, errors.New("helper changed"))) ||
			InstallerLaunchPending(errors.Join(err)) {
			t.Fatal("wait classification accepted text or a mixed refusal")
		}
	}
	if InstallerLaunchPending(nil) {
		t.Fatal("success was classified as pending")
	}
}

func TestInstallerLaunchWaitDoesNotReinterpretOldOrInvalidAuthority(t *testing.T) {
	for name, change := range map[string]func(*invocation){
		"historical":        func(v *invocation) { *v = invocation{Guarded: true, Submitted: true} },
		"unknown contract":  func(v *invocation) { v.ExecutionContract = "future" },
		"unguarded":         func(v *invocation) { v.Guarded = false },
		"mutable helper":    func(v *invocation) { v.Helper = filepath.Join(stateDir, "dispatcher") },
		"changed seal":      func(v *invocation) { v.Bootstrap.EntryIdentity = "changed" },
		"unconsumed worker": func(v *invocation) { v.Bootstrap.Phase = "consumed" },
	} {
		t.Run(name, func(t *testing.T) {
			v := sealedInvocationFixture()
			change(&v)
			before, _ := json.Marshal(v)
			err := invocationQuiescent(context.Background(), v)
			if err == nil || InstallerLaunchPending(err) {
				t.Fatal("legacy or invalid authority entered automatic waiting", err)
			}
			after, _ := json.Marshal(v)
			if string(before) != string(after) {
				t.Fatal("refusal rewrote authority")
			}
		})
	}
}
