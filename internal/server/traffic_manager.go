package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/nginx"
	"fluxo/internal/services/traffic"
	"github.com/google/uuid"
)

const maxTrafficSites = 32

type trafficSiteInfo struct {
	ID         int64
	Domain     string
	ConfigName string
}

type trafficPreference struct {
	Enabled bool            `json:"enabled"`
	Source  *traffic.Source `json:"source,omitempty"`
}

type trafficSelection struct {
	MasterEnabled bool                        `json:"master_enabled"`
	Sites         map[int64]trafficPreference `json:"sites"`
}

type trafficTransition struct {
	Target    trafficSelection      `json:"target"`
	Phase     string                `json:"phase"`
	AppliedAt time.Time             `json:"applied_at,omitempty"`
	Workers   []nginx.TrafficWorker `json:"workers,omitempty"`
	Retired   []traffic.Source      `json:"retired,omitempty"`
}

type trafficDiskState struct {
	Format int `json:"format"`
	trafficSelection
	Pending *trafficTransition `json:"pending,omitempty"`
	Error   string             `json:"error,omitempty"`
	Notice  string             `json:"notice,omitempty"`
}

// trafficControl owns one collector for the panel. Configuration transitions
// are durable before Nginx changes; a reload or a client disconnect cannot make
// a half-applied request look like successful collection.
type trafficControl struct {
	mu        sync.Mutex
	dataDir   string
	path      string
	state     trafficDiskState
	store     *traffic.Store
	collector *traffic.Collector
	loaded    bool
	loadError error
	wake      chan struct{}
	apply     func(context.Context, []nginx.TrafficSite) (nginx.TrafficApplyResult, error)
	drained   func(context.Context, []nginx.TrafficWorker) (bool, error)
	reopen    func(context.Context) error
	retired   func(context.Context, string) (bool, error)
	listSites func(context.Context) ([]trafficSiteInfo, error)
	now       func() time.Time
	admit     func(context.Context) (func(), error)
	started   bool
	done      chan struct{}
	lastSweep time.Time
}

func newTrafficControl(dataDir string) *trafficControl {
	if absolute, err := filepath.Abs(dataDir); err == nil {
		dataDir = absolute
	}
	return &trafficControl{
		dataDir: dataDir, path: filepath.Join(dataDir, "traffic", "control.json"),
		wake: make(chan struct{}, 1), done: make(chan struct{}), apply: nginx.ApplyTraffic,
		drained: nginx.TrafficWorkersDrained, reopen: nginx.ReopenTrafficLogs,
		retired:   nginx.TrafficLogRetired,
		listSites: listTrafficSites, now: time.Now,
		admit: func(context.Context) (func(), error) { return func() {}, nil },
	}
}

func listTrafficSites(ctx context.Context) ([]trafficSiteInfo, error) {
	rows, err := database.DB.QueryContext(ctx, "SELECT id,domain,path FROM sites WHERE deletion_status='' OR deletion_status IS NULL ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]trafficSiteInfo, 0)
	for rows.Next() {
		var site trafficSiteInfo
		var path string
		if err := rows.Scan(&site.ID, &site.Domain, &path); err != nil {
			return nil, err
		}
		site.ConfigName = filepath.Base(filepath.Clean(path))
		result = append(result, site)
	}
	return result, rows.Err()
}

func emptyTrafficState() trafficDiskState {
	return trafficDiskState{Format: 1, trafficSelection: trafficSelection{Sites: make(map[int64]trafficPreference)}}
}

func (m *trafficControl) loadLocked() error {
	if m.loaded {
		return m.loadError
	}
	m.loaded = true
	m.state = emptyTrafficState()
	if dir, err := trafficControlDirectory(filepath.Dir(m.path), false); err == nil {
		dir.Close()
	} else if !os.IsNotExist(err) {
		m.loadError = err
		return err
	}
	f, err := os.OpenFile(m.path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		m.loadError = err
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		m.loadError = err
		return err
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.Mode().IsRegular() || st.Size() > 512<<10 || st.Mode().Perm()&0077 != 0 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		m.loadError = errors.New("traffic configuration has unsafe ownership, permissions or file type")
		return m.loadError
	}
	decoder := json.NewDecoder(io.LimitReader(f, (512<<10)+1))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&m.state); err == nil {
		var extra any
		if e := decoder.Decode(&extra); e != io.EOF {
			err = errors.New("invalid trailing traffic configuration")
		}
	}
	if err == nil && (m.state.Format != 1 || m.state.Sites == nil || len(m.state.Sites) > 4096) {
		err = errors.New("unsupported traffic configuration")
	}
	if err == nil {
		err = m.validateSelection(m.state.trafficSelection)
	}
	if err == nil && m.state.Pending != nil {
		err = m.validateSelection(m.state.Pending.Target)
		if err == nil {
			switch m.state.Pending.Phase {
			case "applying", "activated", "draining", "error":
			default:
				err = errors.New("invalid traffic transition phase")
			}
		}
	}
	if err != nil {
		m.loadError = err
	}
	return err
}

