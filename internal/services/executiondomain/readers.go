package executiondomain

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type AdapterID string

// LegacyAdapter is deliberately independent of the current host. Missing
// adapter identity in an old journal always means the original cgroup-exit
// contract, never the disabled 249 candidate or another selected backend.
func LegacyAdapter(id AdapterID) (AdapterID, error) {
	if id == "" || id == CgroupExitV1 {
		return AdapterID(CgroupExitV1), nil
	}
	return "", fmt.Errorf("unsupported execution adapter %q in legacy journal", id)
}

type ReaderCapabilities struct {
	Format            int                 `json:"format"`
	SchedulerSupport  int                 `json:"scheduler_support"`
	SystemTaskSupport int                 `json:"system_task_support"`
	SchedulerSchemas  []int               `json:"scheduler_schemas"`
	SystemTaskSchemas []int               `json:"system_task_schemas"`
	Adapters          []AdapterID         `json:"adapters"`
	RoleContracts     map[string][]string `json:"role_contracts"`
	Activation        bool                `json:"activation_available"`
}

// Capabilities reports compiled readers, not successful host qualification.
// Versioned core readers retain all legacy interpretations. The persistent-249
// candidate ID remains unsupported. Stable persistent selection is bounded to
// the reviewed Jammy profile; capability claims are not native acceptance.
func Capabilities() ReaderCapabilities {
	schemas := []int{1, 2, 3, 4}
	roles := map[string][]string{"scheduler-execute": {CgroupExitV1, PersistentV1}, "system-task-execute": {CgroupExitV1, PersistentV1}, "system-task-profile": {"apt-a0-noqueue-v3"}, "system-task-profile-reader": {"legacy-host-inputs-v1", "apt-a0-host-inputs-v2", "apt-a0-noqueue-v3"}, "scheduler-backend-reader": {"ubuntu-noble-cron-v1", "cronie-noble-1", "ubuntu-jammy-cron-v1", "ubuntu-resolute-cron-v1"}}
	if CurrentSchedulerSchema >= SchedulerLoaderSchema {
		schemas = append(schemas, CurrentSchedulerSchema)
		roles["scheduler-loader-reader"] = []string{SchedulerLoaderReaderContract}
		roles["scheduler-execute"] = append(roles["scheduler-execute"], SchedulerLoaderExecutionContract, SchedulerBootstrapContract)
	}
	roles[SchedulerHistoricalRetirementRole] = []string{SchedulerHistoricalRetirementContract}
	return ReaderCapabilities{Format: 1, SchedulerSupport: SchedulerCompiledReaderSupport, SystemTaskSupport: SystemTaskReaderSupport, SchedulerSchemas: schemas, SystemTaskSchemas: []int{1, 2, CurrentSystemTaskSchema}, Activation: true, Adapters: []AdapterID{CgroupExitV1, PersistentV1}, RoleContracts: roles}
}

// ReadFloor exposes protected compatibility metadata without initializing state.
// ParseReaderFloor validates wire meaning only. It never grants mutation or
// execution authority; callers still require compiled capabilities and scope.
func ParseReaderFloor(raw []byte) (ReaderFloor, error) { return decodeFloor(raw) }

