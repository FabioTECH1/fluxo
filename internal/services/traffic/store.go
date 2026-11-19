package traffic

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
)

const (
	MaxActiveSources             = 128
	MaxRetainedSites             = 256
	MaxPageDimensionsPerHour     = 64
	MaxReferrerDimensionsPerHour = 32
	MaxDimensionRows             = 50000
	MaxDatabaseBytes             = 256 << 20
	retention                    = 30 * 24 * time.Hour
	overflowValue                = "(other)"
)

// Store is deliberately independent of the panel database and its migrations.
// One connection prevents long-lived report readers from pinning an unbounded WAL.
type Store struct {
	db            *sql.DB
	dir           string
	mu            sync.Mutex
	dimensionRows int64
	apiRows       int64
	apiSupportNS  int64
	live          map[int64]liveStatus
}

type checkpoint struct {
	site       int64
	generation string
	path       string
	start      int64
	stop       int64
	dev        uint64
	ino        uint64
	offset     int64
	skipping   bool
	lastTS     float64
	observed   int64
	invalid    int64
	dropped    int64
	capped     bool
	cutoff     float64
	backlog    int64
	issue      string
	marker     string
}

type aggregate struct {
	requests, pageviews, notFound, other4xx, serverErrors, bytes int64
	timeMS                                                       float64
	pages, referrers                                             map[string]int64
	capped                                                       bool
	api                                                          *apiAggregate
}
type batch struct {
	cp    checkpoint
	hours map[int64]*aggregate
}

func OpenStore(dataDir string) (*Store, error) {
	name, err := filepath.Abs(filepath.Join(dataDir, "traffic"))
	if err != nil {
		return nil, err
	}
	s := &Store{dir: name}
	if _, err = os.Lstat(filepath.Join(name, "analytics.db")); errors.Is(err, os.ErrNotExist) {
		return s, nil
	} else if err != nil {
		return nil, err
	}
	if err = s.ensureLocked(); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Store) ensureLocked() error {
	if s.db != nil {
		return nil
	}
	dir, err := protectedDir(s.dir, true)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err = unix.Fchmod(int(dir.Fd()), 0700); err != nil {
		return err
	}
	fd, err := unix.Openat(int(dir.Fd()), "analytics.db", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	var st unix.Stat_t
	err = unix.Fstat(fd, &st)
	unix.Close(fd)
	if err != nil {
		return err
	}
	if st.Nlink != 1 || st.Mode&unix.S_IFMT != unix.S_IFREG || (st.Uid != 0 && int(st.Uid) != os.Geteuid()) || st.Mode&0077 != 0 {
		return errors.New("traffic database is not private")
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		name := filepath.Join(dir.Name(), "analytics.db"+suffix)
		_, e := os.Lstat(name)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return e
		}
		f, sideStat, e := openTrafficFile(name)
		if e != nil {
			return fmt.Errorf("traffic database sidecar is not protected: %w", e)
		}
		f.Close()
		if sideStat.Mode&0077 != 0 {
			return errors.New("traffic database sidecar is not private")
		}
	}
	uri := url.URL{Scheme: "file", Path: filepath.Join(dir.Name(), "analytics.db")}
	params := uri.Query()
	for _, pragma := range []string{"page_size(4096)", "max_page_count(65536)", "journal_mode(WAL)", "synchronous(FULL)", "busy_timeout(3000)", "foreign_keys(ON)", "cache_size(-4096)", "temp_store(MEMORY)", "mmap_size(0)", "journal_size_limit(8388608)", "wal_autocheckpoint(256)"} {
		params.Add("_pragma", pragma)
	}
	uri.RawQuery = params.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	for _, q := range []string{schema, apiSchema} {
		if _, err = db.Exec(q); err != nil {
			db.Close()
			return fmt.Errorf("initialize traffic store: %w", err)
		}
	}
	if _, err = db.Exec("INSERT OR IGNORE INTO api_meta(id,support_ns) VALUES(1,?)", time.Now().UTC().UnixNano()); err != nil {
		db.Close()
		return err
	}
	if err = db.QueryRow("SELECT support_ns FROM api_meta WHERE id=1").Scan(&s.apiSupportNS); err != nil {
		db.Close()
		return err
	}
	if err = db.QueryRow("SELECT count(*) FROM api_endpoints WHERE path<>?", overflowValue).Scan(&s.apiRows); err != nil {
		db.Close()
		return err
	}
	if err = db.QueryRow("SELECT count(*) FROM dimensions WHERE value<>?", overflowValue).Scan(&s.dimensionRows); err != nil {
		db.Close()
		return err
	}
	s.db = db
	return nil
}

