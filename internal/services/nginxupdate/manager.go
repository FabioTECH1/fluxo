package nginxupdate

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

var ErrBusy = errors.New("A Nginx package check is already in progress")
var ErrChanged = errors.New("The available Nginx update changed; refresh before dismissing it")
var errMaintenanceBusy = errors.New("Nginx package discovery is waiting for current maintenance to finish")

type Saved struct {
	Format              int       `json:"format"`
	Snapshot            *Snapshot `json:"snapshot"`
	LastSuccessfulCheck int64     `json:"last_successful_check"`
	AttemptedAt         int64     `json:"attempted_at"`
	Error               string    `json:"error"`
	DismissedIdentity   string    `json:"dismissed_identity,omitempty"`
	NextCheck           int64     `json:"next_check,omitempty"`
}
type Status struct {
	Snapshot            *Snapshot `json:"snapshot"`
	LastSuccessfulCheck int64     `json:"last_successful_check"`
	AttemptedAt         int64     `json:"attempted_at"`
	Error               string    `json:"error"`
	Stale               bool      `json:"stale"`
	Checking            bool      `json:"checking"`
	Notice              bool      `json:"notice"`
	RunningAdoption     string    `json:"running_adoption"`
}
type Store interface {
	Load() (Saved, error)
	Save(Saved) error
}
type Manager struct {
	mu         sync.Mutex
	checking   bool
	lastError  string
	retryAfter int64
	startOnce  sync.Once
	Store      Store
	Admit      func(context.Context) (func(), error)
	Refresh    func(context.Context) error
	Inspect    func(context.Context) (Snapshot, error)
	Now        func() time.Time
}

func New(store Store) *Manager {
	return &Manager{Store: store, Admit: CheckAdmission, Refresh: Refresh, Inspect: nativeInspect, Now: time.Now}
}
func (m *Manager) status(v Saved) Status {
	if m.lastError != "" {
		v.Error = m.lastError
	}
	age := m.Now().Unix() - v.LastSuccessfulCheck
	stale := v.LastSuccessfulCheck == 0 || age < 0 || age > 86400 || v.Error != ""
	return Status{Snapshot: v.Snapshot, LastSuccessfulCheck: v.LastSuccessfulCheck, AttemptedAt: v.AttemptedAt, Error: v.Error, Stale: stale, Checking: m.checking, Notice: !stale && v.Snapshot != nil && v.Snapshot.State == "update_available" && v.Snapshot.Identity != "" && v.DismissedIdentity != v.Snapshot.Identity, RunningAdoption: "not_verified"}
}
func (m *Manager) Status() (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, e := m.Store.Load()
	return m.status(v), e
}
func (m *Manager) Check(ctx context.Context) (Status, error) {
	return m.check(ctx, false)
}

func (m *Manager) check(ctx context.Context, scheduled bool) (Status, error) {
	m.mu.Lock()
	if m.checking {
		m.mu.Unlock()
		return Status{}, ErrBusy
	}
	value, e := m.Store.Load()
	if e != nil {
		m.mu.Unlock()
		return Status{}, e
	}
	now := m.Now()
	if scheduled && (m.retryAfter > now.Unix() || m.retryAfter == 0 && !scheduledCheckDue(value, now)) {
		result := m.status(value)
		m.mu.Unlock()
		return result, nil
	}
	m.checking = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.checking = false; m.mu.Unlock() }()
	value.Format = 1
	previousAttempt := value.AttemptedAt
	value.AttemptedAt = m.Now().Unix()
	parentContext := ctx
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	admission, cancelAdmission := context.WithTimeout(ctx, 5*time.Second)
	release, e := m.Admit(admission)
	cancelAdmission()
	if e == nil {
		defer release()
		e = m.Refresh(ctx)
		if e == nil {
			var snapshot Snapshot
			snapshot, e = m.Inspect(ctx)
			if e == nil {
				value.Snapshot = &snapshot
				value.LastSuccessfulCheck = m.Now().Unix()
				value.Error = ""
			}
		}
	}
	if scheduled && parentContext.Err() != nil {
		return Status{}, parentContext.Err()
	}
	deferred := scheduled && errors.Is(e, errMaintenanceBusy)
	switch {
	case deferred:
		// Contention is not a repository refresh failure. Preserve the last
		// result and its age, and retain the retry across panel restarts.
		value.AttemptedAt = previousAttempt
		value.NextCheck = m.Now().Add(busyRetry).Unix()
	case e != nil:
		value.Error = e.Error()
		value.NextCheck = m.Now().Add(failedRetry).Unix()
	default:
		value.NextCheck = m.Now().Add(dailyInterval).Unix()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.Store.Save(value); e != nil {
		m.lastError = "The Nginx check result could not be saved; the previous result is stale"
		m.retryAfter = m.Now().Add(failedRetry).Unix()
		return Status{}, e
	}
	m.retryAfter = 0
	if !deferred {
		m.lastError = ""
	}
	result := m.status(value)
	result.Checking = false
	return result, nil
}
func (m *Manager) Dismiss(identity string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.checking {
		return Status{}, ErrBusy
	}
	v, e := m.Store.Load()
	if e != nil {
		return Status{}, e
	}
	if identity == "" || v.Snapshot == nil || v.Snapshot.Identity != identity || v.Snapshot.State != "update_available" {
		return Status{}, ErrChanged
	}
	v.Format = 1
	v.DismissedIdentity = identity
	if e = m.Store.Save(v); e != nil {
		return Status{}, e
	}
	return m.status(v), nil
}

// DecodeSaved rejects malformed persistence rather than inventing a fresh result.
func DecodeSaved(raw string) (Saved, error) {
	if raw == "" {
		return Saved{Format: 1}, nil
	}
	var v Saved
	if len(raw) > 65536 || json.Unmarshal([]byte(raw), &v) != nil || v.Format != 1 {
		return Saved{}, errors.New("Saved Nginx discovery state could not be read")
	}
	return v, nil
}
