package traffic

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	MaxBatchBytes   = 256 << 10
	MaxBatchLines   = 2048
	MaxBacklogBytes = 8 << 20
	MaxSitesPerPass = 128
	MaxPassBytes    = 4 << 20
	MaxPassLines    = 32768
)

type CollectorOptions struct {
	Interval       time.Duration
	TrustedLogUIDs []uint32
	// Called under the collector lock; must not call Collector methods. A nil
	// callback cannot establish retirement and conservatively waits.
	RotatedFileRetired func(context.Context, string) (bool, error)
	// Smaller limits are useful to constrain resources further and in acceptance tests.
	MaxBytesPerSite int
	MaxLinesPerSite int
	MaxSitesPerPass int
	MaxBacklogBytes int64
}

type sourceState struct {
	source             Source
	cp                 checkpoint
	file               *os.File
	checked            time.Time
	failure            string
	awaitingRetirement bool
	apiRules           []apiRule
	apiRulesLoaded     bool
}

// Collector serializes controls, rotation callbacks, and bounded incremental
// batches. It has one worker for all sites and never opens a log when disabled.
type Collector struct {
	store           *Store
	opts            CollectorOptions
	mu              sync.Mutex
	sources         map[int64]*sourceState
	suspended       map[int64]checkpoint
	wake            chan struct{}
	cursor          int
	lastMaintenance time.Time
}

func NewCollector(store *Store, opts CollectorOptions) *Collector {
	if opts.Interval <= 0 {
		opts.Interval = 30 * time.Second
	}
	if opts.MaxBytesPerSite <= 0 || opts.MaxBytesPerSite > MaxBatchBytes {
		opts.MaxBytesPerSite = MaxBatchBytes
	}
	if opts.MaxBytesPerSite < MaxLineBytes+1 {
		opts.MaxBytesPerSite = MaxLineBytes + 1
	}
	if opts.MaxLinesPerSite <= 0 || opts.MaxLinesPerSite > MaxBatchLines {
		opts.MaxLinesPerSite = MaxBatchLines
	}
	if opts.MaxSitesPerPass <= 0 || opts.MaxSitesPerPass > MaxSitesPerPass {
		opts.MaxSitesPerPass = MaxSitesPerPass
	}
	if opts.MaxBacklogBytes <= 0 || opts.MaxBacklogBytes > MaxBacklogBytes {
		opts.MaxBacklogBytes = MaxBacklogBytes
	}
	return &Collector{store: store, opts: opts, sources: map[int64]*sourceState{}, wake: make(chan struct{}, 1)}
}