func ReadFloor(dataDir string) (*ReaderFloor, error) {
	raw, err := readProtected(filepath.Join(dataDir, "execution-readers.json"), 64<<10)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f, err := decodeFloor(raw)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func InitialSchema(component, dataDir string) (int, error) {
	legacy, current := 2, CurrentSchedulerSchema
	if component == "system-task" {
		legacy, current = 1, CurrentSystemTaskSchema
	}
	f, err := ReadFloor(dataDir)
	if err != nil || f == nil {
		return legacy, err
	}
	if err = checkFloor(*f, Capabilities()); err != nil {
		return 0, err
	}
	if component == "system-task" && contains(Capabilities().SystemTaskSchemas, f.SystemTaskSchema) {
		return f.SystemTaskSchema, nil
	}
	if component == "scheduler" && contains(Capabilities().SchedulerSchemas, f.SchedulerSchema) {
		return f.SchedulerSchema, nil
	}
	_ = current
	return legacy, nil
}

// ReaderFloor is a restrictive compatibility requirement, never evidence that
// migration, host qualification or activation succeeded. This implementation
// uses it to gate ordinary writes; bridge activation owns its phase transition.
type ReaderFloor struct {
	Format            int                 `json:"format"`
	Phase             string              `json:"phase"`
	SchedulerSupport  int                 `json:"scheduler_support"`
	SystemTaskSupport int                 `json:"system_task_support"`
	SchedulerSchema   int                 `json:"scheduler_schema"`
	SystemTaskSchema  int                 `json:"system_task_schema"`
	Adapters          []AdapterID         `json:"adapters"`
	RoleContracts     map[string][]string `json:"role_contracts,omitempty"`
	ActivationID      string              `json:"activation_id,omitempty"`
	InstallationID    string              `json:"installation_id,omitempty"`
	BinarySHA256      string              `json:"binary_sha256,omitempty"`
	PlanSHA256        string              `json:"plan_sha256,omitempty"`
}

type JournalCompatibility struct {
	Component string `json:"component"`
	Schema    int    `json:"schema"`
}

type CompatibilityReport struct {
	Compatible   bool                   `json:"compatible"`
	Capabilities ReaderCapabilities     `json:"capabilities"`
	Floor        *ReaderFloor           `json:"minimum_readers,omitempty"`
	Journals     []JournalCompatibility `json:"journals"`
}

// InspectCompatibility is strictly read-only: no SQLite opens, configuration
// initialization, locks, directories or marker files. It checks format/reader
// compatibility only; subsystem readers still validate complete evidence.
func InspectCompatibility(dataDir string) (CompatibilityReport, error) {
	if !filepath.IsAbs(dataDir) || filepath.Clean(dataDir) != dataDir {
		return CompatibilityReport{Capabilities: Capabilities()}, errors.New("execution compatibility needs a canonical absolute data directory")
	}
	return inspectCompatibility(dataDir, readProtected)
}

func inspectCompatibility(dataDir string, read func(string, int64) ([]byte, error)) (CompatibilityReport, error) {
	r := CompatibilityReport{Capabilities: Capabilities(), Journals: []JournalCompatibility{}}
	floorPath := filepath.Join(dataDir, "execution-readers.json")
	raw, err := read(floorPath, 64<<10)
	if err == nil {
		floor, e := decodeFloor(raw)
		if e != nil {
			return r, fmt.Errorf("execution minimum-reader record: %w", e)
		}
		r.Floor = &floor
		if e = checkFloor(floor, r.Capabilities); e != nil {
			return r, e
		}
	} else if !os.IsNotExist(err) {
		return r, err
	}
	if r.Floor == nil {
		// Even before either core fence, a retained activation namespace is
		// operation-owned recovery evidence. Losing the floor cannot turn it
		// into permission to invent another activation UUID or empty baseline.
		if _, e := read(filepath.Join(dataDir, "execution-activations"), 1); !os.IsNotExist(e) {
			return r, errors.New("execution activation history exists without its minimum-reader floor; recover the exact original operation")
		}
	}
	for _, journal := range []struct {
		component string
		path      string
		limit     int64
		schemas   []int
	}{
		{"scheduler", "/var/lib/fluxo-scheduler/state.json", 32 << 20, r.Capabilities.SchedulerSchemas},
		{"system-task", "/var/lib/fluxo-system-tasks/state.json", 8 << 20, r.Capabilities.SystemTaskSchemas},
	} {
		raw, err = read(journal.path, journal.limit)

		if os.IsNotExist(err) {
			if r.Floor != nil && (journal.component == "scheduler" && r.Floor.SchedulerSchema >= 4 || journal.component == "system-task" && r.Floor.SystemTaskSchema >= 3) {
				return r, errors.New("committed execution contract lost its required core")
			}

			base := strings.TrimSuffix(journal.path, "/state.json")
			markers := []string{base + ".initialized", base}
			if journal.component == "scheduler" {
				markers = append(markers, filepath.Join(base, "gate.lock"))
			} else {
				markers = append(markers, base+".lock")
			}
			for _, marker := range markers {
				if _, e := read(marker, 128); !os.IsNotExist(e) {
					return r, errors.New("execution initialization is partial or damaged; use exact recovery before mutation")
				}
			}

			continue
		}
		if err != nil {
			return r, err
		}
		// Existing invalid intent markers must block admission even when an
		// old core still exists beside them. Missing historical markers retain
		// their historical interpretation; subsystem readers validate scope.
		base := strings.TrimSuffix(journal.path, "/state.json")
		if marker, e := read(base+".initialized", 128); e == nil {
			if string(marker) != "1\n" {
				return r, errors.New("execution initialization anchor is incomplete; recover the exact original operation")
			}
		} else if !os.IsNotExist(e) {
			return r, e
		}
		if journal.component == "system-task" {
			if phase, e := read(base+".lock", 128); e == nil {
				if len(phase) != 0 && string(phase) != "ready\n" {
					return r, errors.New("system-task initialization is incomplete; recover the exact original operation")
				}
			} else if !os.IsNotExist(e) {
				return r, e
			}
		}
		var header struct {
			Schema      int                        `json:"schema"`
			Entries     map[string]json.RawMessage `json:"entries"`
			Invocations map[string]json.RawMessage `json:"invocations"`
		}
		if err = json.Unmarshal(raw, &header); err != nil {
			return r, fmt.Errorf("invalid %s journal: %w", journal.component, err)
		}
		r.Journals = append(r.Journals, JournalCompatibility{Component: journal.component, Schema: header.Schema})
		if !contains(journal.schemas, header.Schema) {
			return r, fmt.Errorf("%s journal schema %d needs a compatible Fluxo reader before any initialization or mutation", journal.component, header.Schema)
		}
		firstExplicit := 3
		if journal.component == "system-task" {
			firstExplicit = 2
		}
		if header.Schema >= firstExplicit && r.Floor == nil {
			return r, errors.New("versioned execution journal lost its minimum-reader floor; use exact activation recovery")
		}
		if header.Schema >= firstExplicit && r.Floor != nil {
			schema, minSupport := r.Floor.SchedulerSchema, r.Floor.SchedulerSupport
			if journal.component == "system-task" {
				schema, minSupport = r.Floor.SystemTaskSchema, r.Floor.SystemTaskSupport
			}
			required := MinimumSchedulerReaderSupport(header.Schema)
			if journal.component == "system-task" {
				required = MinimumSystemTaskReaderSupport(header.Schema)
			}
			if schema < header.Schema || minSupport < required {
				return r, errors.New("versioned execution journal has an insufficient minimum-reader floor")
			}
		}
		if r.Floor != nil {
			minimum := r.Floor.SchedulerSchema
			if journal.component == "system-task" {
				minimum = r.Floor.SystemTaskSchema
			}
			if header.Schema < minimum {
				return r, errors.New("execution journal conversion is incomplete; preserve activation evidence")
			}
		}
		if header.Entries == nil || header.Invocations == nil {
			return r, fmt.Errorf("incomplete %s execution journal", journal.component)
		}

		if journal.component == "scheduler" {
			if err = inspectHistoricalRetirementWire(raw, header.Schema, r.Floor); err != nil {
				return r, err
			}
			if err = inspectSchedulerWire(raw, header.Schema, header.Invocations); err != nil {
				return r, err
			}
		}
		for name, rawInvocation := range header.Invocations {
			var fields map[string]json.RawMessage
			if err = json.Unmarshal(rawInvocation, &fields); err != nil || fields == nil {
				return r, fmt.Errorf("invalid %s invocation %s", journal.component, name)
			}
			var adapter AdapterID
			if value, ok := fields["adapter"]; ok {
				if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
					return r, errors.New("execution adapter identity cannot be null")
				}
				if err = json.Unmarshal(value, &adapter); err != nil {
					return r, err
				}
			}
			if err = ValidateAdapter(header.Schema, firstExplicit, adapter); err != nil {
				return r, err
			}
		}
	}
	r.Compatible = true
	return r, nil
}

