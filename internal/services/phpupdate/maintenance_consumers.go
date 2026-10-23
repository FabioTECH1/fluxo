package phpupdate

// This bridge coordinates application consumers only. It deliberately does not
// inspect an APT plan, capture FPM, or create a PHP maintenance generation.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/octaneruntime"
	sitepkg "fluxo/internal/services/site"
	"fluxo/internal/syscmd"
)

type MaintenanceRuntime = unitSnapshot
type MaintenanceResume = naturalResume

type MaintenancePHP struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}
type MaintenanceUnit struct {
	Runtime    MaintenanceRuntime `json:"runtime"`
	Definition map[string]string  `json:"definition"`
	Enabled    string             `json:"enabled"`
}
type MaintenanceConsumer struct {
	Branch   string            `json:"branch"`
	Domain   string            `json:"domain"`
	Consumer Consumer          `json:"consumer"`
	PHP      MaintenancePHP    `json:"php"`
	Units    []MaintenanceUnit `json:"units"`
}
type MaintenanceInspection struct {
	Recovery  bool
	Normalize func(string, map[string]string) (map[string]string, error)
	// Only additional, independently authenticated descendants may be removed
	// from the PHP/native validator's input. The controller is never exempted.
	AdditionalProcess func(MaintenanceRuntime, int) (bool, error)
}

func maintenancePHP(ctx context.Context, branch string) (MaintenancePHP, error) {
	r := MaintenancePHP{Path: "/usr/bin/php" + branch}
	if !ValidBranch(branch) {
		return r, ErrUnsupported
	}
	if err := packageOwns(ctx, r.Path, branch); err != nil {
		return r, err
	}
	b, err := os.ReadFile(r.Path)
	if err != nil {
		return r, err
	}
	h := sha256.Sum256(b)
	r.SHA256 = hex.EncodeToString(h[:])
	r.Version, err = syscmd.RunAsUser(ctx, 5*time.Second, "fluxo", r.Path, "-n", "-r", "echo PHP_VERSION;")
	r.Version = strings.TrimSpace(r.Version)
	if err != nil || !strings.HasPrefix(r.Version, branch+".") {
		return r, errors.New("Cannot verify the unchanged PHP interpreter")
	}
	return r, nil
}

