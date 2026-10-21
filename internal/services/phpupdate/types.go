// Package phpupdate implements manually approved, branch-scoped PHP maintenance.
package phpupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fluxo/internal/services/scheduler"
	"regexp"
	"time"
)

var branchPattern = regexp.MustCompile(`^[0-9]{1,2}\.[0-9]{1,2}$`)

func ValidBranch(s string) bool { return branchPattern.MatchString(s) }

const FreshFor = 24 * time.Hour
const ReviewFreshFor = 15 * time.Minute

type Origin struct {
	Origin    string `json:"origin"`
	Label     string `json:"label"`
	Archive   string `json:"archive"`
	Component string `json:"component"`
	Site      string `json:"site"`
	Trusted   bool   `json:"trusted"`
}
type Package struct {
	PreservedExtension     bool            `json:"preserved_extension,omitempty"`
	Name                   string          `json:"name"`
	Architecture           string          `json:"architecture"`
	Installed              string          `json:"installed"`
	Candidate              string          `json:"candidate"`
	Source                 string          `json:"source"`
	SourceVersion          string          `json:"source_version"`
	CandidateSource        string          `json:"candidate_source"`
	CandidateSourceVersion string          `json:"candidate_source_version"`
	SHA256                 string          `json:"sha256"`
	Size                   int64           `json:"size"`
	Origins                []Origin        `json:"origins"`
	Held                   bool            `json:"held"`
	Automatic              bool            `json:"automatic"`
	Configured             bool            `json:"configured"`
	InstalledArchive       *PackageArchive `json:"installed_archive,omitempty"`
}

// Archive metadata comes from authenticated repository indexes, not from the
// package's locally installed files. Recovery rechecks the bytes and control data.
type PackageArchive struct {
	Name         string            `json:"name"`
	Architecture string            `json:"architecture"`
	Version      string            `json:"version"`
	SHA256       string            `json:"sha256"`
	Size         int64             `json:"size"`
	Control      map[string]string `json:"control"`
	Origins      []Origin          `json:"origins"`
}
type RollbackCapability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}
type Inventory struct {
	Version        string    `json:"version"`
	Packages       []Package `json:"packages"`
	Changes        []Package `json:"changes"`
	BlockedReasons []string  `json:"blocked_reasons"`
	PolicyHash     string    `json:"policy_hash"`
}
type Site struct {
	ID       int    `json:"id"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Strategy string `json:"strategy"`
	AppType  string `json:"app_type"`
	Port     int    `json:"port"`
}
type Consumer struct {
	ID          int               `json:"id"`
	SiteID      int               `json:"site_id"`
	Name        string            `json:"name"`
	Kind        string            `json:"kind"`
	Command     string            `json:"command"`
	Directory   string            `json:"directory"`
	Instances   int               `json:"instances"`
	StopSeconds int               `json:"stop_seconds"`
	Octane      *OctaneDescriptor `json:"octane,omitempty"`
	Port        int               `json:"port,omitempty"`
}
type Impact struct {
	Schedulers     []scheduler.Entry `json:"schedulers,omitempty"`
	Sites          []Site            `json:"sites"`
	Workers        []Consumer        `json:"workers"`
	ConfigHash     string            `json:"config_hash"`
	UnitHash       string            `json:"unit_hash"`
	PHPMyAdmin     bool              `json:"phpmyadmin"`
	BlockedReasons []string          `json:"blocked_reasons"`
	Notes          []string          `json:"notes,omitempty"`
}
type Plan struct {
	ParentPlanID string             `json:"parent_plan_id,omitempty"`
	Reviewed     bool               `json:"reviewed"`
	ID           string             `json:"id"`
	Version      string             `json:"version"`
	CandidateID  string             `json:"candidate_id"`
	CreatedAt    int64              `json:"created_at"`
	Inventory    Inventory          `json:"inventory"`
	Impact       Impact             `json:"impact"`
	Rollback     RollbackCapability `json:"rollback"`
}
type Snapshot struct {
	Version         string   `json:"version"`
	State           string   `json:"state"`
	CheckedAt       int64    `json:"checked_at"`
	AttemptedAt     int64    `json:"attempted_at"`
	Error           string   `json:"error,omitempty"`
	Plan            *Plan    `json:"plan,omitempty"`
	UpdateAvailable bool     `json:"update_available"`
	Notice          bool     `json:"notice"`
	BlockedReasons  []string `json:"blocked_reasons"`
}
type Operation struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Version    string          `json:"version"`
	PlanID     string          `json:"plan_id"`
	Status     string          `json:"status"`
	Phase      string          `json:"phase"`
	Message    string          `json:"message"`
	Output     string          `json:"output"`
	Username   string          `json:"username"`
	IPAddress  string          `json:"ip_address"`
	CreatedAt  int64           `json:"created_at"`
	UpdatedAt  int64           `json:"updated_at"`
	StartedAt  int64           `json:"started_at"`
	FinishedAt int64           `json:"finished_at"`
	Checkpoint string          `json:"checkpoint"`
	Result     json.RawMessage `json:"result"`
}

func (o Operation) Active() bool { return o.Status == "queued" || o.Status == "running" }
func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