const schema = `
CREATE TABLE IF NOT EXISTS sources(site INTEGER PRIMARY KEY,generation TEXT NOT NULL,path TEXT NOT NULL,start INTEGER NOT NULL,stop INTEGER NOT NULL DEFAULT 0,dev INTEGER NOT NULL DEFAULT 0,ino INTEGER NOT NULL DEFAULT 0,offset INTEGER NOT NULL DEFAULT 0,skipping INTEGER NOT NULL DEFAULT 0,last_ts REAL NOT NULL DEFAULT 0,observed INTEGER NOT NULL DEFAULT 0,invalid INTEGER NOT NULL DEFAULT 0,dropped INTEGER NOT NULL DEFAULT 0,capped INTEGER NOT NULL DEFAULT 0,cutoff REAL NOT NULL DEFAULT 0,backlog INTEGER NOT NULL DEFAULT 0,issue TEXT NOT NULL DEFAULT '',marker TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS intervals(site INTEGER NOT NULL,generation TEXT NOT NULL,start INTEGER NOT NULL,stop INTEGER NOT NULL DEFAULT 0,verified INTEGER NOT NULL DEFAULT 0,incomplete INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(site,generation));
CREATE INDEX IF NOT EXISTS intervals_retention ON intervals(stop);
CREATE TABLE IF NOT EXISTS hourly(site INTEGER NOT NULL,hour INTEGER NOT NULL,requests INTEGER NOT NULL,pageviews INTEGER NOT NULL,not_found INTEGER NOT NULL,other_4xx INTEGER NOT NULL,server_errors INTEGER NOT NULL,bytes INTEGER NOT NULL,time_ms REAL NOT NULL,capped INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(site,hour));
CREATE INDEX IF NOT EXISTS hourly_retention ON hourly(hour);
CREATE TABLE IF NOT EXISTS dimensions(site INTEGER NOT NULL,hour INTEGER NOT NULL,kind INTEGER NOT NULL,value TEXT NOT NULL,n INTEGER NOT NULL,PRIMARY KEY(site,hour,kind,value));
CREATE INDEX IF NOT EXISTS dimensions_retention ON dimensions(hour);`

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func scanCheckpoint(row interface{ Scan(...any) error }) (checkpoint, error) {
	var c checkpoint
	err := row.Scan(&c.site, &c.generation, &c.path, &c.start, &c.stop, &c.dev, &c.ino, &c.offset, &c.skipping, &c.lastTS, &c.observed, &c.invalid, &c.dropped, &c.capped, &c.cutoff, &c.backlog, &c.issue, &c.marker)
	return c, err
}

const checkpointColumns = "site,generation,path,start,stop,dev,ino,offset,skipping,last_ts,observed,invalid,dropped,capped,cutoff,backlog,issue,marker"

func (s *Store) loadCheckpoint(ctx context.Context, site int64) (checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return checkpoint{}, sql.ErrNoRows
	}
	return scanCheckpoint(s.db.QueryRowContext(ctx, "SELECT "+checkpointColumns+" FROM sources WHERE site=?", site))
}

func writeCheckpoint(ctx context.Context, tx *sql.Tx, c checkpoint) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO sources(`+checkpointColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(site) DO UPDATE SET generation=excluded.generation,path=excluded.path,start=excluded.start,stop=excluded.stop,dev=excluded.dev,ino=excluded.ino,offset=excluded.offset,skipping=excluded.skipping,last_ts=excluded.last_ts,observed=excluded.observed,invalid=excluded.invalid,dropped=excluded.dropped,capped=excluded.capped,cutoff=excluded.cutoff,backlog=excluded.backlog,issue=excluded.issue,marker=excluded.marker`, c.site, c.generation, c.path, c.start, c.stop, c.dev, c.ino, c.offset, c.skipping, c.lastTS, c.observed, c.invalid, c.dropped, c.capped, c.cutoff, c.backlog, c.issue, c.marker)
	return err
}

