package nginx

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"fluxo/internal/syscmd"
	"golang.org/x/sys/unix"
)

const trafficStateDirectory = "/etc/nginx/.fluxo-traffic"
const trafficHTTPPath = "/etc/nginx/conf.d/fluxo-traffic.conf"

// configurationMu serializes site regeneration, traffic publication, and public
// reloads. The on-disk traffic lock also serializes independent control planes.
var configurationMu sync.Mutex

// TrafficSite is one effective enabled site, after master/per-site policy has
// been combined by the caller. Generation is new for every activation span.
type TrafficSite struct {
	ID         int64  `json:"id"`
	ConfigName string `json:"config_name"`
	Domain     string `json:"domain"`
	Generation string `json:"generation"`
	LogPath    string `json:"log_path"`
}

type TrafficApplyResult struct {
	AppliedAt       time.Time       `json:"applied_at"`
	RetiringWorkers []TrafficWorker `json:"retiring_workers"`
}

type trafficState struct {
	Operation       string          `json:"operation"`
	Sites           []TrafficSite   `json:"sites"`
	InheritedLogs   []string        `json:"inherited_logs"`
	RetiringWorkers []TrafficWorker `json:"retiring_workers"`
	AppliedAt       time.Time       `json:"applied_at"`
}

type trafficChange struct {
	Path   string `json:"path"`
	Before []byte `json:"before"`
	After  []byte `json:"after"`
	Mode   uint32 `json:"mode"`
	GID    uint32 `json:"gid"`
}
type trafficIntent struct {
	Version   int             `json:"version"`
	Operation string          `json:"operation"`
	Changes   []trafficChange `json:"changes"`
	Workers   []TrafficWorker `json:"workers"`
}

type trafficEnvironment struct {
	available, enabled, httpPath, stateDir string
	strict                                 bool
	validate, reload                       func(context.Context) error
	dump                                   func(context.Context) (string, error)
	processes                              func(context.Context) (trafficProcesses, error)
	probe                                  func(context.Context, string, string) error
	drained                                func(context.Context, []TrafficWorker) (bool, error)
	awaitShutdown                          func(context.Context, []TrafficWorker) error
}

func productionTrafficEnvironment() trafficEnvironment {
	return trafficEnvironment{available: sitesAvailable, enabled: sitesEnabled, httpPath: trafficHTTPPath, stateDir: trafficStateDirectory, strict: true,
		validate: func(ctx context.Context) error {
			_, err := syscmd.Run(ctx, 10*time.Second, nginxBinaryPath, "-t")
			return err
		}, reload: reloadService, processes: trafficProcessSnapshot, probe: probeTrafficActivation, drained: TrafficWorkersDrained, awaitShutdown: awaitTrafficWorkerShutdown,
		dump: func(ctx context.Context) (string, error) {
			return syscmd.Run(ctx, 10*time.Second, nginxBinaryPath, "-T")
		}}
}

// ApplyTraffic atomically validates and activates the complete effective set.
// Empty on a never-used installation is a strict no-op. A successful reload is
// insufficient: the Unix-only marker must be served by loaded Nginx workers.
// RetiringWorkers must drain before the caller reports a completed OFF switch.
func ApplyTraffic(ctx context.Context, sites []TrafficSite) (TrafficApplyResult, error) {
	configurationMu.Lock()
	defer configurationMu.Unlock()
	return applyTraffic(ctx, productionTrafficEnvironment(), sites)
}