// InspectMaintenanceConsumers enumerates every non-exempt record with a LEFT
// JOIN. A managed label, a stopped state, or a command substring is no exemption.
func InspectMaintenanceConsumers(ctx context.Context, exempt map[int]bool, options MaintenanceInspection) ([]MaintenanceConsumer, []string, error) {
	rows, err := database.DB.QueryContext(ctx, `SELECT d.id,d.site_id,COALESCE(d.name,''),COALESCE(d.managed_kind,''),d.command,d.directory,COALESCE(d.instances,1),COALESCE(d.stop_seconds,15),d.user,COALESCE(s.id,0),COALESCE(s.path,''),COALESCE(s.deployment_strategy,'standard'),COALESCE(s.app_type,''),COALESCE(s.domain,''),COALESCE(s.app_port,0) FROM daemons d LEFT JOIN sites s ON s.id=d.site_id ORDER BY d.id`)
	if err != nil {
		return nil, nil, err
	}
	type record struct {
		c    Consumer
		site Site
		user string
	}
	var records []record
	for rows.Next() {
		var r record
		if err = rows.Scan(&r.c.ID, &r.c.SiteID, &r.c.Name, &r.c.Kind, &r.c.Command, &r.c.Directory, &r.c.Instances, &r.c.StopSeconds, &r.user, &r.site.ID, &r.site.Path, &r.site.Strategy, &r.site.AppType, &r.site.Domain, &r.site.Port); err != nil {
			rows.Close()
			return nil, nil, err
		}
		records = append(records, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	set := []MaintenanceConsumer{}
	reasons := []string{}
	for _, r := range records {
		if exempt[r.c.ID] {
			continue
		}
		c, s := r.c, r.site
		fields := strings.Fields(c.Command)
		branch := ""
		if len(fields) > 0 {
			branch = strings.TrimPrefix(filepath.Base(fields[0]), "php")
		}
		block := func(e error) {
			reasons = append(reasons, fmt.Sprintf("Background process %s: %v", sitepkg.MaintenanceLabel(c.Name, s.Domain), e))
		}
		if !ValidBranch(branch) || s.ID <= 0 || s.ID != c.SiteID || s.AppType != "php" && s.AppType != "laravel" {
			block(errors.New("no verified PHP consumer protocol; manual Node.js maintenance is required"))
			continue
		}
		if qualifyCustomQueue(branch, c, s) {
			c.Protocol = "laravel_queue"
		} else if customArtisanSite(c, s) {
			if _, ok := customArtisanArguments(branch, c.Command); ok {
				c.Protocol = "artisan_natural"
			}
		}
		if issue := workerMetadataIssue(branch, c, r.user); issue != "" {
			block(errors.New(issue))
			continue
		}
		if c.Kind == "laravel_nightwatch" {
			port, ok := managedNightwatchPort(branch, c.Command)
			if !ok || c.Instances != 1 {
				block(ErrUnsupported)
				continue
			}
			c.Port = port
		}
		if c.Kind == "laravel_octane" {
			c.Port = s.Port
			if c.Instances != 1 || c.Command != octaneruntime.Command(branch, c.Port) {
				block(ErrUnsupported)
				continue
			}
			probe, e := probeOctane(ctx, branch, c, false)
			if e != nil {
				block(e)
				continue
			}
			c.Octane = &probe.Descriptor
		}
		// All consumers must be site-bound, including managed records. Keep the
		// custom parser's directory policy for managed features as well.
		if !customArtisanSite(Consumer{SiteID: c.SiteID, Directory: c.Directory}, s) {
			block(errors.New("PHP consumer directory does not match its site"))
			continue
		}
		receipt, e := maintenancePHP(ctx, branch)
		if e != nil {
			block(e)
			continue
		}
		item := MaintenanceConsumer{Branch: branch, Domain: s.Domain, Consumer: c, PHP: receipt, Units: []MaintenanceUnit{}}
		for instance := 1; instance <= c.Instances; instance++ {
			u, e := inspectMaintenanceUnitWithRetry(ctx, item, unitName(c.ID, instance), options)
			if e != nil {
				block(e)
				item.Units = nil
				break
			}
			item.Units = append(item.Units, u)
		}
		if item.Units != nil {
			set = append(set, item)
		}
	}
	return set, reasons, nil
}

func inspectMaintenanceUnit(ctx context.Context, c MaintenanceConsumer, name string, options MaintenanceInspection) (MaintenanceUnit, error) {
	var result MaintenanceUnit
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", name, "--property="+inspectedUnitProperties+",LoadState,UnitFileState")
	if err != nil {
		return result, err
	}
	p := unitProperties(out)
	result.Enabled = p["UnitFileState"]
	delete(p, "UnitFileState")
	if p["LoadState"] != "loaded" {
		return result, errors.New("PHP consumer service is not loaded")
	}
	delete(p, "LoadState")
	if result.Enabled != "enabled" && result.Enabled != "disabled" && result.Enabled != "static" {
		return result, errors.New("Unsupported PHP consumer enablement")
	}
	if options.Normalize != nil {
		p, err = options.Normalize(name, p)
		if err != nil {
			return result, err
		}
	}
	p, err = normalizeBarrierDefinition(name, p)
	if err != nil {
		return result, err
	}
	if err = validateUnitDefinition(c.Branch, &c.Consumer, p); err != nil {
		return result, err
	}
	if p["Id"] != name || p["FragmentPath"] != filepath.Join("/etc/systemd/system", name) {
		return result, errors.New("PHP consumer service identity changed")
	}
	if err = rootOwned(p["FragmentPath"]); err != nil {
		return result, err
	}
	u, err := observeUnit(ctx, name, "/system.slice/"+name)
	if err != nil {
		return result, err
	}
	if u.CGroup != "" && u.CGroup != "/system.slice/"+name {
		return result, errors.New("Unexpected PHP consumer cgroup")
	}
	u.Natural = isNaturalConsumer(&c.Consumer)
	result.Runtime = u // Preserve the observation needed to prove a child exited.
	if !options.Recovery && (u.State != "active" && u.State != "inactive" || u.State == "active" && !u.Active || !u.Active && (u.MainPID != 0 || len(u.PIDs) > 0)) {
		return result, errors.New("PHP consumer is failed or changing state; review again when settled")
	}
	if err = ValidateMaintenanceProcesses(ctx, c, &u, options.AdditionalProcess, options.Recovery); err != nil {
		return result, err
	}
	for key, value := range p {
		if strings.HasPrefix(key, "Exec") {
			p[key], _, _ = strings.Cut(value, "; start_time=")
		}
	}
	if !options.Recovery && u.Active && c.Consumer.Kind == "laravel_octane" {
		if err = VerifyMaintenanceHealth(ctx, c, u, options.AdditionalProcess); err != nil {
			return result, err
		}
	}
	result.Runtime, result.Definition = u, p
	return result, nil
}

func inspectMaintenanceUnitWithRetry(ctx context.Context, c MaintenanceConsumer, name string, options MaintenanceInspection) (MaintenanceUnit, error) {
	var result MaintenanceUnit
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		result, err = inspectMaintenanceUnit(ctx, c, name, options)
		if err == nil || !maintenanceChildDisappeared(result.Runtime, err) {
			return result, err
		}
		// Refresh only a recorded non-controller proven gone. Permissions,
		// controller replacement and provenance failures never take this path.
		if cancelled := ctx.Err(); cancelled != nil {
			return result, cancelled
		}
	}
	return result, err
}

// ReinspectMaintenanceUnit authenticates a late descendant against the same
// qualified interpreter, effective unit contract and controller/native anchors.
// It does not treat a matching cgroup name as proof of ownership.
func ReinspectMaintenanceUnit(ctx context.Context, c MaintenanceConsumer, old MaintenanceUnit, options MaintenanceInspection) (MaintenanceUnit, error) {
	if err := ValidateMaintenancePHP(ctx, []MaintenanceConsumer{c}); err != nil {
		return MaintenanceUnit{}, err
	}
	now, err := inspectMaintenanceUnitWithRetry(ctx, c, old.Runtime.Name, options)
	if err != nil {
		return now, err
	}
	if !sameMaintenanceUnitExecution(old, now) {
		return now, errors.New("PHP consumer configuration or controller changed during process inventory")
	}
	return now, nil
}

func sameMaintenanceUnitExecution(old, now MaintenanceUnit) bool {
	if old.Enabled != now.Enabled || digest(old.Definition) != digest(now.Definition) || old.Runtime.Natural != now.Runtime.Natural {
		return false
	}
	identity := func(u MaintenanceRuntime) string {
		anchors := map[int]string{}
		if u.MainPID > 0 {
			anchors[u.MainPID] = u.PIDs[u.MainPID]
		}
		if u.Octane != nil {
			for _, pid := range []int{u.Octane.MasterPID, u.Octane.ManagerPID} {
				if pid > 0 {
					anchors[pid] = u.PIDs[pid]
				}
			}
		}
		return digest(struct {
			Name, Restarts string
			Controller     int
			Native         *octaneProcesses
			Anchors        map[int]string
		}{u.Name, u.Restarts, u.MainPID, u.Octane, anchors})
	}
	return identity(old.Runtime) == identity(now.Runtime)
}

func ValidateMaintenanceProcesses(ctx context.Context, c MaintenanceConsumer, u *MaintenanceRuntime, additional func(MaintenanceRuntime, int) (bool, error), recovery bool) error {
	credential, err := syscmd.ResolveCredential("fluxo")
	if err != nil {
		return err
	}
	for pid := range u.PIDs {
		if err = validateProcessUserAndEnvironment(pid, credential.Uid); err != nil {
			return err
		}
		if err = processCurrent(pid); err != nil {
			return err
		}
	}
	filtered := *u
	filtered.PIDs = map[int]string{}
	for pid, key := range u.PIDs {
		accepted := false
		if pid != u.MainPID && additional != nil {
			var err error
			accepted, err = additional(*u, pid)
			if err != nil {
				return err
			}
		}
		if !accepted {
			filtered.PIDs[pid] = key
		}
	}
	// Recovery eligibility is identity/configuration based, not dependent on
	// an already healthy app. Any surviving PHP controller still gets checked.
	if u.MainPID > 0 {
		actual, e := os.Stat(fmt.Sprintf("/proc/%d/exe", u.MainPID))
		if e != nil {
			return e
		}
		expected, e := os.Stat(c.PHP.Path)
		if e != nil || !os.SameFile(actual, expected) {
			return errors.New("PHP controller executable changed")
		}
	}
	if recovery && !u.Active {
		if c.Consumer.Port > 0 {
			return verifyOptionalConsumerListener(ctx, *u, c.Consumer.Port)
		}
		return nil
	}
	if c.Consumer.Kind == "laravel_octane" && u.Active {
		native, err := inspectOctaneProcessesForMaintenance(ctx, c.Branch, c.Consumer, filtered, !recovery)
		if err != nil {
			return err
		}
		u.Octane = native
	} else if err := validateUnitProcesses(ctx, c.Branch, &c.Consumer, filtered); err != nil {
		return err
	}
	if c.Consumer.Kind == "laravel_nightwatch" && u.Active {
		if recovery {
			return verifyOptionalConsumerListener(ctx, *u, c.Consumer.Port)
		}
		return verifyListenerOwnership(ctx, *u, c.Consumer.Port)
	}
	return nil
}

func ObserveMaintenanceUnit(ctx context.Context, old MaintenanceRuntime) (MaintenanceRuntime, error) {
	return observeUnit(ctx, old.Name, old.CGroup)
}
func MaintenanceDrained(old, current MaintenanceRuntime) (bool, error) {
	return fullyDrained(old, current, processKey)
}
func SameMaintenanceConsumers(a, b []MaintenanceConsumer) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if len(x.Units) != len(y.Units) {
			return false
		}
		x.Units = append([]MaintenanceUnit(nil), x.Units...)
		y.Units = append([]MaintenanceUnit(nil), y.Units...)
		for j := range x.Units {
			x.Units[j].Runtime = unitSnapshot{Name: x.Units[j].Runtime.Name, Natural: x.Units[j].Runtime.Natural}
			y.Units[j].Runtime = unitSnapshot{Name: y.Units[j].Runtime.Name, Natural: y.Units[j].Runtime.Natural}
		}
		if digest(x) != digest(y) {
			return false
		}
	}
	return true
}