func validSource(s Source) bool {
	if s.SiteID <= 0 || len(s.Generation) < 1 || len(s.Generation) > 64 || s.StartTime.IsZero() || !filepath.IsAbs(s.Path) || filepath.Clean(s.Path) != s.Path || len(s.Path) > 4096 {
		return false
	}
	for _, c := range s.Generation {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// SetSources applies a complete effective-source snapshot at the current time.
func (c *Collector) SetSources(ctx context.Context, sources []Source) error {
	return c.SetSourcesAt(ctx, sources, time.Now().UTC())
}

// SetSourcesAt closes removed/replaced intervals at the producer's successful
// activation time, including when a persisted transition is retried after restart.
// The database snapshot and active in-memory map change only after one commit.
func (c *Collector) SetSourcesAt(ctx context.Context, sources []Source, effectiveTime time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if effectiveTime.IsZero() {
		return errors.New("traffic effective time is required")
	}
	if len(sources) > MaxActiveSources {
		return errors.New("traffic active-source capacity reached")
	}
	desired := make(map[int64]Source, len(sources))
	for _, src := range sources {
		if !validSource(src) {
			return errors.New("invalid traffic source")
		}
		if _, ok := desired[src.SiteID]; ok {
			return errors.New("duplicate traffic source")
		}
		desired[src.SiteID] = src
	}
	stopped := make(map[int64]checkpoint, len(c.suspended)+len(c.sources))
	for id, cp := range c.suspended {
		stopped[id] = cp
	}
	for id, state := range c.sources {
		src, ok := desired[id]
		if ok && sameSource(src, state.source) {
			continue
		}
		stopped[id] = c.stoppedCheckpoint(state)
	}
	checkpoints, err := c.store.reconcile(ctx, desired, stopped, effectiveTime.UTC().Unix())
	if err != nil {
		return err
	}
	next := make(map[int64]*sourceState, len(desired))
	for id, src := range desired {
		if old := c.sources[id]; old != nil && sameSource(src, old.source) {
			next[id] = old
		} else {
			next[id] = &sourceState{source: src, cp: checkpoints[id]}
		}
	}
	for id, old := range c.sources {
		if next[id] != old {
			if old.file != nil {
				old.file.Close()
			}
			c.store.setLive(id, nil)
		}
	}
	for id := range c.suspended {
		c.store.setLive(id, nil)
	}
	c.sources = next
	c.suspended = nil
	c.cursor = 0
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return nil
}
func sameSource(a, b Source) bool {
	return a.SiteID == b.SiteID && a.Generation == b.Generation && a.Path == b.Path && a.StartTime.Equal(b.StartTime)
}

// Suspend immediately stops ingestion after producer OFF, even when storage
// cannot commit a durable stop. It performs no filesystem reads or database writes.
// The caller retries SetSourcesAt with the producer's original activation time.
func (c *Collector) Suspend() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.suspended == nil {
		c.suspended = map[int64]checkpoint{}
	}
	for id, state := range c.sources {
		cp := state.cp
		cp.capped = true
		cp.issue = "Collection was suspended before a durable stop could be recorded"
		c.suspended[id] = cp
		if state.file != nil {
			state.file.Close()
		}
		c.store.setLive(id, nil)
	}
	c.sources = map[int64]*sourceState{}
	c.cursor = 0
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Collector) Run(ctx context.Context) error {
	defer c.closeFiles()
	for {
		c.mu.Lock()
		active := len(c.sources) > 0
		maintenanceDue := c.lastMaintenance.IsZero() || time.Since(c.lastMaintenance) >= time.Hour
		c.mu.Unlock()
		if maintenanceDue {
			if err := c.store.Prune(ctx, time.Now()); err != nil && ctx.Err() != nil {
				return ctx.Err()
			}
			c.mu.Lock()
			c.lastMaintenance = time.Now()
			c.mu.Unlock()
		}
		wait := time.Hour
		if active {
			wait = c.opts.Interval
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-c.wake:
			timer.Stop()
		case <-timer.C:
		}
		if err := c.CollectOnce(ctx, time.Now().UTC()); err != nil && ctx.Err() != nil {
			return nil
		}
	}
}
func (c *Collector) closeFiles() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.sources {
		if s.file != nil {
			s.file.Close()
			s.file = nil
		}
	}
}

// CollectOnce caps work per source and rotates the starting site on every pass.
func (c *Collector) CollectOnce(ctx context.Context, now time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]int64, 0, len(c.sources))
	for id := range c.sources {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) == 0 {
		return nil
	}
	start := c.cursor % len(ids)
	count := min(len(ids), c.opts.MaxSitesPerPass)
	byteBudget := min(c.opts.MaxBytesPerSite, MaxPassBytes/count)
	lineBudget := min(c.opts.MaxLinesPerSite, MaxPassLines/count)
	passCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	began := time.Now()
	var first error
	for i := 0; i < count; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if i > 0 && time.Since(began) > time.Second {
			count = i
			break
		}
		s := c.sources[ids[(start+i)%len(ids)]]
		s.checked = now
		if err := c.collect(passCtx, s, now, byteBudget, lineBudget); err != nil {
			s.failure = "Analytics collection is delayed: " + safeIssue(err)
			if first == nil {
				first = err
			}
		} else {
			s.failure = ""
		}
		c.store.setLive(s.source.SiteID, &liveStatus{cp: s.cp, checked: s.checked, failure: s.failure})
	}
	c.cursor = (start + count) % len(ids)
	return first
}
func safeIssue(err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return "dedicated log is not available"
	}
	if errors.Is(err, os.ErrPermission) {
		return "dedicated log permission denied"
	}
	v := err.Error()
	if i := strings.Index(v, ": /"); i >= 0 {
		v = v[:i]
	}
	if len(v) > 160 {
		v = v[:160]
	}
	return v
}

