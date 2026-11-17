package scheduler

import (
	"context"
	"errors"
	"os"
)

// Schema5 entry receipts never fall through into historical A/B state. Their
// separate targets prove current provider loading, not retirement of old work.
func receiveLoaderEpochReceipt(ctx context.Context, j journal, branch, nonce string, id int) (bool, error) {
	if j.Schema != loaderEpochSchema || id <= 0 {
		return false, nil
	}
	if historicalRetirementPending(j) {
		// The strict core reader has verified the operation-bound committed
		// retirement fence. Existing backend-attempt validation remains the only
		// authority for these historical nonces; no loader receipt exists yet.
		return false, nil
	}
	if err := requireLoaderWriter(ctx); err != nil {
		return true, err
	}
	s, err := readLoaderStore()
	if err != nil {
		return true, err
	}
	epoch, ok := s.Epochs[s.Current]
	e, found := j.Entries[id]
	target, present := epoch.Targets[id]
	if !j.LoaderEpochEstablished || s.Pending != "" || !ok || epoch.Phase != "published" || !found || !e.Enabled || e.Version != branch || !present || target.Binding != e.Identity || nonce != target.Nonce || nonce != e.LoaderNonce || target.TriggerHash != hashBytes([]byte(CronContent(e))) {
		return true, errors.New("stale or unpublished current loader epoch receipt")
	}
	if err = owned(CronPath(id), false); err != nil {
		return true, err
	}
	raw, err := os.ReadFile(CronPath(id))
	if err != nil || hashBytes(raw) != target.TriggerHash {
		return true, errors.New("current loader trigger changed")
	}
	provider, err := qualifyLoaderProvider(ctx, &epoch.Provider)
	if err != nil {
		return true, err
	}
	if !sameLoaderProvider(provider, epoch.Provider) {
		return true, errors.New("current loader provider generation changed; reconcile before acknowledging")
	}
	if err = cronReceiptOrigin(epoch.Provider.Identity); err != nil {
		return true, err
	}
	target.Received = true
	epoch.Targets[id] = target
	s.Epochs[epoch.ID] = epoch
	return true, saveLoaderStore(s)
}
