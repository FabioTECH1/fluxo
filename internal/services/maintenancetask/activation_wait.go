package maintenancetask

import (
	"context"
	"time"
)

// WaitActivationAcknowledgements observes genuine existing receipts while the
// installed panel and cron remain running. It never creates an acknowledgement
// or changes registration state. The caller owns exclusive lifecycle admission.
func WaitActivationAcknowledgements(ctx context.Context) error {
	for {
		pending, err := activationAcknowledgementsPending(ctx)
		if err != nil || !pending {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func activationAcknowledgementsPending(ctx context.Context) (bool, error) {
	a, err := absent()
	if err != nil || a {
		return false, err
	}
	unlock, err := gate(ctx, false)
	if err != nil {
		return false, err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return false, err
	}
	for _, r := range j.Entries {
		if r.Enabled && r.Published && !r.Received {
			if err = verifyRegistrationFiles(r); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	return false, nil
}
