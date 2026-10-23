package nodeupdate

import (
	"encoding/json"

	"fluxo/internal/services/phpupdate"
)

// A review approves service configuration and original execution intent. Job
// children may turn over between fully authenticated observations; controllers,
// native serving anchors, enablement and original running/stopped intent may not.
func sameApplyConsumers(current, reviewed Impact) bool {
	if !sameConsumers(current, reviewed) || len(reviewed.BlockedReasons) > 0 || !phpupdate.SameMaintenanceExecution(current.PHP, reviewed.PHP) {
		return false
	}
	for i := range current.Units {
		x, y := current.Units[i], reviewed.Units[i]
		if x.Active != y.Active || x.MainPID != y.MainPID || x.MainPID > 0 && x.PIDs[x.MainPID] != y.PIDs[y.MainPID] {
			return false
		}
	}
	return true
}

// Keep the approved Plan immutable, with its original intent, while retaining
// all authenticated process-domain evidence in the caller-owned coordination.
func applyCoordination(reviewed Impact, observations ...Impact) (Impact, error) {
	var result Impact
	b, err := json.Marshal(reviewed)
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(b, &result); err != nil {
		return result, err
	}
	merge := func(old, fresh map[int]string) map[int]string {
		if old == nil {
			old = map[int]string{}
		}
		for pid, key := range fresh {
			old[pid] = key
		}
		return old
	}
	for _, observed := range observations {
		if !sameApplyConsumers(observed, reviewed) {
			return Impact{}, ErrChanged
		}
		for i := range result.Units {
			result.Units[i].PIDs = merge(result.Units[i].PIDs, observed.Units[i].PIDs)
		}
		for i := range result.PHP {
			for j := range result.PHP[i].Units {
				u := &result.PHP[i].Units[j]
				u.Runtime.PIDs = merge(u.Runtime.PIDs, observed.PHP[i].Units[j].Runtime.PIDs)
			}
		}
	}
	return result, nil
}
