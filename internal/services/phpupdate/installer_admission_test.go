package phpupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestInstallerAdmissionReleasesRealLifecycleLockBeforeLaunchProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lifecycle.lock")
	busy := errors.New("unconsumed bootstrap")
	attempts, releases, pauses := 0, 0, 0
	advanced := false
	acquire := func(context.Context) (func(), error) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		return func() {
			releases++
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			_ = f.Close()
		}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, id, err := retryInstallerLeaseAdmission(ctx, func() bool { return true }, acquire,
		func(ctx context.Context) (string, error) {
			attempts++
			if attempts == 1 {
				return "", busy
			}
			if !advanced {
				t.Fatal("admitted before bootstrap could take shared lifecycle access")
			}
			return "verified-upgrade", nil
		}, func(err error) bool { return err == busy },
		func(context.Context) error {
			pauses++
			// A separately opened OFD represents the bootstrap helper, rather
			// than re-locking the installer's own descriptor/process mutex.
			f, err := os.OpenFile(path, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err = syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
				t.Fatal("installer retained exclusive ownership across the wait", err)
			}
			advanced = true
			return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		})
	if err != nil || release == nil || id != "verified-upgrade" || attempts != 2 || pauses != 1 || releases != 1 {
		t.Fatal("launch handoff did not preserve admission ownership", id, err, attempts, pauses, releases)
	}
	release()
	if releases != 2 {
		t.Fatal("successful caller did not retain/release its lease")
	}
}

func TestInstallerAdmissionWaitRetainsHardRefusalsAndParentChecks(t *testing.T) {
	busy := errors.New("unconsumed bootstrap")
	blocked := errors.New("changed evidence or real maintenance")
	for _, name := range []string{"hard", "joined", "acquire", "parent-before", "parent-after-lock", "parent-after-wait", "deadline", "canceled-before", "canceled-after-lock", "canceled-during-wait", "admitted-at-deadline"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "canceled-before" {
				cancel()
			}
			parents, acquired, prepared, released, paused := 0, 0, 0, 0, 0
			release, id, err := retryInstallerLeaseAdmission(ctx,
				func() bool {
					parents++
					return name != "parent-before" && !(name == "parent-after-lock" && parents == 2) && !(name == "parent-after-wait" && parents == 3)
				},
				func(context.Context) (func(), error) {
					acquired++
					if name == "acquire" {
						return nil, blocked
					}
					if name == "canceled-after-lock" {
						cancel()
					}
					return func() { released++ }, nil
				},
				func(context.Context) (string, error) {
					prepared++
					if name == "hard" {
						return "", blocked
					}
					if name == "joined" {
						return "", errors.Join(busy, blocked)
					}
					if name == "admitted-at-deadline" {
						cancel()
						return "durable-owner", nil
					}
					return "", busy
				}, func(err error) bool { return err == busy },
				func(context.Context) error {
					paused++
					if released != acquired {
						t.Fatal("pause kept an admission lock")
					}
					if name == "deadline" {
						return context.DeadlineExceeded
					}
					if name == "canceled-during-wait" {
						cancel()
					}
					return nil
				})
			if name == "admitted-at-deadline" {
				if err != nil || id != "durable-owner" || release == nil || paused != 0 || released != 0 {
					t.Fatal("retry window discarded an already durable admission", id, err)
				}
				release()
				return
			}
			if err == nil || release != nil || id != "" || released != acquired && name != "acquire" {
				t.Fatal("failed admission leaked ownership or reported READY", id, err, acquired, released)
			}
			if (name == "hard" || name == "joined" || name == "acquire") && (!errors.Is(err, blocked) || paused != 0) {
				t.Fatal("hard refusal was retried", err, paused)
			}
			if name == "parent-before" && acquired != 0 || (name == "parent-after-lock" || name == "canceled-after-lock") && prepared != 0 || name == "parent-after-wait" && acquired != 1 {
				t.Fatal("lost parent identity permitted admission work", parents, acquired, prepared)
			}
			if name == "deadline" && !errors.Is(err, context.DeadlineExceeded) || (name == "canceled-before" || name == "canceled-after-lock" || name == "canceled-during-wait") && !errors.Is(err, context.Canceled) {
				t.Fatal("deadline/cancellation was lost", err)
			}
		})
	}
}
