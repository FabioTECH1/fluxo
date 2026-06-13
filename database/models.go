// Domain model types that map directly to SQLite table rows.
// All types include JSON struct tags for API serialization.
// The User.TokenHash field is tagged json:"-" to prevent the
// hashed password from ever being exposed in API responses.
package database

import "time"

type Site struct {
	ID                 int       `json:"id"`
	Domain             string    `json:"domain"`
	Path               string    `json:"path"`
	Repository         string    `json:"repository"`
	Branch             string    `json:"branch"`
	PHPVersion         string    `json:"php_version"`
	AppType            string    `json:"app_type"`
	AppPort            int       `json:"app_port"`
	DeploymentStrategy string    `json:"deployment_strategy"`
	SSLProvider        string    `json:"ssl_provider"`
	SSLActive          bool      `json:"ssl_active"`
	WebRoot            string    `json:"web_root"`
	PushToDeploy       bool      `json:"push_to_deploy"`
	DeployScript       string    `json:"deploy_script"`
	ExposeEnv          bool      `json:"expose_env"`
	DBEngine           string    `json:"db_engine"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type Deployment struct {
	ID            int       `json:"id"`
	SiteID        int       `json:"site_id"`
	CommitHash    string    `json:"commit_hash"`
	CommitMessage string    `json:"commit_message"`
	Branch        string    `json:"branch"`
	TriggerSource string    `json:"trigger_source"` // manual, github_webhook
	Status        string    `json:"status"`         // pending, running, success, failed
	Output        string    `json:"output"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Daemon struct {
	ID           int       `json:"id"`
	SiteID       int       `json:"site_id"`
	Name         string    `json:"name"`
	Command      string    `json:"command"`
	Directory    string    `json:"directory"`
	User         string    `json:"user"`
	Instances    int       `json:"instances"`
	Status       string    `json:"status"`
	StartSeconds int       `json:"start_seconds"`
	StopSeconds  int       `json:"stop_seconds"`
	StopSignal   string    `json:"stop_signal"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Cron struct {
	ID         int       `json:"id"`
	SiteID     int       `json:"site_id"`
	Name       string    `json:"name"`
	Expression string    `json:"expression"`
	Command    string    `json:"command"`
	User       string    `json:"user"`
	CreatedAt  time.Time `json:"created_at"`
}

type Database struct {
	ID        int       `json:"id"`
	SiteID    int       `json:"site_id"`
	Engine    string    `json:"engine"`
	Name      string    `json:"name"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

type User struct {
	ID            int       `json:"id"`
	Username      string    `json:"username"`
	TokenHash     string    `json:"-"` // Store hashed token, never return in JSON
	GitHubPAT     string    `json:"github_pat"`
	AdminEmail    string    `json:"admin_email"`
	DefaultPHP    string    `json:"default_php"`
	WebhookSecret string    `json:"webhook_secret"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type SSHKey struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	PublicKey string    `json:"public_key"`
	CreatedAt time.Time `json:"created_at"`
}

type FirewallRule struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	RuleType  string    `json:"type"`
	Port      string    `json:"port"`
	FromIP    string    `json:"from_ip"`
	CreatedAt time.Time `json:"created_at"`
}

type Command struct {
	ID        int       `json:"id"`
	SiteID    int       `json:"site_id"`
	Command   string    `json:"command"`
	Status    string    `json:"status"`
	Output    string    `json:"output"`
	CreatedAt time.Time `json:"created_at"`
}

type DomainAlias struct {
	ID        int       `json:"id"`
	SiteID    int       `json:"site_id"`
	Domain    string    `json:"domain"`
	CreatedAt time.Time `json:"created_at"`
}

type Activity struct {
	ID        int       `json:"id"`
	SiteID    int       `json:"site_id"`
	Type      string    `json:"type"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}