func decodeFloor(raw []byte) (ReaderFloor, error) {
	var f ReaderFloor
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return f, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return f, errors.New("unexpected trailing minimum-reader data")
	}
	if f.Format != 1 || f.Phase != "prepared" && f.Phase != "committed" || f.SchedulerSupport < 1 || f.SystemTaskSupport < 1 || f.SchedulerSchema < 1 || f.SystemTaskSchema < 1 || len(f.Adapters) == 0 {
		return f, errors.New("incomplete or unknown execution minimum-reader record")
	}
	if f.SchedulerSupport >= 4 || f.SystemTaskSupport >= 2 || f.SchedulerSchema >= 3 || f.SystemTaskSchema >= 2 {
		if f.SchedulerSchema >= 3 && f.SchedulerSupport < 4 || f.SystemTaskSchema >= 2 && f.SystemTaskSupport < 2 || len(f.RoleContracts["system-task-profile"]) == 0 || len(f.RoleContracts["system-task-execute"]) == 0 || len(f.RoleContracts["scheduler-execute"]) == 0 {
			return f, errors.New("versioned reader floor is missing its minimum execution/profile contracts")
		}

		if f.SchedulerSupport < MinimumSchedulerReaderSupport(f.SchedulerSchema) || f.SystemTaskSupport < MinimumSystemTaskReaderSupport(f.SystemTaskSchema) {
			return f, errors.New("current execution floor understates reader support")
		}
		if (f.SchedulerSchema >= 4 || f.SystemTaskSchema >= 3) && len(f.RoleContracts["scheduler-backend-reader"]) == 0 {
			return f, errors.New("current execution floor lost its exact backend reader contracts")
		}
		if f.SchedulerSupport >= 7 && !floorRoleContains(f, SchedulerHistoricalRetirementRole, SchedulerHistoricalRetirementContract) {
			return f, errors.New("reader7 floor lost historical retirement contract")
		}
		if f.SchedulerSchema >= SchedulerLoaderSchema && (!floorRoleContains(f, "scheduler-loader-reader", SchedulerLoaderReaderContract) || !floorRoleContains(f, "scheduler-execute", SchedulerLoaderExecutionContract) || !floorRoleContains(f, "scheduler-execute", SchedulerBootstrapContract)) {
			return f, errors.New("schema5 floor lost its loader/bootstrap execution contracts")
		}
		id, err := hex.DecodeString(f.InstallationID)
		image, imageErr := hex.DecodeString(f.BinarySHA256)
		plan, planErr := hex.DecodeString(f.PlanSHA256)
		_, operationErr := uuid.Parse(f.ActivationID)
		if err != nil || len(id) != 16 || imageErr != nil || len(image) != 32 || planErr != nil || len(plan) != 32 || operationErr != nil || strings.ToLower(f.InstallationID) != f.InstallationID || strings.ToLower(f.BinarySHA256) != f.BinarySHA256 || strings.ToLower(f.PlanSHA256) != f.PlanSHA256 {
			return f, errors.New("versioned reader floor has no valid activation/image/installation/plan identity")
		}
	}

	seen := map[AdapterID]bool{}
	for _, id := range f.Adapters {
		if id == "" || seen[id] {
			return f, errors.New("invalid or duplicate execution adapter requirement")
		}
		seen[id] = true
	}
	for role, contracts := range f.RoleContracts {
		if role == "" || len(contracts) == 0 {
			return f, errors.New("incomplete role contract requirement")
		}
		seen := map[string]bool{}
		for _, contract := range contracts {
			if contract == "" || seen[contract] {
				return f, errors.New("invalid or duplicate role contract requirement")
			}
			seen[contract] = true
		}
	}
	return f, nil
}

