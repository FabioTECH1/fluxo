package phpupdate

import (
	"context"
	"errors"
	"fluxo/internal/syscmd"
	"golang.org/x/sys/unix"
	"os"
	"time"
)

// Shared by PHP package maintenance and caller-owned runtime coordination.
// Intent is persisted before native signals; recovery never guesses delivery.
type MaintenanceStop struct {
	Head     string `json:"head,omitempty"`
	Delivery string `json:"delivery,omitempty"`
	Identity string `json:"identity,omitempty"`
}

func StopMaintenanceConsumer(ctx context.Context, head string, u MaintenanceRuntime, state *MaintenanceStop, save func() error) error {
	current, err := observeUnit(ctx, u.Name, u.CGroup)
	if err != nil {
		return err
	}
	if !u.Active && (current.Active || len(current.PIDs) > 0) {
		return errors.New("A previously stopped consumer was started during maintenance")
	}
	if u.Natural {
		return nil
	} // Arbitrary Artisan commands never receive a signal.
	if u.Active && current.Active && !sameOriginalMain(u, current) {
		return errors.New("Consumer main process changed before graceful shutdown")
	}
	if !u.Active || !current.Active || !sameOriginalMain(u, current) {
		return nil
	}
	if u.Octane == nil {
		_, err = syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "stop", "--no-block", u.Name)
		return err
	}
	if state.Delivery == "sent" || state.Head == head {
		return nil
	}
	if u.Octane.Backend == "roadrunner" && state.Head != "" {
		return errors.New("RoadRunner shutdown delivery is uncertain; complete a controlled native shutdown before recovery")
	}
	state.Head, state.Delivery, state.Identity = head, "intent", u.PIDs[u.Octane.MasterPID]
	if err = save(); err != nil {
		return err
	}
	if key, e := processKey(u.Octane.MasterPID); e == nil && key == state.Identity {
		if err = signalRecordedProcess(ctx, u, u.Octane.MasterPID, unix.SIGTERM); err != nil {
			return err
		}
		state.Delivery = "sent"
		return save()
	} else if !errors.Is(e, os.ErrNotExist) {
		return errors.New("Octane serving master changed before graceful stop")
	}
	return nil
}