// reconcile changes the complete durable effective-source snapshot atomically.
// Capacity and identity checks precede all writes in the same transaction.
func (s *Store) reconcile(ctx context.Context, desired map[int64]Source, stopped map[int64]checkpoint, effective int64) (map[int64]checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := map[int64]checkpoint{}
	if s.db == nil && len(desired) == 0 {
		return next, nil
	}
	if err := s.ensureLocked(); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT "+checkpointColumns+" FROM sources")
	if err != nil {
		return nil, err
	}
	existing := map[int64]checkpoint{}
	for rows.Next() {
		cp, e := scanCheckpoint(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		existing[cp.site] = cp
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	retained := len(existing)
	ids := make([]int64, 0, len(desired))
	for id := range desired {
		ids = append(ids, id)
		if _, ok := existing[id]; !ok {
			retained++
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if retained > MaxRetainedSites {
		return nil, errors.New("traffic retained-site capacity reached; delete unused history")
	}
	for _, id := range ids {
		src := desired[id]
		cp, ok := existing[id]
		if ok && cp.generation == src.Generation {
			if cp.stop > 0 {
				return nil, errors.New("closed traffic generation requires a new generation")
			}
			if cp.path != src.Path || cp.start != src.StartTime.Unix() {
				return nil, errors.New("traffic generation identity changed")
			}
			next[id] = cp
		} else {
			var priorStop int64
			e := tx.QueryRowContext(ctx, "SELECT stop FROM intervals WHERE site=? AND generation=?", id, src.Generation).Scan(&priorStop)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return nil, e
			}
			if e == nil {
				return nil, errors.New("traffic generation cannot be reused")
			}
			next[id] = checkpoint{site: id, generation: src.Generation, path: src.Path, start: src.StartTime.Unix()}
		}
	}
	for id, cp := range existing {
		src, wanted := desired[id]
		if cp.stop != 0 || wanted && src.Generation == cp.generation {
			continue
		}
		if effective < cp.start {
			return nil, errors.New("traffic stop time precedes its generation")
		}
	}
	// Close all removed/replaced generations, including intervals loaded after a
	// restart. No unrelated retained source or active in-memory file changes yet.
	for id, cp := range existing {
		src, wanted := desired[id]
		if cp.stop != 0 || wanted && src.Generation == cp.generation {
			continue
		}
		if known, ok := stopped[id]; ok && known.generation == cp.generation {
			cp = known
		} else {
			cp.capped = true
			cp.issue = "Collection ended while the panel was offline; interval coverage is unknown"
		}
		cp.stop = effective
		cp.backlog = 0
		if err = writeCheckpoint(ctx, tx, cp); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE intervals SET stop=?,incomplete=max(incomplete,?) WHERE site=? AND generation=? AND stop=0", effective, cp.capped || cp.invalid > 0 || cp.dropped > 0 || cp.issue != "", id, cp.generation); err != nil {
			return nil, err
		}
	}
	for _, id := range ids {
		cp := next[id]
		if err = s.ensureAPIRule(ctx, tx, id, max(s.apiSupportNS, cp.start*int64(time.Second))); err != nil {
			return nil, err
		}
		old, ok := existing[id]
		if ok && old.generation == cp.generation {
			continue
		}
		if err = writeCheckpoint(ctx, tx, cp); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO intervals(site,generation,start,stop) VALUES(?,?,?,0)", id, cp.generation, max(cp.start, int64(cp.cutoff))); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM intervals WHERE site=? AND generation NOT IN (SELECT generation FROM intervals WHERE site=? ORDER BY start DESC LIMIT 128)", id, id); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	for id, live := range s.live {
		src, ok := desired[id]
		if !ok || src.Generation != live.cp.generation {
			delete(s.live, id)
		}
	}
	return next, nil
}

func (s *Store) apply(ctx context.Context, b *batch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.boundWALLocked(ctx); err != nil {
		return err
	}
	var fs unix.Statfs_t
	if e := unix.Statfs(s.dir, &fs); e != nil {
		return e
	}
	if fs.Bavail*uint64(fs.Bsize) < 32<<20 {
		return errors.New("traffic collection paused: less than 32 MiB available")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	added := int64(0)
	apiAdded := int64(0)
	hours := make([]int64, 0, len(b.hours))
	for h := range b.hours {
		hours = append(hours, h)
	}
	sort.Slice(hours, func(i, j int) bool { return hours[i] < hours[j] })
	for _, h := range hours {
		a := b.hours[h]
		if a.api != nil {
			n, err := s.applyAPIAggregate(ctx, tx, b.cp.site, h, a.api, apiAdded)
			if err != nil {
				return err
			}
			apiAdded += n
		}
		for kind, values := range []map[string]int64{a.pages, a.referrers} {
			capN := MaxPageDimensionsPerHour
			if kind == 1 {
				capN = MaxReferrerDimensionsPerHour
			}
			rows, err := tx.QueryContext(ctx, "SELECT value FROM dimensions WHERE site=? AND hour=? AND kind=? AND value<>?", b.cp.site, h, kind, overflowValue)
			if err != nil {
				return err
			}
			known := map[string]bool{}
			for rows.Next() {
				var v string
				if err = rows.Scan(&v); err != nil {
					rows.Close()
					return err
				}
				known[v] = true
			}
			if err = rows.Close(); err != nil {
				return err
			}
			keys := make([]string, 0, len(values))
			for v := range values {
				keys = append(keys, v)
			}
			sort.Strings(keys)
			overflow := int64(0)
			for _, v := range keys {
				n := values[v]
				if v == overflowValue || !known[v] && (len(known) >= capN || s.dimensionRows+added >= MaxDimensionRows) {
					overflow += n
					a.capped = true
					continue
				}
				if !known[v] {
					known[v] = true
					added++
				}
				if _, e = tx.ExecContext(ctx, `INSERT INTO dimensions(site,hour,kind,value,n) VALUES(?,?,?,?,?) ON CONFLICT(site,hour,kind,value) DO UPDATE SET n=min(9000000000000000,dimensions.n+excluded.n)`, b.cp.site, h, kind, v, n); e != nil {
					return e
				}
			}
			if overflow > 0 {
				if _, e = tx.ExecContext(ctx, `INSERT INTO dimensions(site,hour,kind,value,n) VALUES(?,?,?,?,?) ON CONFLICT(site,hour,kind,value) DO UPDATE SET n=min(9000000000000000,dimensions.n+excluded.n)`, b.cp.site, h, kind, overflowValue, overflow); e != nil {
					return e
				}
			}
		}
		if a.capped {
			b.cp.capped = true
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO hourly(site,hour,requests,pageviews,not_found,other_4xx,server_errors,bytes,time_ms,capped) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(site,hour) DO UPDATE SET requests=min(9000000000000000,hourly.requests+excluded.requests),pageviews=min(9000000000000000,hourly.pageviews+excluded.pageviews),not_found=min(9000000000000000,hourly.not_found+excluded.not_found),other_4xx=min(9000000000000000,hourly.other_4xx+excluded.other_4xx),server_errors=min(9000000000000000,hourly.server_errors+excluded.server_errors),bytes=min(9000000000000000,hourly.bytes+excluded.bytes),time_ms=min(9000000000000000,hourly.time_ms+excluded.time_ms),capped=max(hourly.capped,excluded.capped)`, b.cp.site, h, a.requests, a.pageviews, a.notFound, a.other4xx, a.serverErrors, a.bytes, a.timeMS, a.capped)
		if e != nil {
			return e
		}
	}
	if b.cp.ino != 0 {
		if _, e = tx.ExecContext(ctx, "UPDATE intervals SET verified=1,incomplete=max(incomplete,?) WHERE site=? AND generation=?", b.cp.capped || b.cp.invalid > 0 || b.cp.dropped > 0 || b.cp.issue != "", b.cp.site, b.cp.generation); e != nil {
			return e
		}
	}
	if e = writeCheckpoint(ctx, tx, b.cp); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	s.dimensionRows += added
	s.apiRows += apiAdded
	// No report can hold this connection while this bounded checkpoint executes.
	_, _ = s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return nil
}

// Prune removes at most 4096 rows per table per invocation. Uncollected expired
// frames are filtered by the parser, independently of this incremental cleanup.
func (s *Store) Prune(ctx context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	cut := now.Add(-retention).UTC().Truncate(time.Hour).Unix()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, q := range []string{"DELETE FROM dimensions WHERE rowid IN (SELECT rowid FROM dimensions WHERE hour<? LIMIT 4096)", "DELETE FROM hourly WHERE rowid IN (SELECT rowid FROM hourly WHERE hour<? LIMIT 4096)", "DELETE FROM intervals WHERE rowid IN (SELECT rowid FROM intervals WHERE stop>0 AND stop<? LIMIT 4096)", "DELETE FROM api_endpoints WHERE rowid IN (SELECT rowid FROM api_endpoints WHERE hour<? LIMIT 4096)", "DELETE FROM api_hourly WHERE rowid IN (SELECT rowid FROM api_hourly WHERE hour<? LIMIT 4096)"} {
		if _, e = tx.ExecContext(ctx, q, cut); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM api_rules WHERE rowid IN (SELECT rowid FROM api_rules WHERE stop_ns>0 AND stop_ns<? LIMIT 4096)", now.Add(-retention).UnixNano()); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM sources WHERE site IN (SELECT site FROM sources WHERE stop>0 AND stop<? LIMIT 32)", cut); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if e = s.db.QueryRowContext(ctx, "SELECT count(*) FROM dimensions WHERE value<>?", overflowValue).Scan(&s.dimensionRows); e != nil {
		s.dimensionRows = MaxDimensionRows
	}
	if e = s.db.QueryRowContext(ctx, "SELECT count(*) FROM api_endpoints WHERE path<>?", overflowValue).Scan(&s.apiRows); e != nil {
		s.apiRows = MaxAPIEndpointRows
	}
	_, _ = s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return nil
}

func (s *Store) deleteHistory(ctx context.Context, siteID int64, cp *checkpoint, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	// Global dimension rows are capped, and hourly rows have <=720 retained hours
	// per site. Deletion touches aggregates only, never raw requests.
	for _, table := range []string{"dimensions", "hourly", "intervals", "sources", "api_endpoints", "api_hourly", "api_rules"} {
		if _, e = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE site=?", siteID); e != nil {
			return e
		}
	}
	if cp != nil {
		if e = writeCheckpoint(ctx, tx, *cp); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO intervals(site,generation,start,stop,verified) VALUES(?,?,?,0,?)", siteID, cp.generation, now.Unix(), cp.ino != 0); e != nil {
			return e
		}
		if e = s.ensureAPIRule(ctx, tx, siteID, now.UnixNano()); e != nil {
			return e
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if e = s.db.QueryRowContext(ctx, "SELECT count(*) FROM dimensions WHERE value<>?", overflowValue).Scan(&s.dimensionRows); e != nil {
		s.dimensionRows = MaxDimensionRows
	}
	if e = s.db.QueryRowContext(ctx, "SELECT count(*) FROM api_endpoints WHERE path<>?", overflowValue).Scan(&s.apiRows); e != nil {
		s.apiRows = MaxAPIEndpointRows
	}
	_, _ = s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return nil
}

type liveStatus struct {
	cp      checkpoint
	checked time.Time
	failure string
}

func (s *Store) setLive(id int64, live *liveStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live == nil {
		s.live = map[int64]liveStatus{}
	}
	if live == nil {
		delete(s.live, id)
	} else {
		s.live[id] = *live
	}
}

// A failed post-commit checkpoint must never be reported as a failed aggregate
// transaction: the caller would retry already committed bytes. Before another
// write, enforce a small WAL baseline; one bounded transaction may overshoot it.
func (s *Store) boundWALLocked(ctx context.Context) error {
	info, err := os.Stat(filepath.Join(s.dir, "analytics.db-wal"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() <= 8<<20 {
		return nil
	}
	var busy, frames, done int
	if err = s.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &done); err != nil {
		return err
	}
	if busy != 0 {
		return errors.New("traffic collection paused: analytics WAL checkpoint is busy")
	}
	return nil
}