// SameMaintenanceExecution binds a reviewed invocation to its controller and
// native serving anchors while allowing fully authenticated job turnover.
func SameMaintenanceExecution(a, b []MaintenanceConsumer) bool {
	if !SameMaintenanceConsumers(a, b) {
		return false
	}
	for i := range a {
		for j := range a[i].Units {
			x, y := a[i].Units[j], b[i].Units[j]
			if x.Runtime.Active != y.Runtime.Active || !sameMaintenanceUnitExecution(x, y) {
				return false
			}
		}
	}
	return true
}

func ValidateMaintenancePHP(ctx context.Context, set []MaintenanceConsumer) error {
	for _, c := range set {
		receipt, err := maintenancePHP(ctx, c.Branch)
		if err != nil {
			return err
		}
		if receipt != c.PHP {
			return errors.New("PHP interpreter changed during Node.js maintenance")
		}
	}
	return nil
}

// Validate the complete protocol inventory and retained no-replay records before
// a caller installs holds or dispatches any unit.
func ValidateMaintenanceResumes(set []MaintenanceConsumer, resumes map[string]MaintenanceResume) error {
	known := map[string]bool{}
	for _, c := range set {
		if len(c.Units) != c.Consumer.Instances {
			return ErrPlanChanged
		}
		for i, u := range c.Units {
			name := unitName(c.Consumer.ID, i+1)
			if known[name] || u.Runtime.Name != name || u.Runtime.Natural != isNaturalConsumer(&c.Consumer) {
				return ErrPlanChanged
			}
			known[name] = u.Runtime.Natural && u.Runtime.Active
		}
	}
	for name, r := range resumes {
		if !known[name] {
			return ErrPlanChanged
		}
		if err := validateNaturalResume(r); err != nil {
			return err
		}
	}
	return nil
}