func applyTraffic(ctx context.Context, env trafficEnvironment, sites []TrafficSite) (TrafficApplyResult, error) {
	var result TrafficApplyResult
	if err := validateTrafficSites(sites); err != nil {
		return result, err
	}
	if len(sites) == 0 {
		_, stateErr := os.Lstat(env.stateDir)
		_, configErr := os.Lstat(env.httpPath)
		if os.IsNotExist(stateErr) && os.IsNotExist(configErr) {
			return TrafficApplyResult{AppliedAt: time.Now().UTC()}, nil
		}
	}
	if err := trafficEnsurePrivateDirectory(env.stateDir, env.strict); err != nil {
		return result, err
	}
	lock, err := trafficOpenFile(env.stateDir, "lock", unix.O_CREAT|unix.O_RDWR, 0600, env.strict, true)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	for {
		err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return result, err
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	state, err := readTrafficState(env)
	if err != nil {
		return result, err
	}
	if err := recoverTrafficIntent(ctx, env, &state); err != nil {
		return result, err
	}
	snapshot, err := env.processes(ctx)
	if err != nil {
		return result, err
	}
	retained, err := liveTrafficWorkers(ctx, env, state.RetiringWorkers)
	if err != nil {
		return result, err
	}
	workers := mergeTrafficWorkers(retained, snapshot.Workers)
	if len(workers) > 4096 {
		return result, errors.New("too many previous Nginx workers are still draining; retry after their requests finish")
	}
	var inherited []string
	if len(sites) > 0 {
		dump, err := env.dump(ctx)
		if err != nil {
			return result, fmt.Errorf("inspect Nginx logging policy: %w", err)
		}
		inherited, err = trafficInheritedLogs(dump)
		if err != nil {
			return result, err
		}
	}
	operation, err := newTrafficOperation()
	if err != nil {
		return result, err
	}
	after := trafficState{Operation: operation, Sites: sites, InheritedLogs: inherited, RetiringWorkers: workers}
	changes, err := planTrafficChanges(env, state, after)
	if err != nil {
		return result, err
	}
	for _, site := range sites {
		if err := prepareTrafficLog(site.LogPath, env.strict); err != nil {
			return result, err
		}
	}
	intent := trafficIntent{Version: 1, Operation: operation, Changes: changes, Workers: workers}
	if err := writeTrafficJSON(env, "intent.json", intent); err != nil {
		return result, err
	}
	// The intent is durable before the first public mutation. Recovery restores
	// its exact baseline; no disk state is mistaken for loaded worker state.
	for _, change := range changes {
		if err := applyTrafficChange(env, change, false); err != nil {
			return result, rollbackTrafficError(ctx, env, state, intent, false, err)
		}
	}
	if err := env.validate(ctx); err != nil {
		return result, rollbackTrafficError(ctx, env, state, intent, false, fmt.Errorf("traffic Nginx validation failed: %w", err))
	}
	if err := env.reload(ctx); err != nil {
		return result, rollbackTrafficError(ctx, env, state, intent, true, fmt.Errorf("traffic Nginx reload failed: %w", err))
	}
	if err := env.probe(ctx, filepath.Join(env.stateDir, "control.sock"), operation); err != nil {
		return result, rollbackTrafficError(ctx, env, state, intent, true, err)
	}
	if env.awaitShutdown != nil {
		if err := env.awaitShutdown(ctx, workers); err != nil {
			return result, rollbackTrafficError(ctx, env, state, intent, true, err)
		}
	}
	current, err := env.processes(ctx)
	if err != nil {
		return result, rollbackTrafficError(ctx, env, state, intent, true, err)
	}
	if current.Master != snapshot.Master {
		return result, rollbackTrafficError(ctx, env, state, intent, true, errors.New("Nginx master changed during traffic activation; retry after it stabilizes"))
	}
	after.AppliedAt = time.Now().UTC()
	after.RetiringWorkers, err = liveTrafficWorkers(ctx, env, workers)
	if err != nil {
		return result, rollbackTrafficError(ctx, env, state, intent, true, err)
	}
	if err := writeTrafficJSON(env, "state.json", after); err != nil {
		return result, rollbackTrafficError(ctx, env, state, intent, true, err)
	}
	// state.Operation is the commit record. A crash before intent removal is
	// distinguishable from an uncommitted transaction on the next invocation.
	if err := removeTrafficFile(env.stateDir, "intent.json", env.strict); err != nil {
		return result, fmt.Errorf("traffic was activated but transaction cleanup must be retried: %w", err)
	}
	return TrafficApplyResult{after.AppliedAt, after.RetiringWorkers}, nil
}

func validateTrafficSites(sites []TrafficSite) error {
	if len(sites) > 1000 {
		return errors.New("traffic analytics supports at most 1000 enabled sites")
	}
	seen := map[string]bool{}
	ids := map[int64]bool{}
	paths := map[string]bool{}
	for _, site := range sites {
		if site.ID <= 0 || ids[site.ID] || !trafficConfigNamePattern.MatchString(site.ConfigName) || seen[site.ConfigName] || !trafficGenerationPattern.MatchString(site.Generation) || !trafficPathPattern.MatchString(site.LogPath) || !strings.HasSuffix(site.LogPath, ".jsonl") || len(site.LogPath) > 4095 || filepath.Clean(site.LogPath) != site.LogPath || paths[site.LogPath] {
			return errors.New("invalid or duplicate traffic site identity, generation, or absolute log path")
		}
		if strings.HasPrefix(site.ConfigName, ".") || site.ConfigName == defaultSite || site.ConfigName == panelSiteName {
			return errors.New("reserved Nginx site name")
		}
		seen[site.ConfigName] = true
		ids[site.ID] = true
		paths[site.LogPath] = true
	}
	return nil
}

func planTrafficChanges(env trafficEnvironment, before, after trafficState) ([]trafficChange, error) {
	desired := map[string]TrafficSite{}
	names := map[string]bool{}
	for _, site := range before.Sites {
		names[site.ConfigName] = true
	}
	for _, site := range after.Sites {
		desired[site.ConfigName] = site
		names[site.ConfigName] = true
	}
	var sorted []string
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	var changes []trafficChange
	for _, name := range sorted {
		path := filepath.Join(env.available, name)
		raw, mode, err := readTrafficFile(path, env.strict, true)
		if os.IsNotExist(err) {
			if _, enabled := desired[name]; enabled {
				return nil, fmt.Errorf("traffic site %s has no existing Nginx configuration", name)
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		target, err := os.Readlink(filepath.Join(env.enabled, name))
		if err != nil {
			if _, enabled := desired[name]; enabled || !os.IsNotExist(err) {
				return nil, fmt.Errorf("traffic site %s must be enabled through its existing Nginx symlink: %w", name, err)
			}
		}
		if err := trafficCheckDirectory(env.enabled, env.strict); err != nil {
			return nil, err
		}
		if target != "" && !symlinkPointsTo(filepath.Join(env.enabled, name), target, path) {
			return nil, fmt.Errorf("traffic site %s has a custom enabled target", name)
		}
		clean, err := stripTrafficConfig(string(raw))
		if err != nil {
			return nil, err
		}
		updated := clean
		if site, enabled := desired[name]; enabled {
			updated, err = injectTrafficConfig(clean, site, after.InheritedLogs)
			if err != nil {
				return nil, fmt.Errorf("traffic site %s: %w", name, err)
			}
		}
		if !bytes.Equal(raw, []byte(updated)) {
			changes = append(changes, trafficChange{Path: path, Before: raw, After: []byte(updated), Mode: uint32(mode), GID: trafficFileGroup(path)})
		}
	}
	raw, mode, err := readTrafficFile(env.httpPath, env.strict, true)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil && !bytes.HasPrefix(raw, []byte(trafficHTTPHeader)) {
		return nil, errors.New("refusing to replace custom Nginx traffic HTTP configuration")
	}
	if mode == 0 {
		mode = 0644
	}
	updated := []byte(renderTrafficHTTP(after.Sites, filepath.Join(env.stateDir, "control.sock"), after.Operation))
	changes = append(changes, trafficChange{Path: env.httpPath, Before: raw, After: updated, Mode: uint32(mode), GID: trafficFileGroup(env.httpPath)})
	return changes, nil
}

func rollbackTrafficError(ctx context.Context, env trafficEnvironment, state trafficState, intent trafficIntent, reload bool, cause error) error {
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := restoreTrafficIntent(recoveryCtx, env, state, intent, reload); err != nil {
		return fmt.Errorf("%w (traffic recovery pending: %v)", cause, err)
	}
	return cause
}

func restoreTrafficIntent(ctx context.Context, env trafficEnvironment, state trafficState, intent trafficIntent, reload bool) error {
	// Verify every destination before restoring any of them. An operator's
	// unrelated edit is retained with the journal for explicit reconciliation.
	for _, change := range intent.Changes {
		raw, _, err := readTrafficFile(change.Path, env.strict, true)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if !bytes.Equal(raw, change.Before) && !bytes.Equal(raw, change.After) {
			return fmt.Errorf("traffic recovery found a changed Nginx file: %s", change.Path)
		}
	}
	for i := len(intent.Changes) - 1; i >= 0; i-- {
		if err := applyTrafficChange(env, intent.Changes[i], true); err != nil {
			return err
		}
	}
	if reload {
		current, err := env.processes(ctx)
		if err != nil {
			return err
		}
		state.RetiringWorkers = mergeTrafficWorkers(state.RetiringWorkers, intent.Workers, current.Workers)
		if err := env.validate(ctx); err != nil {
			return err
		}
		if err := env.reload(ctx); err != nil {
			return err
		}
		if state.Operation != "" {
			if err := env.probe(ctx, filepath.Join(env.stateDir, "control.sock"), state.Operation); err != nil {
				return err
			}
		}
		// With no previous marker, a rollback cannot prove which config is loaded.
		// Retain all possibly writing workers; the next Apply always publishes a
		// fresh marker and resolves this uncertainty before returning success.
	}
	if err := writeTrafficJSON(env, "state.json", state); err != nil {
		return err
	}
	return removeTrafficFile(env.stateDir, "intent.json", env.strict)
}

func recoverTrafficIntent(ctx context.Context, env trafficEnvironment, state *trafficState) error {
	raw, _, err := readTrafficFile(filepath.Join(env.stateDir, "intent.json"), env.strict, true)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var intent trafficIntent
	if err := json.Unmarshal(raw, &intent); err != nil {
		return fmt.Errorf("invalid traffic recovery intent: %w", err)
	}
	if intent.Version != 1 || !trafficGenerationPattern.MatchString(intent.Operation) || len(intent.Changes) > 1001 || len(intent.Workers) > 4096 {
		return errors.New("invalid traffic recovery intent")
	}
	for _, change := range intent.Changes {
		if change.Path != env.httpPath && (filepath.Dir(change.Path) != env.available || !validConfigName(filepath.Base(change.Path))) {
			return errors.New("traffic recovery intent contains an invalid destination")
		}
	}
	if state.Operation == intent.Operation {
		return removeTrafficFile(env.stateDir, "intent.json", env.strict)
	}
	if err := restoreTrafficIntent(ctx, env, *state, intent, true); err != nil {
		return err
	}
	recovered, err := readTrafficState(env)
	if err == nil {
		*state = recovered
	}
	return err
}

func readTrafficState(env trafficEnvironment) (trafficState, error) {
	var state trafficState
	raw, _, err := readTrafficFile(filepath.Join(env.stateDir, "state.json"), env.strict, true)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err = json.Unmarshal(raw, &state); err != nil {
		return state, err
	}
	if len(state.RetiringWorkers) > 4096 {
		return state, errors.New("retained traffic worker set exceeded its size limit")
	}
	if state.Operation != "" && !trafficGenerationPattern.MatchString(state.Operation) {
		return state, errors.New("invalid retained traffic operation")
	}
	if err = validateTrafficSites(state.Sites); err != nil {
		return state, err
	}
	return state, nil
}

func newTrafficOperation() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
func mergeTrafficWorkers(groups ...[]TrafficWorker) []TrafficWorker {
	seen := map[TrafficWorker]bool{}
	var out []TrafficWorker
	for _, group := range groups {
		for _, worker := range group {
			if !seen[worker] {
				seen[worker] = true
				out = append(out, worker)
			}
		}
	}
	return out
}
func liveTrafficWorkers(ctx context.Context, env trafficEnvironment, workers []TrafficWorker) ([]TrafficWorker, error) {
	var live []TrafficWorker
	for _, worker := range workers {
		drained, err := env.drained(ctx, []TrafficWorker{worker})
		if err != nil {
			return nil, err
		}
		if !drained {
			live = append(live, worker)
		}
	}
	return live, nil
}

func preserveTrafficConfig(env trafficEnvironment, name, config string) (string, error) {
	if _, err := os.Lstat(env.stateDir); os.IsNotExist(err) {
		return config, nil
	} else if err != nil {
		return "", err
	}
	if _, err := os.Lstat(filepath.Join(env.stateDir, "intent.json")); err == nil {
		return "", errors.New("finish pending traffic analytics recovery before changing this Nginx site")
	} else if !os.IsNotExist(err) {
		return "", err
	}
	state, err := readTrafficState(env)
	if err != nil {
		return "", err
	}
	for _, site := range state.Sites {
		if site.ConfigName == name {
			return injectTrafficConfig(config, site, state.InheritedLogs)
		}
	}
	return config, nil
}

// The filesystem helpers use directory FDs, no-follow opens, single-link files,
// and durable atomic replacement. Production rejects symlinks and writable or
// untrusted ancestors; tests use private temporary directories under /tmp.
func trafficDirectory(path string, strict bool) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("unsafe traffic directory path")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		if part == "" {
			continue
		}
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		unix.Close(fd)
		if err != nil {
			return nil, err
		}
		fd = next
		var st unix.Stat_t
		if err := unix.Fstat(fd, &st); err != nil {
			unix.Close(fd)
			return nil, err
		}
		if (strict || i == len(parts)-1) && (st.Uid != uint32(os.Geteuid()) || st.Mode&0022 != 0) {
			unix.Close(fd)
			return nil, fmt.Errorf("unsafe traffic directory ownership or permissions: %s", path)
		}
	}
	return os.NewFile(uintptr(fd), path), nil
}
func trafficCheckDirectory(path string, strict bool) error {
	directory, err := trafficDirectory(path, strict)
	if err != nil {
		return err
	}
	return directory.Close()
}
func trafficEnsurePrivateDirectory(path string, strict bool) error {
	parent, err := trafficDirectory(filepath.Dir(path), strict)
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := unix.Mkdirat(int(parent.Fd()), filepath.Base(path), 0700); err != nil && !errors.Is(err, unix.EEXIST) {
		return err
	}
	directory, err := trafficDirectory(path, strict)
	if err != nil {
		return err
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	if info.Mode().Perm() != 0700 {
		return errors.New("traffic control directory must be private (0700)")
	}
	return nil
}
func trafficOpenFile(dir, name string, flags int, mode uint32, strict, owner bool) (*os.File, error) {
	parent, err := trafficDirectory(dir, strict)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, mode)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || st.Nlink != 1 || info.Mode().Perm()&0022 != 0 || owner && st.Uid != uint32(os.Geteuid()) {
		file.Close()
		return nil, fmt.Errorf("unsafe traffic file: %s", filepath.Join(dir, name))
	}
	return file, nil
}
func readTrafficFile(path string, strict, owner bool) ([]byte, os.FileMode, error) {
	file, err := trafficOpenFile(filepath.Dir(path), filepath.Base(path), unix.O_RDONLY, 0, strict, owner)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	if info.Size() > 16<<20 {
		return nil, 0, errors.New("traffic control file exceeded its size limit")
	}
	raw, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
	if len(raw) > 16<<20 {
		return nil, 0, errors.New("traffic control file exceeded its size limit")
	}
	return raw, info.Mode().Perm(), err
}
func writeTrafficFile(path string, raw []byte, mode os.FileMode, strict bool, group ...uint32) error {
	parent, err := trafficDirectory(filepath.Dir(path), strict)
	if err != nil {
		return err
	}
	defer parent.Close()
	token, err := newTrafficOperation()
	if err != nil {
		return err
	}
	temporary := ".traffic-" + token
	fd, err := unix.Openat(int(parent.Fd()), temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(mode.Perm()))
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), temporary)
	if len(group) > 0 {
		if err := unix.Fchown(fd, -1, int(group[0])); err != nil {
			file.Close()
			unix.Unlinkat(int(parent.Fd()), temporary, 0)
			return err
		}
	}
	defer func() { file.Close(); unix.Unlinkat(int(parent.Fd()), temporary, 0) }()
	if _, err := file.Write(raw); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := unix.Renameat(int(parent.Fd()), temporary, int(parent.Fd()), filepath.Base(path)); err != nil {
		return err
	}
	return parent.Sync()
}
func removeTrafficFile(dir, name string, strict bool) error {
	parent, err := trafficDirectory(dir, strict)
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := unix.Unlinkat(int(parent.Fd()), name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return err
	}
	return parent.Sync()
}
func writeTrafficJSON(env trafficEnvironment, name string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) > 16<<20 {
		return errors.New("traffic transaction exceeded its durable size limit")
	}
	return writeTrafficFile(filepath.Join(env.stateDir, name), raw, 0600, env.strict)
}
func applyTrafficChange(env trafficEnvironment, change trafficChange, reverse bool) error {
	raw, _, err := readTrafficFile(change.Path, env.strict, true)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	expected, desired := change.Before, change.After
	if reverse {
		expected, desired = change.After, change.Before
	}
	if bytes.Equal(raw, desired) {
		return nil
	}
	if !bytes.Equal(raw, expected) {
		return fmt.Errorf("Nginx traffic destination changed during transaction: %s", change.Path)
	}
	if desired == nil {
		return removeTrafficFile(filepath.Dir(change.Path), filepath.Base(change.Path), env.strict)
	}
	return writeTrafficFile(change.Path, desired, os.FileMode(change.Mode), env.strict, change.GID)
}
func prepareTrafficLog(path string, strict bool) error {
	directory, err := trafficDirectory(filepath.Dir(path), strict)
	if err != nil {
		return err
	}
	info, err := directory.Stat()
	directory.Close()
	if err != nil {
		return err
	}
	if info.Mode().Perm() != 0700 {
		return errors.New("traffic source directory must be private (0700)")
	}
	file, err := trafficOpenFile(filepath.Dir(path), filepath.Base(path), unix.O_CREAT|unix.O_APPEND|unix.O_WRONLY, 0600, strict, false)
	if err != nil {
		return fmt.Errorf("prepare traffic log: %w", err)
	}
	return file.Close()
}

// TrafficRecoveryReady is a read-only admission check for other Nginx writers
// and upgrade qualification. Recovery must finish before another path can
// activate or delete files from a retained analytics transaction.
func TrafficRecoveryReady() error { return trafficRecoveryReady(productionTrafficEnvironment()) }
func trafficRecoveryReady(env trafficEnvironment) error {
	if _, err := os.Lstat(env.stateDir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	directory, err := trafficDirectory(env.stateDir, env.strict)
	if err != nil {
		return err
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	if info.Mode().Perm() != 0700 {
		return errors.New("traffic control directory must remain private (0700)")
	}
	if _, _, err := readTrafficFile(filepath.Join(env.stateDir, "intent.json"), env.strict, true); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("finish pending traffic analytics recovery before changing or qualifying Nginx configuration")
}

func trafficFileGroup(path string) uint32 {
	info, err := os.Lstat(path)
	if err == nil {
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			return st.Gid
		}
	}
	return uint32(os.Getegid())
}