func (m *trafficControl) validateSelection(s trafficSelection) error {
	if len(s.Sites) > 4096 {
		return errors.New("traffic site preference capacity reached")
	}
	active := 0
	for id, p := range s.Sites {
		if id <= 0 {
			return errors.New("invalid traffic site identity")
		}
		if p.Source != nil {
			v := p.Source
			if v.SiteID != id || !validTrafficGeneration(v.Generation) || v.Path != m.sourcePath(id, v.Generation) {
				return errors.New("invalid traffic source identity")
			}
		}
		if s.MasterEnabled && p.Enabled {
			active++
		}
	}
	if active > maxTrafficSites {
		return fmt.Errorf("traffic collection supports at most %d enabled sites on this server", maxTrafficSites)
	}
	return nil
}

func validTrafficGeneration(g string) bool {
	id, err := uuid.Parse(g)
	return err == nil && id.String() == g
}

func (m *trafficControl) sourcePath(id int64, generation string) string {
	return filepath.Join(m.dataDir, "traffic", "logs", strconv.FormatInt(id, 10), generation+".jsonl")
}

func (m *trafficControl) saveLocked() error {
	dir := filepath.Dir(m.path)
	verified, err := trafficControlDirectory(dir, true)
	if err != nil {
		return err
	}
	defer verified.Close()
	st, err := verified.Stat()
	if err != nil {
		return err
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.IsDir() || st.Mode().Perm()&0077 != 0 || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("traffic directory must be private and owned by the panel")
	}
	raw, err := json.Marshal(m.state)
	if err != nil {
		return err
	}
	if len(raw) > 512<<10 {
		return errors.New("traffic configuration exceeds its size limit")
	}
	f, err := os.CreateTemp(dir, ".control-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, m.path)
	}
	if err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (m *trafficControl) ensureCollectorLocked() error {
	if m.collector != nil {
		return nil
	}
	store, err := traffic.OpenStore(m.dataDir)
	if err != nil {
		return err
	}
	m.store = store
	var trusted []uint32
	if worker, e := user.Lookup("www-data"); e == nil {
		if uid, e := strconv.ParseUint(worker.Uid, 10, 32); e == nil {
			trusted = append(trusted, uint32(uid))
		}
	}
	m.collector = traffic.NewCollector(store, traffic.CollectorOptions{Interval: 30 * time.Second, TrustedLogUIDs: trusted, RotatedFileRetired: m.retired})
	m.notify()
	return nil
}

func (m *trafficControl) notify() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func cloneTrafficSelection(v trafficSelection) trafficSelection {
	x := trafficSelection{MasterEnabled: v.MasterEnabled, Sites: make(map[int64]trafficPreference, len(v.Sites))}
	for id, pref := range v.Sites {
		if pref.Source != nil {
			source := *pref.Source
			pref.Source = &source
		}
		x.Sites[id] = pref
	}
	return x
}

func trafficSources(v trafficSelection) []traffic.Source {
	sources := make([]traffic.Source, 0)
	if v.MasterEnabled {
		for _, p := range v.Sites {
			if p.Enabled && p.Source != nil {
				sources = append(sources, *p.Source)
			}
		}
	}
	return sources
}

func (m *trafficControl) change(ctx context.Context, siteID int64, enabled bool) (traffic.State, error) {
	return m.requestChange(ctx, siteID, enabled, true)
}

func (m *trafficControl) requestChange(ctx context.Context, siteID int64, enabled, applyNow bool) (traffic.State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.loadLocked(); err != nil {
		return traffic.State{}, err
	}
	if m.state.Pending != nil && m.state.Pending.Phase != "error" {
		return m.statusLocked(ctx), errTrafficPending
	}
	sites, err := m.listSites(ctx)
	if err != nil {
		return traffic.State{}, err
	}
	known := make(map[int64]trafficSiteInfo, len(sites))
	for _, site := range sites {
		known[site.ID] = site
	}
	if siteID != 0 {
		if _, ok := known[siteID]; !ok {
			return traffic.State{}, errTrafficSiteMissing
		}
	}
	if m.state.Pending == nil && m.state.Error == "" && m.state.Notice == "" {
		unchanged := siteID == 0 && m.state.MasterEnabled == enabled || siteID != 0 && m.state.Sites[siteID].Enabled == enabled
		if unchanged {
			return m.statusLocked(ctx), nil
		}
	}
	target := cloneTrafficSelection(m.state.trafficSelection)
	if siteID == 0 {
		target.MasterEnabled = enabled
	} else {
		p := target.Sites[siteID]
		p.Enabled = enabled
		target.Sites[siteID] = p
	}
	for id, p := range target.Sites {
		if _, exists := known[id]; !exists {
			p.Enabled = false
			p.Source = nil
		}
		if !target.MasterEnabled || !p.Enabled {
			p.Source = nil
		} else if p.Source == nil {
			g := uuid.NewString()
			p.Source = &traffic.Source{SiteID: id, Generation: g, Path: m.sourcePath(id, g)}
		}
		target.Sites[id] = p
	}
	if err := m.validateSelection(target); err != nil {
		return m.statusLocked(ctx), err
	}
	if err := m.beginTargetLocked(target, ""); err != nil {
		return m.statusLocked(ctx), err
	}
	if !applyNow {
		m.notify()
		return m.statusLocked(ctx), nil
	}
	// The durable intent belongs to the control loop, not the HTTP request.
	// Cancellation must not abandon an already-applied configuration.
	work, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	err = m.applyLocked(work, sites)
	m.notify()
	return m.statusLocked(ctx), err
}

func (m *trafficControl) beginTargetLocked(target trafficSelection, notice string) error {
	previous := m.state
	retired := make([]traffic.Source, 0)
	for _, source := range trafficSources(previous.trafficSelection) {
		next := target.Sites[source.SiteID].Source
		if next == nil || next.Generation != source.Generation {
			retired = append(retired, source)
		}
	}
	m.state.Pending = &trafficTransition{Target: target, Phase: "applying", Retired: retired}
	m.state.Error, m.state.Notice = "", notice
	if err := m.saveLocked(); err != nil {
		m.state = previous
		return err
	}
	return nil
}

func (m *trafficControl) applyLocked(ctx context.Context, sites []trafficSiteInfo) error {
	p := m.state.Pending
	if p == nil {
		return nil
	}
	if p.Phase == "draining" {
		if err := m.ensureCollectorLocked(); err != nil {
			return err
		}
		if len(trafficSources(p.Target)) > 0 {
			if err := m.pruneAPISettingsLocked(ctx, sites); err != nil {
				return err
			}
		}
		if err := m.collector.SetSourcesAt(ctx, trafficSources(p.Target), p.AppliedAt); err != nil {
			return err
		}
		drained, err := m.drained(ctx, p.Workers)
		if err != nil {
			m.state.Error = err.Error()
			_ = m.saveLocked()
			return err
		}
		if !drained {
			return nil
		}
		for _, source := range p.Retired {
			if err := m.removeRetiredTrafficLog(source); err != nil {
				m.state.Error = "Collection stopped, but an old analytics log could not be removed"
				_ = m.saveLocked()
				return err
			}
		}
		m.state.Pending = nil
		m.state.Error = ""
		return m.saveLocked()
	}
	release, err := m.admit(ctx)
	if err != nil {
		return err
	}
	defer release()
	// Open existing storage before activating an ON producer. Corrupt storage
	// must not start a log that has no admitted consumer. OFF can still stop the
	// producer even while its analytics bookkeeping requires repair.
	if err := m.ensureCollectorLocked(); err != nil && len(trafficSources(p.Target)) > 0 {
		p.Phase = "error"
		m.state.Error = err.Error()
		_ = m.saveLocked()
		return err
	}
	if len(trafficSources(p.Target)) > 0 {
		if err := m.pruneAPISettingsLocked(ctx, sites); err != nil {
			return err
		}
	}
	index := make(map[int64]trafficSiteInfo, len(sites))
	for _, site := range sites {
		index[site.ID] = site
	}
	active := make([]nginx.TrafficSite, 0)
	for _, source := range trafficSources(p.Target) {
		site, ok := index[source.SiteID]
		if !ok {
			return errors.New("traffic site changed during configuration; retry the selection")
		}
		active = append(active, nginx.TrafficSite{ID: source.SiteID, ConfigName: site.ConfigName, Domain: site.Domain, Generation: source.Generation, LogPath: source.Path})
	}
	if p.Phase != "activated" {
		for _, source := range trafficSources(p.Target) {
			dir, err := trafficControlDirectory(filepath.Dir(source.Path), true)
			if err != nil {
				return err
			}
			dir.Close()
		}
		result, err := m.apply(ctx, active)
		if err != nil {
			p.Phase = "error"
			m.state.Error = err.Error()
			_ = m.saveLocked()
			return err
		}
		for id, pref := range p.Target.Sites {
			if pref.Source != nil && pref.Source.StartTime.IsZero() {
				pref.Source.StartTime = result.AppliedAt
				p.Target.Sites[id] = pref
			}
		}
		p.AppliedAt, p.Workers, p.Phase = result.AppliedAt, result.RetiringWorkers, "activated"
		if err := m.saveLocked(); err != nil {
			return err
		}
	}
	if err := m.ensureCollectorLocked(); err != nil {
		if len(trafficSources(p.Target)) == 0 {
			m.state.trafficSelection = p.Target
			m.state.Error = err.Error()
			_ = m.saveLocked()
		}
		return err
	}
	if err := m.collector.SetSourcesAt(ctx, trafficSources(p.Target), p.AppliedAt); err != nil {
		return m.stopUncollectableLocked(ctx, sites, err)
	}
	m.state.trafficSelection = p.Target
	p.Phase = "draining"
	if err := m.saveLocked(); err != nil {
		return err
	}
	return m.applyLocked(ctx, sites)
}

// If ingestion admission fails after Nginx has activated a new producer, stop
// production rather than leave an unbounded log with no working reader. The
// ordinary Enable switch can be used again after history/storage is repaired.
func (m *trafficControl) stopUncollectableLocked(ctx context.Context, sites []trafficSiteInfo, cause error) error {
	p := m.state.Pending
	if p == nil {
		return cause
	}
	if len(trafficSources(p.Target)) == 0 && (p.Phase == "activated" || p.Phase == "draining") && !p.AppliedAt.IsZero() {
		m.state.trafficSelection = p.Target
		if m.collector != nil {
			m.collector.Suspend()
		}
		m.state.Error = cause.Error()
		_ = m.saveLocked()
		return cause
	}
	retired := append([]traffic.Source{}, p.Retired...)
	retired = append(retired, trafficSources(p.Target)...)
	retired = append(retired, trafficSources(m.state.trafficSelection)...)
	target := cloneTrafficSelection(p.Target)
	target.MasterEnabled = false
	for id, pref := range target.Sites {
		pref.Source = nil
		target.Sites[id] = pref
	}
	m.state.Pending = &trafficTransition{Target: target, Phase: "applying", Retired: retired}
	m.state.Notice = "Collection paused because analytics storage could not accept the change. Delete analytics history or review storage before enabling collection again."
	m.state.Error = cause.Error()
	if err := m.saveLocked(); err != nil {
		return fmt.Errorf("analytics admission failed (%v); could not save stop intent: %w", cause, err)
	}
	result, err := m.apply(ctx, nil)
	if err != nil {
		return fmt.Errorf("analytics admission failed (%v); stopping its producer is pending: %w", cause, err)
	}
	m.state.Pending.Phase, m.state.Pending.AppliedAt, m.state.Pending.Workers = "activated", result.AppliedAt, result.RetiringWorkers
	m.state.trafficSelection = target
	if m.collector != nil {
		m.collector.Suspend()
	}
	if err := m.saveLocked(); err != nil {
		return err
	}
	// Remain activated if this final bookkeeping write cannot fit. New log
	// ingestion is suspended, and replay closes the interval at AppliedAt.
	if m.collector == nil {
		return cause
	}
	if err := m.collector.SetSourcesAt(ctx, nil, result.AppliedAt); err != nil {
		return cause
	}
	m.state.Pending.Phase = "draining"
	if err := m.saveLocked(); err != nil {
		return err
	}
	return cause
}

func (m *trafficControl) statusLocked(ctx context.Context) traffic.State {
	result := traffic.State{MasterEnabled: m.state.MasterEnabled, RetentionDays: 30, FreshnessSeconds: 30, Status: "disabled", Error: m.state.Error, Sites: []traffic.SiteStatus{}}
	if result.Error == "" {
		result.Error = m.state.Notice
	}
	selection := m.state.trafficSelection
	if m.state.Pending != nil && m.state.Pending.Phase != "error" {
		result.Pending = true
		result.Status = m.state.Pending.Phase
	}
	if !result.Pending && result.MasterEnabled {
		result.Status = "enabled"
	}
	if m.loadError != nil {
		result.Status = "error"
		result.Error = m.loadError.Error()
	}
	sites, err := m.listSites(ctx)
	if err != nil {
		result.Error = "Traffic site list is unavailable"
		result.Status = "error"
		return result
	}
	for _, site := range sites {
		pref := selection.Sites[site.ID]
		status := traffic.SiteStatus{SiteID: site.ID, Domain: site.Domain, Enabled: pref.Enabled, EffectiveEnabled: selection.MasterEnabled && pref.Enabled && pref.Source != nil, Status: "disabled", Pending: result.Pending, Error: result.Error}
		if m.collector != nil {
			stats := m.collector.Stats(site.ID)
			status.LastProcessedAt, status.LastCheckedAt = stats.LastProcessedAt, stats.LastCheckedAt
			status.BacklogBytes, status.Incomplete, status.DroppedBytes = stats.BacklogBytes, stats.Incomplete, stats.DroppedBytes
			if stats.Error != "" {
				status.Error = stats.Error
			}
		}
		if status.EffectiveEnabled {
			status.Status = "enabled"
		}
		if pref.Enabled && !selection.MasterEnabled {
			status.Status = "disabled_by_master"
		}
		if result.Pending {
			status.Status = result.Status
		} else if status.Error != "" {
			status.Status = "error"
		}
		result.Sites = append(result.Sites, status)
	}
	return result
}

func (m *trafficControl) Status(ctx context.Context) (traffic.State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.loadLocked(); err != nil {
		return m.statusLocked(ctx), err
	}
	return m.statusLocked(ctx), nil
}

func (m *trafficControl) Report(ctx context.Context, id int64, period traffic.Range) (traffic.Report, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.loadLocked(); err != nil {
		return traffic.Report{}, err
	}
	if err := m.ensureCollectorLocked(); err != nil {
		return traffic.Report{}, err
	}
	r, err := m.store.Query(ctx, id, period, m.now())
	if err != nil {
		return r, err
	}
	state := m.statusLocked(ctx)
	r.MasterEnabled = state.MasterEnabled
	for _, site := range state.Sites {
		if site.SiteID == id {
			if !site.EffectiveEnabled {
				site.LastProcessedAt, site.LastCheckedAt = r.Site.LastProcessedAt, r.Site.LastCheckedAt
				site.Incomplete, site.DroppedBytes = r.Site.Incomplete, r.Site.DroppedBytes
			}
			r.Site = site
			break
		}
	}
	return r, nil
}

func (m *trafficControl) DeleteHistory(ctx context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.loadLocked(); err != nil {
		return err
	}
	if m.state.Pending != nil && m.state.Pending.Phase != "error" && !(m.state.Pending.Phase == "activated" && len(trafficSources(m.state.Pending.Target)) == 0) {
		return errors.New("wait for the collection change before deleting traffic history")
	}
	if err := m.ensureCollectorLocked(); err != nil {
		return err
	}
	return m.collector.DeleteHistory(ctx, id)
}

func (m *trafficControl) Start(ctx context.Context) {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	if err := m.loadLocked(); err != nil {
		m.mu.Unlock()
		log.Printf("Traffic analytics configuration unavailable: %v", err)
		return
	}
	if err := m.ensureCollectorLocked(); err != nil {
		log.Printf("Traffic analytics unavailable: %v", err)
		m.state.Error = err.Error()
		m.started = true
		m.mu.Unlock()
		go m.storageFailureLoop(ctx, err)
		return
	}
	m.started = true
	m.mu.Unlock()
	m.launchLoops(ctx)
}

func (m *trafficControl) launchLoops(ctx context.Context) {
	collector := m.collector
	collectorDone := make(chan struct{})
	go func() {
		defer close(collectorDone)
		if err := collector.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("Traffic analytics collector stopped: %v", err)
		}
	}()
	go func() {
		m.controlLoop(ctx)
		<-collectorDone
		_ = m.store.Close()
		close(m.done)
	}()
}

