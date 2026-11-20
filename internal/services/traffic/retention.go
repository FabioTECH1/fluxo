package traffic

import (
	"errors"
	"time"
)

const DefaultRetentionDays = 30

// Zero means keep aggregates until explicit history deletion. Storage and work
// budgets still apply; this does not make collection or reporting unbounded.
func ValidateRetentionDays(days int) error {
	switch days {
	case 0, 30, 60, 90, 180, 365:
		return nil
	default:
		return errors.New("retention_days must be 0 (keep until manually deleted), 30, 60, 90, 180 or 365")
	}
}

// SetRetentionDays serializes parser policy changes with collection and cleanup.
// The controller owns persistence; it saves the policy before calling this.
func (c *Collector) SetRetentionDays(days int) error {
	if err := ValidateRetentionDays(days); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store.mu.Lock()
	c.store.retentionDays = days
	c.store.mu.Unlock()
	c.lastMaintenance = time.Time{}
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return nil
}

func (s *Store) retentionStart(now time.Time) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retentionDays == 0 {
		return time.Time{}
	}
	return now.Add(-time.Duration(s.retentionDays) * 24 * time.Hour)
}
