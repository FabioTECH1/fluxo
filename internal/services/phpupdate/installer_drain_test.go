package phpupdate

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestInstallerCronWaitRepeatsAdmissionAndOwnership(t *testing.T) {
	busy := errors.New("qualified hierarchy still busy")
	var order []string
	var output bytes.Buffer
	checks := 0
	err := verifyInstallerCronDrain(context.Background(),
		func() error { order = append(order, "owner"); return nil },
		func() error {
			order = append(order, "check")
			checks++
			if checks == 1 {
				return busy
			}
			return nil
		},
		func() error { order = append(order, "maintenance"); return nil },
		func(err error) bool { return err == busy }, &output)
	want := []string{"owner", "check", "maintenance", "owner", "check", "owner"}
	if err != nil || !reflect.DeepEqual(order, want) || !strings.Contains(output.String(), "Waiting for earlier cron workers") || !strings.Contains(output.String(), "continuing the upgrade") {
		t.Fatal("wait did not repeat full admission under verified ownership", order, err, output.String())
	}
}

func TestInstallerCronWaitKeepsHardBlocksAndTheDeadline(t *testing.T) {
	busy := errors.New("qualified hierarchy still busy")
	blocked := errors.New("maintenance or ownership requires recovery")
	for _, name := range []string{"hard-admission", "maintenance", "owner-before", "owner-during-wait", "owner-after", "deadline", "already-canceled", "canceled-during-check"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			if name == "deadline" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 25*time.Millisecond)
			}
			defer cancel()
			if name == "already-canceled" {
				cancel()
			}
			owners, checks, maintenance := 0, 0, 0
			var output bytes.Buffer
			err := verifyInstallerCronDrain(ctx,
				func() error {
					owners++
					if name == "owner-before" || owners == 2 && (name == "owner-during-wait" || name == "owner-after") {
						return blocked
					}
					return nil
				},
				func() error {
					checks++
					if name == "hard-admission" {
						return blocked
					}
					if name == "canceled-during-check" {
						cancel()
						return nil
					}
					if name == "owner-after" {
						return nil
					}
					return busy
				},
				func() error {
					maintenance++
					if name == "maintenance" {
						return blocked
					}
					return nil
				},
				func(err error) bool { return err == busy }, &output)
			if name == "deadline" {
				if !errors.Is(err, context.DeadlineExceeded) || checks != 1 {
					t.Fatal("persistent activity crossed deadline", checks, err)
				}
			} else if name == "already-canceled" || name == "canceled-during-check" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation crossed admission", err)
				}
				if name == "already-canceled" && (owners != 0 || checks != 0) {
					t.Fatal("canceled admission performed work")
				}
			} else if !errors.Is(err, blocked) {
				t.Fatal("hard blocker retried or ignored", name, err)
			}
			if name == "owner-during-wait" && checks != 1 {
				t.Fatal("lost lease allowed another check", checks)
			}
			if (name == "hard-admission" || name == "maintenance") && checks != 1 {
				t.Fatal("maintenance blocker retried", checks)
			}
			if name == "hard-admission" && maintenance != 0 {
				t.Fatal("hard admission was reinterpreted")
			}
			if strings.Contains(output.String(), "Earlier cron workers finished") {
				t.Fatal("failure reported upgrade continuation")
			}
		})
	}
}
