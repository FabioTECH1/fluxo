package nginxupdate

import (
	"context"
	"errors"
	"log"
	"math/rand/v2"
	"time"
)

const (
	dailyInterval = 24 * time.Hour
	failedRetry   = time.Hour
	busyRetry     = 5 * time.Minute
	startupDelay  = 2 * time.Minute
	startupJitter = 3 * time.Minute
	schedulePoll  = time.Minute
)

func scheduledCheckDue(v Saved, now time.Time) bool {
	timestamp := now.Unix()
	if v.NextCheck > 0 {
		return v.NextCheck <= timestamp || v.NextCheck > now.Add(dailyInterval).Unix()
	}
	if v.LastSuccessfulCheck > timestamp || v.AttemptedAt > timestamp {
		return true // A clock change must not hide legacy discovery indefinitely.
	}
	// Existing saved discoveries predate the optional next_check field.
	if v.Error != "" && v.AttemptedAt > 0 {
		return timestamp-v.AttemptedAt >= int64(failedRetry/time.Second)
	}
	return v.LastSuccessfulCheck == 0 || timestamp-v.LastSuccessfulCheck >= int64(dailyInterval/time.Second)
}

// Start belongs to the installed panel's existing background lifecycle, like
// PHP/Node discovery. It needs no browser, new daemon or cron registration.
// Startup jitter avoids competing with their initial checks or synchronizing
// repository requests across a fleet. Reads continue to load saved facts only.
func (m *Manager) Start(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	m.startOnce.Do(func() {
		go func() {
			delay := startupDelay + time.Duration(rand.IntN(int(startupJitter/time.Second)+1))*time.Second
			timer := time.NewTimer(delay)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					if ctx.Err() != nil {
						return
					}
					_, err := m.check(ctx, true)
					delay = schedulePoll
					if err != nil && ctx.Err() == nil {
						if errors.Is(err, ErrBusy) {
							delay = busyRetry
						} else {
							log.Printf("Nginx daily package check: %v", err)
							delay = failedRetry
						}
					}
					timer.Reset(delay)
				}
			}
		}()
	})
}