// Storage can fail independently of the producer. Keep a controller alive to
// retire a previously enabled producer, including when lifecycle admission or
// Nginx reload is temporarily busy. Once OFF drains it sleeps without polling
// logs; an API wake can hand off to a repaired store and the ordinary worker.
func (m *trafficControl) storageFailureLoop(ctx context.Context, cause error) {
	handedOff := false
	defer func() {
		if !handedOff {
			close(m.done)
		}
	}()
	for {
		m.mu.Lock()
		if m.collector != nil {
			m.mu.Unlock()
			handedOff = true
			m.launchLoops(ctx)
			return
		}
		work, cancel := context.WithTimeout(ctx, 5*time.Second)
		needsStop := len(trafficSources(m.state.trafficSelection)) > 0 || m.state.Pending != nil
		if needsStop {
			if m.state.Pending == nil {
				m.state.Pending = &trafficTransition{Target: cloneTrafficSelection(m.state.trafficSelection), Phase: "applying"}
			}
			p := m.state.Pending
			if len(trafficSources(p.Target)) == 0 && (p.Phase == "activated" || p.Phase == "draining") && !p.AppliedAt.IsZero() {
				if p.Phase == "draining" {
					needsStop = false
				} else if drained, err := m.drained(work, p.Workers); err == nil && drained {
					// Keep AppliedAt and old logs until SQLite can durably close
					// the interval. Losing this timestamp would turn a disabled
					// period into apparent zero traffic after storage repair.
					p.Phase = "draining"
					m.state.Error = cause.Error()
					_ = m.saveLocked()
					needsStop = false
				}
			} else if release, err := m.admit(work); err == nil {
				_ = m.stopUncollectableLocked(work, nil, cause)
				release()
			}
		}
		cancel()
		m.mu.Unlock()
		wait := time.Hour
		if needsStop {
			wait = time.Second
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-m.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (m *trafficControl) controlLoop(ctx context.Context) {
	first := true
	for {
		m.mu.Lock()
		active := len(trafficSources(m.state.trafficSelection)) != 0
		pending := m.state.Pending != nil && m.state.Pending.Phase != "error"
		if first && (active || pending) {
			if m.state.Pending == nil {
				m.state.Pending = &trafficTransition{Target: cloneTrafficSelection(m.state.trafficSelection), Phase: "applying"}
				_ = m.saveLocked()
			}
			if m.state.Pending != nil && (m.state.Pending.Phase == "draining" || m.state.Pending.Phase == "error") {
				at := m.now()
				if m.state.Pending.Phase == "draining" {
					at = m.state.Pending.AppliedAt
				}
				if err := m.pruneAPISettingsLocked(ctx, nil); err != nil {
					m.state.Error = err.Error()
				} else if err := m.collector.SetSourcesAt(ctx, trafficSources(m.state.trafficSelection), at); err != nil {
					m.state.Error = err.Error()
				}
			}
		}
		if first && !active && !pending {
			_ = m.collector.SetSources(ctx, nil)
		}
		if m.state.Pending != nil && m.state.Pending.Phase != "error" {
			work, cancel := context.WithTimeout(ctx, 15*time.Second)
			sites, err := m.listSites(work)
			if err == nil {
				err = m.applyLocked(work, sites)
			}
			if err != nil {
				m.state.Error = err.Error()
			}
			cancel()
		}
		first = false
		if (m.state.Pending == nil || m.state.Pending.Phase == "error") && active {
			work, cancel := context.WithTimeout(ctx, 10*time.Second)
			if err := m.superviseLocked(work); err != nil && ctx.Err() == nil {
				log.Printf("Traffic analytics supervision: %v", err)
			}
			cancel()
		}
		active = len(trafficSources(m.state.trafficSelection)) != 0
		pending = m.state.Pending != nil && m.state.Pending.Phase != "error"
		m.mu.Unlock()
		var tick <-chan time.Time
		var timer *time.Timer
		if active || pending {
			timer = time.NewTimer(time.Second)
			tick = timer.C
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-m.wake:
			if timer != nil {
				timer.Stop()
			}
		case <-tick:
		}
	}
}
