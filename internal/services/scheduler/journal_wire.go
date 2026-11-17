package scheduler

import (
	"encoding/json"
	"errors"

	"fluxo/internal/services/executiondomain"
)

// A new Go field cannot retroactively change the bytes/meaning of archived
// schema4 activation projections. Schema5 alone emits the explicit false/true
// initialization fence. The shadow field suppresses the alias's bool for old
// schemas while preserving their original field order.
func (j journal) MarshalJSON() ([]byte, error) {
	if j.Schema < executiondomain.SchedulerLoaderSchema && j.LoaderEpochEstablished {
		return nil, errors.New("old Scheduler schema cannot claim loader establishment")
	}
	type wire journal
	var established *bool
	if j.Schema >= executiondomain.SchedulerLoaderSchema {
		value := j.LoaderEpochEstablished
		established = &value
	}
	return json.Marshal(struct {
		Established *bool `json:"loader_epochs_established,omitempty"`
		wire
	}{established, wire(j)})
}