func (c *Collector) open(name string) (*os.File, unix.Stat_t, error) {
	return openTrafficFileTrusted(name, c.opts.TrustedLogUIDs)
}

func (c *Collector) collect(ctx context.Context, s *sourceState, now time.Time, maxBytes, maxLines int) error {
	current, st, err := c.open(s.source.Path)
	if err != nil {
		return err
	}
	defer func() {
		if current != nil {
			current.Close()
		}
	}()
	cp := s.cp
	s.awaitingRetirement = false
	if s.file == nil {
		// Resume the exact old inode after rename/reopen, never scan a directory.
		if cp.ino != 0 && (uint64(st.Ino) != cp.ino || uint64(st.Dev) != cp.dev) {
			old, oldSt, e := c.open(s.source.Path + ".1")
			if e == nil && uint64(oldSt.Ino) == cp.ino && uint64(oldSt.Dev) == cp.dev {
				s.file = old
			} else {
				if old != nil {
					old.Close()
				}
				cp.issue = "A rotated log interval was lost before collection"
				cp.capped = true
				cp.dev = uint64(st.Dev)
				cp.ino = uint64(st.Ino)
				cp.offset = 0
				cp.skipping = false
			}
		}
		if s.file == nil {
			s.file = current
			current = nil
		}
	}
	var held unix.Stat_t
	if err = unix.Fstat(int(s.file.Fd()), &held); err != nil {
		return err
	}
	if cp.ino == 0 {
		cp.dev = uint64(held.Dev)
		cp.ino = uint64(held.Ino)
	}
	if uint64(held.Dev) != cp.dev || uint64(held.Ino) != cp.ino {
		return errors.New("traffic inode cursor mismatch")
	}
	markerChanged := false
	if cp.offset > 0 && cp.marker != "" && held.Size >= cp.offset {
		got, e := cursorMarker(s.file, cp.offset)
		if e != nil {
			return e
		}
		markerChanged = got != cp.marker
	}
	if held.Size < cp.offset || markerChanged {
		cp.offset = 0
		cp.skipping = false
		cp.capped = true
		cp.issue = "A log was truncated or rewritten; this interval may be incomplete"
		cp.marker = ""
	}
	// A bounded suffix is retained under sustained overload. Advancing is durable
	// with its explicit dropped-byte count, so restart cannot resurrect that gap.
	if held.Size-cp.offset > c.opts.MaxBacklogBytes {
		next := held.Size - c.opts.MaxBacklogBytes
		cp.dropped += next - cp.offset
		cp.offset = next
		cp.skipping = true
		cp.capped = true
		cp.issue = "Traffic exceeded the collection backlog budget"
	}
	if held.Size == cp.offset {
		if current != nil && (uint64(st.Ino) != cp.ino || uint64(st.Dev) != cp.dev) {
			retired, e := c.rotatedFileRetired(ctx, s.source.Path+".1", cp.dev, cp.ino)
			if e != nil {
				return e
			}
			// A worker can append between the initial EOF stat and the proof.
			// Re-stat after proof before releasing the old descriptor.
			var final unix.Stat_t
			if e = unix.Fstat(int(s.file.Fd()), &final); e != nil {
				return e
			}
			if !retired || final.Size != held.Size {
				s.awaitingRetirement = true
				cp.backlog = max(int64(0), final.Size-cp.offset) + st.Size
				s.cp = cp
				return nil
			}
			s.file.Close()
			s.file = current
			current = nil
			cp.dev = uint64(st.Dev)
			cp.ino = uint64(st.Ino)
			cp.offset = 0
			cp.skipping = false
			held = st
		} else {
			cp.backlog = 0
			if cp.ino != s.cp.ino || cp.dev != s.cp.dev || cp.offset != s.cp.offset || cp.observed == 0 {
				cp.observed = now.Unix()
				if err = c.store.apply(ctx, &batch{cp: cp, hours: map[int64]*aggregate{}}); err != nil {
					return err
				}
			}
			s.cp = cp
			return nil
		}
	}
	buf := make([]byte, min(int64(maxBytes), max(int64(0), held.Size-cp.offset)))
	n, err := s.file.ReadAt(buf, cp.offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	buf = buf[:n]
	if !s.apiRulesLoaded {
		rules, e := c.store.loadAPIRules(ctx, s.source.SiteID)
		if e != nil {
			return e
		}
		s.apiRules, s.apiRulesLoaded = rules, true
	}
	retentionStart := c.store.retentionStart(now)
	b := batch{cp: cp, hours: map[int64]*aggregate{}}
	position := 0
	lines := 0
	for position < len(buf) && lines < maxLines {
		rel := bytes.IndexByte(buf[position:], '\n')
		if rel < 0 {
			if b.cp.skipping || len(buf)-position > MaxLineBytes {
				if !b.cp.skipping {
					b.cp.invalid++
				}
				b.cp.skipping = true
				b.cp.dropped += int64(len(buf) - position)
				b.cp.capped = true
				b.cp.offset += int64(len(buf) - position)
			}
			break
		}
		line := buf[position : position+rel]
		consumed := rel + 1
		position += consumed
		lines++
		b.cp.offset += int64(consumed)
		if b.cp.skipping {
			b.cp.skipping = false
			b.cp.dropped += int64(consumed)
			continue
		}
		start := s.source.StartTime
		if b.cp.cutoff > float64(start.UnixNano())/1e9 {
			start = time.Unix(0, int64(b.cp.cutoff*1e9))
		}
		start = maxTime(start, retentionStart)
		r, e := parseRecord(line, s.source.Generation, start, now)
		if errors.Is(e, errOutsideWindow) {
			continue
		}
		if e != nil {
			b.cp.invalid++
			b.cp.capped = true
			continue
		}
		a := b.hours[r.hour]
		if a == nil {
			a = &aggregate{pages: map[string]int64{}, referrers: map[string]int64{}}
			b.hours[r.hour] = a
		}
		if classifier := classifierAt(s.apiRules, r.Timestamp); classifier != nil {
			endpoint, api := classifier.classify(r.Path)
			r.pageview = estimatePageView(r, api)
			if api {
				if a.api == nil {
					a.api = &apiAggregate{endpoints: map[apiKey]*apiCounters{}}
				}
				a.api.addEndpoint(r, endpoint)
			}
		}
		a.requests++
		a.bytes += r.Bytes
		a.timeMS += r.RequestTime * 1000
		if r.Status == 404 {
			a.notFound++
		} else if r.Status >= 400 && r.Status < 500 {
			a.other4xx++
		} else if r.Status >= 500 {
			a.serverErrors++
		}
		if r.pageview {
			a.pageviews++
			addDimension(a.pages, r.Path, MaxPageDimensionsPerHour, &a.capped)
		}
		if r.Referrer != "" {
			addDimension(a.referrers, r.Referrer, MaxReferrerDimensionsPerHour, &a.capped)
		}
		if r.Timestamp > b.cp.lastTS {
			b.cp.lastTS = r.Timestamp
		}
	}
	// EOF is provisional while a retiring Nginx worker may still append. A
	// fragment is discarded only after the producer proves the old inode retired.
	rotating := current != nil && (uint64(st.Ino) != cp.ino || uint64(st.Dev) != cp.dev)
	if rotating && int64(n)+cp.offset >= held.Size && b.cp.offset < held.Size && lines < maxLines {
		retired, e := c.rotatedFileRetired(ctx, s.source.Path+".1", cp.dev, cp.ino)
		if e != nil {
			return e
		}
		if retired {
			var final unix.Stat_t
			if e = unix.Fstat(int(s.file.Fd()), &final); e != nil {
				return e
			}
			if final.Size != held.Size {
				held = final
				retired = false
			}
		}
		if retired {
			b.cp.dropped += held.Size - b.cp.offset
			b.cp.offset = held.Size
			b.cp.invalid++
			b.cp.capped = true
			b.cp.skipping = false
			b.cp.issue = "An incomplete final frame was discarded after writer retirement"
		} else {
			s.awaitingRetirement = true
		}
	}
	b.cp.backlog = max(int64(0), held.Size-b.cp.offset)
	if rotating {
		s.awaitingRetirement = true
		b.cp.backlog += st.Size
	}
	b.cp.observed = now.Unix()
	if b.cp.offset == s.cp.offset && b.cp.dev == s.cp.dev && b.cp.ino == s.cp.ino && b.cp.invalid == s.cp.invalid && b.cp.dropped == s.cp.dropped {
		s.cp.backlog = b.cp.backlog
		return nil
	}
	if b.cp.offset > 0 {
		b.cp.marker, err = cursorMarker(s.file, b.cp.offset)
		if err != nil {
			return err
		}
	}
	if err = c.store.apply(ctx, &b); err != nil {
		return err
	}
	s.cp = b.cp
	return nil
}

func addDimension(values map[string]int64, value string, limit int, capped *bool) {
	if _, ok := values[value]; ok {
		values[value]++
		return
	}
	if len(values) >= limit {
		values[overflowValue]++
		*capped = true
		return
	}
	values[value] = 1
}

func (c *Collector) Stats(siteID int64) SiteStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	status := SiteStatus{SiteID: siteID, Status: "disabled"}
	s := c.sources[siteID]
	if s == nil {
		return status
	}
	status.Enabled = true
	status.EffectiveEnabled = true
	status.Status = "collecting"
	if s.checked.IsZero() || time.Since(s.checked) > c.opts.Interval*2 {
		status.Status = "delayed"
	}
	status.BacklogBytes = s.cp.backlog
	status.DroppedBytes = s.cp.dropped
	status.Incomplete = s.cp.invalid > 0 || s.cp.dropped > 0 || s.cp.capped || s.cp.issue != ""
	if !s.checked.IsZero() {
		v := s.checked
		status.LastCheckedAt = &v
	}
	if s.cp.observed > 0 {
		v := time.Unix(s.cp.observed, 0).UTC()
		status.LastProcessedAt = &v
	}
	if s.cp.backlog > 0 || s.awaitingRetirement {
		status.Status = "delayed"
	}
	if status.Incomplete {
		status.Status = "incomplete"
	}
	if s.awaitingRetirement {
		status.Status = "delayed"
	}
	if s.failure != "" {
		status.Status = "delayed"
		status.Error = s.failure
		status.Incomplete = true
	}
	return status
}

