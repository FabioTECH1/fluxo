package maintenancetask

import (
	"context"
	"errors"
	"fmt"
)

// BridgeReady performs no initialization, trigger publication or retirement.
// The current journal and content-addressed helper readers remain supported.
func BridgeReady(ctx context.Context) error { return bridgeReady(ctx, true) }

// BridgeReadyForInstaller validates retained authority without demanding that a
// known shared-lease owner is already terminal. The normal installer must still
// acquire exclusive lifecycle admission and drain it before changing the host.
// Submitted authority and its exact compatible helper are retained, not cleared.
func BridgeReadyForInstaller(ctx context.Context) error { return bridgeReady(ctx, false) }

// ExecutionReadyForInstaller retains authority, reader, trigger, acknowledgement
// and drainage checks. Future runtime configuration is qualified at actual launch
// or explicit runtime review, not as a prerequisite for installing the base UI.
func ExecutionReadyForInstaller(ctx context.Context) error {
	return bridgeReadyWithRegistration(ctx, false, bridgeExecutionRegistration)
}

func bridgeReady(ctx context.Context, requireTerminal bool) error {
	return bridgeReadyWithRegistration(ctx, requireTerminal, bridgeReadyRegistration)
}

func bridgeReadyWithRegistration(ctx context.Context, requireTerminal bool, check func(context.Context, registration) error) error {
	a, err := absent()
	if err != nil || a {
		return err
	}
	j, err := read()
	if err != nil {
		return err
	}
	if err = settled(ctx, &j); err != nil {
		return err
	}
	for _, r := range j.Entries {
		if r.Enabled {
			if err = check(ctx, r); err != nil {
				return err
			}
		}
	}
	for _, v := range j.Invocations {
		if requireTerminal && (v.Status == "submitted" || v.Status == "started" || v.Status == "failed") {
			return errors.New("reconcile outstanding guarded system-task authority before certification")
		}
	}
	return nil
}

func BridgeRegistration(ctx context.Context, c Contract, dbPath string) error {
	j, err := read()
	if err != nil {
		return err
	}
	r, ok := j.Entries[c.ID]
	if !ok || r.Contract != c || r.Database != dbPath {
		return errors.New("system-task row lacks a matching guarded registration")
	}
	return bridgeReadyRegistration(ctx, r)
}

func bridgeExecutionRegistration(ctx context.Context, r registration) error {
	if err := verifyRegistrationFiles(r); err != nil {
		return err
	}
	if err := verifyHelperReaderContracts(ctx, r.Helper); err != nil {
		return err
	}
	if err := requireCurrentLaunchAcknowledgement(ctx, r); err != nil {
		return err
	}
	if err := readyBackend(ctx, r); err != nil {
		return err
	}
	return nil
}

func bridgeReadyRegistration(ctx context.Context, r registration) error {
	if err := bridgeExecutionRegistration(ctx, r); err != nil {
		return err
	}

	// A loader receipt proves trigger consumption, not that the current host
	// still satisfies the job's execution contract. Inspect without dispatching
	// or approving changed code before certifying an enabled registration.
	if _, err := qualifyContractProfile(ctx, r.Contract); err != nil {
		return fmt.Errorf("guarded system-task %d current execution profile: %w", r.Contract.ID, err)
	}
	return nil
}

func BridgeRegisteredIDs() ([]int, error) {
	a, err := absent()
	if err != nil || a {
		return nil, err
	}
	j, err := read()
	if err != nil {
		return nil, err
	}
	var ids []int
	for id, r := range j.Entries {
		if r.Enabled {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
