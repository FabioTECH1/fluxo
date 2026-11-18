package bridge

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"strings"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/maintenancetask"
)

func validSHA256(value string) bool {
	b, e := hex.DecodeString(value)
	return e == nil && len(b) == 32 && strings.ToLower(value) == value
}

// Mutation uses the same stopped-panel and durable archive machinery as the
// other execution operations, but its projection and source contract are task
// requalification only. It does not refresh a helper or change Scheduler state.
func MaintenanceRegistrationCommand(ctx context.Context, dataDir, version, action string, args []string) (any, error) {
	return maintenanceRegistrationCommand(ctx, dataDir, version, action, args, nil)
}
func maintenanceRegistrationCommand(ctx context.Context, dataDir, version, action string, args []string, candidate *maintenanceCandidateAdmission) (any, error) {
	if os.Geteuid() != 0 || dataDir != "/var/lib/fluxo" {
		return nil, errors.New("maintenance registration commands require root and the default installed scope")
	}
	switch action {
	case "inspect":
		if len(args) != 0 {
			break
		}
		if _, err := executiondomain.InspectCompatibility(dataDir); err != nil {
			return nil, err
		}
		release, err := lockCoordination(ctx)
		if err != nil {
			return nil, err
		}
		defer release()
		return maintenancetask.InspectRegistrationBackendsLocked(ctx)
	case "requalify":
		if len(args) != 2 || !validSHA256(args[0]) || !validSHA256(args[1]) {
			break
		}
		return activateExecutionFormats(ctx, dataDir, version, activationRequest{ExpectedTaskCore: args[0], TaskBackendSHA256: args[1], Candidate: candidate})
	case "recover":
		if len(args) != 1 {
			break
		}
		id, e := uuid.Parse(args[0])
		if e != nil || id.String() != args[0] {
			return nil, errors.New("recovery requires an exact canonical operation UUID")
		}
		raw, e := readProtected(filepath.Join(dataDir, "execution-activations", args[0], "plan.json"), 128<<20)
		if e != nil {
			return nil, e
		}
		var plan executionActivation
		if e = json.Unmarshal(raw, &plan); e != nil {
			return nil, e
		}
		if plan.Format != 4 || plan.Kind != activationTaskBackendRequalification || plan.ID != args[0] {
			return nil, errors.New("maintenance registration recovery cannot resume another operation kind")
		}

		// The generic recovery validates exact kind/image/UUID before publication.
		return activateExecutionFormats(ctx, dataDir, version, activationRequest{Resume: args[0], Candidate: candidate})
	}
	return nil, errors.New("usage: maintenance-registration inspect | requalify TASK_CORE_SHA256 CURRENT_BACKEND_SHA256 | recover OPERATION_UUID")
}

func validateRequalificationSchedulerSource(p executionActivation) error {
	if !taskBackendTransitionKind(p.Kind) {
		return nil
	}
	current, err := activationProtectedHash("/var/lib/fluxo-scheduler/state.json", 32<<20)
	if err != nil {
		return err
	}
	if current != p.SchedulerSourceSHA256 {
		return errors.New("Scheduler core changed during task-only backend requalification")
	}
	return nil
}

func taskRequalificationIntended(p executionActivation) []byte {
	for _, f := range p.Files {
		if f.Path == "/var/lib/fluxo-system-tasks/state.json" {
			return f.Data
		}
	}
	return nil
}