func checkFloor(f ReaderFloor, c ReaderCapabilities) error {
	if f.SchedulerSupport > c.SchedulerSupport || f.SystemTaskSupport > c.SystemTaskSupport || !contains(c.SchedulerSchemas, f.SchedulerSchema) || !contains(c.SystemTaskSchemas, f.SystemTaskSchema) {
		return errors.New("installed execution evidence requires a newer compatible Fluxo reader")
	}
	for _, wanted := range f.Adapters {
		found := false
		for _, supported := range c.Adapters {
			found = found || wanted == supported
		}
		if !found {
			return fmt.Errorf("installed execution evidence requires unsupported adapter %q", wanted)
		}
	}
	for role, contracts := range f.RoleContracts {
		for _, contract := range contracts {
			found := false
			for _, supported := range c.RoleContracts[role] {
				found = found || contract == supported
			}
			if contract == "" || !found {
				return fmt.Errorf("installed execution evidence requires unsupported %s contract %q", role, contract)
			}
		}
	}
	if f.Phase != "committed" {
		return errors.New("execution reader activation is incomplete; preserve its evidence and use compatible explicit recovery")
	}
	return nil
}

func contains(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func RequireCapabilities(have, need ReaderCapabilities) error {
	if have.Format != 1 || have.SchedulerSupport < need.SchedulerSupport || have.SystemTaskSupport < need.SystemTaskSupport {
		return errors.New("helper execution reader protocol is incompatible")
	}
	for _, schema := range need.SchedulerSchemas {
		if !contains(have.SchedulerSchemas, schema) {
			return errors.New("helper cannot read required Scheduler schema")
		}
	}
	for _, schema := range need.SystemTaskSchemas {
		if !contains(have.SystemTaskSchemas, schema) {
			return errors.New("helper cannot read required system-task schema")
		}
	}
	for _, adapter := range need.Adapters {
		found := false
		for _, available := range have.Adapters {
			found = found || adapter == available
		}
		if !found {
			return errors.New("helper execution adapter is incompatible")
		}
	}
	for role, contracts := range need.RoleContracts {
		for _, contract := range contracts {
			found := false
			for _, available := range have.RoleContracts[role] {
				found = found || available == contract
			}
			if !found {
				return fmt.Errorf("refresh helper: required %s contract %s is unavailable", role, contract)
			}
		}
	}
	return nil
}

func readProtected(path string, limit int64) ([]byte, error) {
	// Validate existing ancestors even when the leaf is absent. A symlink must
	// not turn a missing floor into permission to initialize unrelated state.
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		var st unix.Stat_t
		if err := unix.Lstat(parent, &st); err != nil {
			if !errors.Is(err, unix.ENOENT) {
				return nil, err
			}
		} else if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Mode&0022 != 0 {
			return nil, fmt.Errorf("unsafe execution evidence directory: %s", parent)
		}
		if parent == "/" {
			break
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		var before unix.Stat_t
		if err := unix.Lstat(path, &before); err != nil {
			return nil, err
		}
		if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != 0 || before.Mode&0022 != 0 || before.Nlink != 1 || before.Size > limit {
			return nil, fmt.Errorf("unsafe execution evidence file: %s", path)
		}
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, err
		}
		f := os.NewFile(uintptr(fd), path)
		var opened, after, named unix.Stat_t
		err = unix.Fstat(fd, &opened)
		if err != nil || !sameFileObservation(before, opened) {
			f.Close()
			if err != nil {
				return nil, err
			}
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(f, limit+1))
		if err == nil {
			err = unix.Fstat(fd, &after)
		}
		f.Close()
		if err != nil {
			return nil, err
		}
		if len(raw) > int(limit) {
			return nil, errors.New("execution evidence exceeds its read budget")
		}
		if err = unix.Lstat(path, &named); err != nil {
			return nil, err
		}
		if sameFileObservation(before, after) && sameFileObservation(before, named) {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("execution evidence changed during inspection: %s", path)
}

func sameFileObservation(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

// IsPureMetadata recognizes only forms that unconditionally return before any
// mutable command. In particular --version=false is not a metadata bypass.
func IsPureMetadata(args []string) bool {
	if len(args) != 1 {
		return false
	}
	name := strings.TrimPrefix(strings.TrimPrefix(args[0], "-"), "-")
	if strings.HasSuffix(name, "=true") {
		name = strings.TrimSuffix(name, "=true")
	}
	switch name {
	case "nginx-http-defaults-recovery-ready", "scheduler-required-support", "system-task-required-support", "execution-capabilities", "bridge-support", "bridge-contract", "scheduler-support", "system-task-support", "installer-execution-support", "installer-scheduler-support", "installer-runtime-recovery-support", "node-migration-support", "node-update-support", "php-repository-support", "supports-node-toolchain", "supports-python-toolchain", "installer-tool-versions", "version", "h", "help":
		return strings.HasPrefix(args[0], "-")
	}
	return false
}

// These historical minima are immutable even when the current schema advances.
func MinimumSchedulerReaderSupport(schema int) int {
	if schema >= SchedulerLoaderSchema {
		return SchedulerLoaderSupport
	}
	if schema == 4 {
		return 5
	}
	if schema == 3 {
		return 4
	}
	return 1
}
func MinimumSystemTaskReaderSupport(schema int) int {
	if schema >= 3 {
		return 3
	}
	if schema == 2 {
		return 2
	}
	return 1
}
