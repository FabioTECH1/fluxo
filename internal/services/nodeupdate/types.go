// Package nodeupdate coordinates explicitly approved, same-major Node updates.
package nodeupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fluxo/internal/services/nodetoolchain"
	"fluxo/internal/services/phpupdate"
)

var ErrBusy = errors.New("Runtime maintenance is in progress or requires recovery")
var ErrChanged = errors.New("The Node.js update review changed or expired; check and review again")
var ErrUnsupported = errors.New("This Node.js configuration requires manual maintenance")
var ErrSupervisor = errors.New("Node.js updates require an installed root-owned Fluxo binary and systemd")

const FreshSeconds = int64(24 * 60 * 60)
const ReviewSeconds = int64(15 * 60)

type Snapshot struct {
	LegacySource       *nodetoolchain.LegacySource `json:"legacy_source,omitempty"`
	MigrationAvailable bool                        `json:"migration_available"`
	CandidateID        string                      `json:"candidate_id"`
	State              string                      `json:"state"`
	CheckedAt          int64                       `json:"checked_at"`
	AttemptedAt        int64                       `json:"attempted_at"`
	Error              string                      `json:"error,omitempty"`
	DiscoveryID        string                      `json:"discovery_id"`
	Installed          nodetoolchain.OwnedRuntime  `json:"installed"`
	Candidate          *nodetoolchain.Release      `json:"candidate"`
	UpdateAvailable    bool                        `json:"update_available"`
	Notice             bool                        `json:"notice"`
}
type Operation struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	PlanID       string `json:"plan_id"`
	Status       string `json:"status"`
	Phase        string `json:"phase"`
	Message      string `json:"message"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
	Username     string `json:"username"`
	IPAddress    string `json:"ip_address"`
	GenerationID string `json:"generation_id,omitempty"`
}
type Site struct {
	ID      int    `json:"id"`
	Domain  string `json:"domain"`
	Mode    string `json:"mode"`
	AppType string `json:"app_type"`
}
type Unit struct {
	ID         int            `json:"id"`
	SiteID     int            `json:"site_id"`
	Domain     string         `json:"domain"`
	Name       string         `json:"name"`
	Command    string         `json:"command"`
	Directory  string         `json:"directory"`
	Port       int            `json:"port"`
	Active     bool           `json:"active"`
	Enabled    string         `json:"enabled"`
	Definition string         `json:"definition"`
	MainPID    int            `json:"main_pid"`
	PIDs       map[int]string `json:"pids"`
	BunOnly    bool           `json:"bun_only"`
	Bun        *BunRuntime    `json:"bun,omitempty"`
	// Recovery observations describe current health without replacing the saved
	// generation's running/stopped intent in Active.
	State           string `json:"state,omitempty"`
	SubState        string `json:"sub_state,omitempty"`
	ListenerPresent *bool  `json:"listener_present,omitempty"`
}
type Impact struct {
	PHPResumes     map[string]phpupdate.MaintenanceResume `json:"php_resumes,omitempty"`
	PHPPolicy      int                                    `json:"php_policy,omitempty"`
	PHP            []phpupdate.MaintenanceConsumer        `json:"php_consumers,omitempty"`
	Sites          []Site                                 `json:"sites"`
	Units          []Unit                                 `json:"units"`
	BlockedReasons []string                               `json:"blocked_reasons"`
}
type RetainedReleases struct {
	Before nodetoolchain.Release `json:"before"`
	After  nodetoolchain.Release `json:"after"`
}
type Plan struct {
	Migration      *nodetoolchain.LegacySource `json:"migration,omitempty"`
	ConsumerPolicy int                         `json:"consumer_policy"`
	ID             string                      `json:"id"`
	Kind           string                      `json:"kind"`
	DiscoveryID    string                      `json:"discovery_id"`
	CreatedAt      int64                       `json:"created_at"`
	GenerationID   string                      `json:"generation_id,omitempty"`
	Head           string                      `json:"head,omitempty"`
	Before         nodetoolchain.OwnedRuntime  `json:"before"`
	After          nodetoolchain.Release       `json:"after"`
	Impact         Impact                      `json:"impact"`
	Retained       *RetainedReleases           `json:"retained,omitempty"`
}
type Generation struct {
	MigrationTarget     *nodetoolchain.Release `json:"migration_target,omitempty"`
	MigrationStep       string                 `json:"migration_step,omitempty"`
	PHPState            *PHPState              `json:"php_state,omitempty"`
	ActivationAttempted bool                   `json:"activation_attempted,omitempty"`
	ID                  string                 `json:"id"`
	Head                string                 `json:"head"`
	State               string                 `json:"state"`
	Plan                Plan                   `json:"plan"`
	Boot                string                 `json:"boot"`
	Goal                string                 `json:"goal"`
	// Refreshed authentication is separate from the original immutable plan.
	Retained *RetainedReleases `json:"retained,omitempty"`
	// Fresh coordination evidence never rewrites the original reviewed history.
	Coordination *Impact `json:"coordination,omitempty"`
}
type Status struct {
	Snapshot        Snapshot     `json:"snapshot"`
	ActiveOperation *Operation   `json:"active_operation"`
	Operations      []Operation  `json:"operations"`
	Recoveries      []Generation `json:"recoveries"`
	Locked          bool         `json:"locked"`
}

func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