func (c *Collector) rotationReadyLocked(ctx context.Context, src Source) bool {
	s := c.sources[src.SiteID]
	if s == nil || s.source.Generation != src.Generation || s.source.Path != src.Path {
		return false
	}
	old, st, e := c.open(src.Path + ".1")
	if errors.Is(e, os.ErrNotExist) {
		return true
	}
	if e != nil {
		return false
	}
	defer old.Close()
	// Existing .1 is safe to replace only when its exact durable inode has been
	// drained, or the durable cursor has already moved to the current log.
	if uint64(st.Ino) == s.cp.ino && uint64(st.Dev) == s.cp.dev {
		if s.cp.offset < st.Size {
			return false
		}
		retired, e := c.rotatedFileRetired(ctx, src.Path+".1", s.cp.dev, s.cp.ino)
		if e != nil || !retired {
			return false
		}
		final, e := old.Stat()
		return e == nil && s.cp.offset >= final.Size()
	}
	cur, curSt, e := c.open(src.Path)
	if e != nil {
		return false
	}
	defer cur.Close()
	return uint64(curSt.Ino) == s.cp.ino && uint64(curSt.Dev) == s.cp.dev
}
func (c *Collector) RotationReady(src Source) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return c.rotationReadyLocked(ctx, src)
}
func (c *Collector) RotateSource(ctx context.Context, src Source, rotate func(context.Context) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.rotationReadyLocked(ctx, src) {
		return errors.New("traffic rotation is waiting for previous log collection")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return rotate(ctx)
}

// DeleteHistory moves an active source to EOF in the same transaction as deletion.
// New appended requests continue; pre-delete backlog can never recreate history.
func (c *Collector) DeleteHistory(ctx context.Context, siteID int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	s := c.sources[siteID]
	var cp *checkpoint
	if s != nil {
		next := s.cp
		next.cutoff = float64(now.UnixNano()) / 1e9
		next.invalid = 0
		next.dropped = 0
		next.capped = false
		next.issue = ""
		next.lastTS = 0
		next.observed = 0
		next.backlog = 0
		next.skipping = false
		next.marker = ""
		file, st, err := c.open(s.source.Path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if file != nil {
			defer file.Close()
			next.dev = uint64(st.Dev)
			next.ino = uint64(st.Ino)
			next.offset = st.Size
			if st.Size > 0 {
				var last [1]byte
				if _, e := file.ReadAt(last[:], st.Size-1); e != nil {
					return e
				}
				next.skipping = last[0] != '\n'
				next.marker, err = cursorMarker(file, st.Size)
				if err != nil {
					return err
				}
			}
		} else {
			next.dev = 0
			next.ino = 0
			next.offset = 0
		}
		cp = &next
	}
	if err := c.store.deleteHistory(ctx, siteID, cp, now); err != nil {
		return err
	}
	if s != nil {
		if s.file != nil {
			s.file.Close()
			s.file = nil
		}
		s.cp = *cp
		s.apiRulesLoaded = false
		c.store.setLive(siteID, &liveStatus{cp: s.cp, checked: s.checked, failure: s.failure})
	}
	return nil
}

func cursorMarker(f *os.File, offset int64) (string, error) {
	size := min(offset, int64(64))
	buf := make([]byte, size)
	_, err := f.ReadAt(buf, offset-size)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:]), nil
}

