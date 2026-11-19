package maintenancetask

import (
	"encoding/json"
	"errors"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/scheduler"
)

// ValidateInstallerHistoricalActivationPlan accepts ordinary backend continuity
// without a retirement witness. It binds every same-boot replacement
// to its original acknowledged source and exact intended generation. Live
// retirement is separately rechecked before the reader fence and final commit.
func ValidateInstallerHistoricalActivationPlan(original, intended []byte, image string, files []executiondomain.ActivationFile, proofs []scheduler.CronBackendRetirement) error {
	var old, next journal
	if json.Unmarshal(original, &old) != nil || json.Unmarshal(intended, &next) != nil || (old.Schema != 1 && old.Schema != 2) {
		return errors.New("historical retirement requires an original schema1/2 task core")
	}
	if err := historicalInstallerInvocationsSettled(old); err != nil {
		return err
	}
	used := make([]bool, len(proofs))
	for id, r := range old.Entries {
		if !r.Enabled {
			continue
		}
		target, ok := next.Entries[id]
		if !ok {
			return errors.New("historical retirement lost a registration")
		}
		if target.Backend == r.Backend || target.Backend.Boot != r.Backend.Boot {
			if err := validateActivationBackendContinuity(old.Schema, r.Backend, target.Backend); err != nil {
				return err
			}
			continue
		}
		if !r.Published || !r.Received || r.Boot != r.Backend.Boot {
			return errors.New("historical retirement lacks original acknowledgement")
		}
		found := false
		for i, p := range proofs {
			if p.Saved == r.Backend && p.Current == target.Backend && !used[i] {
				if err := scheduler.ValidateInstallerHistoricalRetirement(p); err != nil {
					return err
				}
				found, used[i] = true, true
				break
			}
		}
		if !found {
			return errors.New("historical retirement lost its exact generation witness")
		}
	}
	for _, ok := range used {
		if !ok {
			return errors.New("historical retirement archive has unrelated evidence")
		}
	}
	return ValidateExecutionActivationPlan(original, intended, image, files)
}

func historicalInstallerInvocationsSettled(j journal) error {
	for _, v := range j.Invocations {
		switch v.Status {
		case "completed", "blocked", "interrupted":
		default:
			return errors.New("historical task execution authority is active or uncertain")
		}
	}
	return nil
}