func VerifyMaintenanceHealth(ctx context.Context, c MaintenanceConsumer, u MaintenanceRuntime, additional func(MaintenanceRuntime, int) (bool, error)) error {
	_, err := maintenanceHealthSnapshot(ctx, c, u, additional)
	return err
}

func maintenanceHealthSnapshot(ctx context.Context, c MaintenanceConsumer, u MaintenanceRuntime, additional func(MaintenanceRuntime, int) (bool, error)) (MaintenanceRuntime, error) {
	observed := u
	inspect := func(snapshot unitSnapshot) error {
		observed = snapshot
		if err := ValidateMaintenanceProcesses(ctx, c, &snapshot, additional, false); err != nil {
			return err
		}
		observed = snapshot // Includes freshly verified native process roles.
		return nil
	}
	if c.Consumer.Kind != "laravel_octane" {
		err := inspect(u)
		return observed, err
	}
	err := octaneHealthWithInspector(ctx, c.Consumer, u, inspect)
	return observed, err
}

// ErrMaintenanceObservationChanged requires a fresh fully validated observation
// before readiness can pass. It is limited to a recorded non-controller PID proven gone;
// live replacements, permissions, stale binaries and other errors stay fatal.
var ErrMaintenanceObservationChanged = errors.New("Maintenance child exited during observation")