// Controls discard pending bytes when an interval is closed. Record that gap
// explicitly rather than reporting unread pre-disable requests as zero traffic.
func (c *Collector) stoppedCheckpoint(s *sourceState) checkpoint {
	cp := s.cp
	pending := int64(0)
	unknown := false
	cur, st, err := c.open(s.source.Path)
	if err != nil {
		unknown = true
	} else {
		defer cur.Close()
		if uint64(st.Dev) == cp.dev && uint64(st.Ino) == cp.ino {
			pending = max(int64(0), st.Size-cp.offset)
		} else {
			pending = st.Size
			if cp.ino != 0 {
				if s.file != nil {
					if held, e := s.file.Stat(); e == nil {
						pending += max(int64(0), held.Size()-cp.offset)
					} else {
						unknown = true
					}
				} else {
					old, oldSt, e := c.open(s.source.Path + ".1")
					if e == nil {
						old.Close()
						if uint64(oldSt.Dev) == cp.dev && uint64(oldSt.Ino) == cp.ino {
							pending += max(int64(0), oldSt.Size-cp.offset)
						} else {
							unknown = true
						}
					} else {
						unknown = true
					}
				}
			}
		}
	}
	if pending > 0 || unknown {
		cp.dropped += pending
		cp.capped = true
		cp.issue = "Collection stopped before all dedicated log bytes could be collected"
	}
	cp.backlog = 0
	return cp
}

// Only ask the producer about the protected exact inode still held by this
// cursor. A missing/replaced .1 cannot prove retirement of an open old inode.
func (c *Collector) rotatedFileRetired(ctx context.Context, path string, dev, ino uint64) (bool, error) {
	old, st, err := c.open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	old.Close()
	if uint64(st.Dev) != dev || uint64(st.Ino) != ino {
		return false, nil
	}
	if c.opts.RotatedFileRetired == nil {
		return false, nil
	}
	return c.opts.RotatedFileRetired(ctx, path)
}
