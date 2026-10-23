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
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/octaneruntime"
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
			reasons = append(reasons, fmt.Sprintf("Background process #%d (%q, kind %q): %v", c.ID, c.Name, c.Kind, e))
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
			u, e := inspectMaintenanceUnit(ctx, item, unitName(c.ID, instance), options)
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
	if c.Consumer.Kind != "laravel_octane" {
		return ValidateMaintenanceProcesses(ctx, c, &u, additional, false)
	}
	return octaneHealthWithInspector(ctx, c.Consumer, u, func(snapshot unitSnapshot) error {
		return ValidateMaintenanceProcesses(ctx, c, &snapshot, additional, false)
	})
}