// MaintenanceHealthIdentity validates the entire observed process group and
// native/listener health, then returns the stable controller/serving identity.
// Transient job processes remain subject to all provenance checks.
func MaintenanceHealthIdentity(ctx context.Context, c MaintenanceConsumer, u MaintenanceRuntime, additional func(MaintenanceRuntime, int) (bool, error)) (string, error) {
	observed, err := maintenanceHealthSnapshot(ctx, c, u, additional)
	if err != nil {
		if maintenanceChildDisappeared(observed, err) {
			return "", fmt.Errorf("%w: %v", ErrMaintenanceObservationChanged, err)
		}
		return "", err
	}
	return maintenanceStabilityIdentity(c, observed)
}

// Descendants may turn over as jobs finish. Their full provenance/roles remain
// verified above, but stability belongs to the controller and native serving
// anchors. PID plus boot/start identity detects replacement and PID reuse.
func maintenanceStabilityIdentity(c MaintenanceConsumer, u MaintenanceRuntime) (string, error) {
	if !u.Active || u.MainPID <= 1 || u.PIDs[u.MainPID] == "" {
		return "", errors.New("PHP consumer has no verified active controller identity")
	}
	if _, err := strconv.ParseUint(u.Restarts, 10, 64); err != nil {
		return "", errors.New("PHP consumer restart evidence is missing")
	}
	anchors := map[int]string{u.MainPID: u.PIDs[u.MainPID]}
	var native *octaneProcesses
	if c.Consumer.Kind == "laravel_octane" {
		if u.Octane == nil || u.Octane.MasterPID <= 1 || u.PIDs[u.Octane.MasterPID] == "" {
			return "", errors.New("Octane has no verified native serving identity")
		}
		native = u.Octane
		anchors[u.Octane.MasterPID] = u.PIDs[u.Octane.MasterPID]
		if u.Octane.ManagerPID > 0 {
			if u.PIDs[u.Octane.ManagerPID] == "" {
				return "", errors.New("Octane native manager identity is missing")
			}
			anchors[u.Octane.ManagerPID] = u.PIDs[u.Octane.ManagerPID]
		}
	}
	return digest(struct {
		Unit, Restarts string
		Controller     int
		Native         *octaneProcesses
		Anchors        map[int]string
	}{u.Name, u.Restarts, u.MainPID, native, anchors}), nil
}

func maintenanceChildDisappeared(u MaintenanceRuntime, err error) bool {
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || !errors.Is(err, os.ErrNotExist) || !strings.HasPrefix(pathErr.Path, "/proc/") || filepath.Clean(pathErr.Path) != pathErr.Path {
		return false
	}
	parts := strings.SplitN(strings.TrimPrefix(pathErr.Path, "/proc/"), "/", 2)
	if len(parts) != 2 || parts[1] == "" {
		return false
	}
	pid, parseErr := strconv.Atoi(parts[0])
	if parseErr != nil || pid <= 1 || strconv.Itoa(pid) != parts[0] || pid == u.MainPID || u.PIDs[pid] == "" {
		return false
	}
	_, currentErr := processKey(pid)
	return errors.Is(currentErr, os.ErrNotExist)
}
