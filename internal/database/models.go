// Domain model types mapping to SQLite table rows with JSON tags for API serialization.
package database

import "time"

type Certificate struct {
	ID                  int      `json:"id"`
	SiteID              int      `json:"site_id"`
	Domain              string   `json:"domain"`
	Provider            string   `json:"provider"`
	CertPath            string   `json:"cert_path"`
	KeyPath             string   `json:"key_path"`
	Active              bool     `json:"active"`
	ExpiresAt           string   `json:"expires_at"`
	SourceCertificateID int      `json:"source_certificate_id"`
	CreatedAt           string   `json:"created_at"`
	ActiveDomains       []string `json:"active_domains,omitempty"`
	CoveredDomains      []string `json:"covered_domains,omitempty"`
}

type CertificateDomainBinding struct {
	SiteID        int    `json:"site_id"`
	Domain        string `json:"domain"`
	CertificateID int    `json:"certificate_id"`
	Provider      string `json:"provider"`
	Origin        string `json:"origin"`
	CertPath      string `json:"-"`
	KeyPath       string `json:"-"`
}

type CertificateCleanup struct {
	ID                  int
	CertificateID       int
	FormerSiteID        int
	Domain              string
	Provider            string
	CertPath            string
	KeyPath             string
	SourceCertificateID int
	CleanupStatus       string
	CleanupError        string
	CleanupAttempts     int
}

type CloneableCertificate struct {
	Certificate
	SiteDomain string `json:"site_domain"`
}

type Site struct {
	ID                  int       `json:"id"`
	Domain              string    `json:"domain"`
	Path                string    `json:"path"`
	Repository          string    `json:"repository"`
	Branch              string    `json:"branch"`
	PHPVersion          string    `json:"php_version"`
	AppType             string    `json:"app_type"`
	AppPort             int       `json:"app_port"`
	NodePreset          string    `json:"node_preset"`
	NodeMode            string    `json:"node_mode"`
	PackageManager      string    `json:"package_manager"`
	BuildCommand        string    `json:"build_command"`
	StartCommand        string    `json:"start_command"`
	StaticOutputDir     string    `json:"static_output_dir"`
	DeploymentStrategy  string    `json:"deployment_strategy"`
	SSLProvider         string    `json:"ssl_provider"`
	SSLActive           bool      `json:"ssl_active"`
	WebRoot             string    `json:"web_root"`
	PushToDeploy        bool      `json:"push_to_deploy"`
	DeployScript        string    `json:"deploy_script"`
	DeployScriptMode    string    `json:"deploy_script_mode"`
	ExposeEnv           bool      `json:"expose_env"`
	DBEngine            string    `json:"db_engine"`
	DeletionStatus      string    `json:"deletion_status"`
	DeletionError       string    `json:"deletion_error"`
	DeletionStage       string    `json:"deletion_stage"`
	DeletionDeleteDBs   bool      `json:"deletion_delete_databases"`
	DeletionDatabaseIDs string    `json:"deletion_database_ids"`
	GithubDeployKeyID   int64     `json:"-"` // never exposed to API
	GithubWebhookID     int64     `json:"-"` // never exposed to API
	GithubAccountID     int       `json:"github_account_id"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type Deployment struct {
	ID               int       `json:"id"`
	SiteID           int       `json:"site_id"`
	CommitHash       string    `json:"commit_hash"`
	CommitMessage    string    `json:"commit_message"`
	CommitAuthor     string    `json:"commit_author"`
	Branch           string    `json:"branch"`
	TriggerSource    string    `json:"trigger_source"`     // manual, github_webhook, rollback
	Status           string    `json:"status"`             // pending, running, success, failed
	TargetCommitHash string    `json:"target_commit_hash"` // set for rollbacks
	Output           string    `json:"output"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Daemon struct {
	ID              int       `json:"id"`
	SiteID          int       `json:"site_id"`
	Name            string    `json:"name"`
	Command         string    `json:"command"`
	Directory       string    `json:"directory"`
	User            string    `json:"user"`
	Instances       int       `json:"instances"`
	Status          string    `json:"status"`
	StartSeconds    int       `json:"start_seconds"`
	StopSeconds     int       `json:"stop_seconds"`
	StopSignal      string    `json:"stop_signal"`
	RestartOnDeploy bool      `json:"restart_on_deploy"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
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

type BackupDestination struct {
	ID              int       `json:"id"`
	Name            string    `json:"name"`
	Provider        string    `json:"provider"`
	Bucket          string    `json:"bucket"`
	Region          string    `json:"region"`
	AccountID       string    `json:"account_id"`
	Jurisdiction    string    `json:"jurisdiction"`
	Prefix          string    `json:"prefix"`
	ServerID        string    `json:"server_id"`
	AccessKey       string    `json:"-"`
	SecretKey       string    `json:"-"`
	UseInstanceRole bool      `json:"use_instance_role"`
	IsDefault       bool      `json:"is_default"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type BackupPlan struct {
	ID               int        `json:"id"`
	Name             string     `json:"name"`
	SiteID           int        `json:"site_id"`
	SiteDomain       string     `json:"site_domain"`
	DestinationID    int        `json:"destination_id"`
	DestinationName  string     `json:"destination_name"`
	IncludeFiles     bool       `json:"include_files"`
	DatabaseIDs      []int      `json:"database_ids"`
	Schedule         string     `json:"schedule"`
	BackupHour       int        `json:"backup_hour"`
	RetentionProfile string     `json:"retention_profile"`
	Enabled          bool       `json:"enabled"`
	NextRunAt        *time.Time `json:"next_run_at"`
	LastRunAt        *time.Time `json:"last_run_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type BackupArtifact struct {
	ID              int       `json:"id"`
	RunID           string    `json:"run_id"`
	Kind            string    `json:"kind"`
	DatabaseID      int       `json:"database_id"`
	DatabaseName    string    `json:"database_name"`
	Engine          string    `json:"engine"`
	ObjectKey       string    `json:"-"`
	ObjectVersionID string    `json:"-"`
	Filename        string    `json:"filename"`
	SizeBytes       int64     `json:"size_bytes"`
	SHA256          string    `json:"sha256"`
	CreatedAt       time.Time `json:"created_at"`
}

type BackupRun struct {
	ID                string           `json:"id"`
	PlanID            int              `json:"plan_id"`
	PlanName          string           `json:"plan_name"`
	DestinationID     int              `json:"destination_id"`
	DestinationName   string           `json:"destination_name"`
	SiteID            int              `json:"site_id"`
	SiteDomain        string           `json:"site_domain"`
	Trigger           string           `json:"trigger"`
	Status            string           `json:"status"`
	TotalSizeBytes    int64            `json:"total_size_bytes"`
	ManifestKey       string           `json:"-"`
	ManifestVersionID string           `json:"-"`
	Error             string           `json:"error"`
	StartedAt         *time.Time       `json:"started_at"`
	CompletedAt       *time.Time       `json:"completed_at"`
	CreatedAt         time.Time        `json:"created_at"`
	Artifacts         []BackupArtifact `json:"artifacts"`
}

type User struct {
	ID            int       `json:"id"`
	Username      string    `json:"username"`
	TokenHash     string    `json:"-"` // hashed token, never returned in JSON
	GitHubPAT     string    `json:"github_pat"`
	AdminEmail    string    `json:"admin_email"`
	DefaultPHP    string    `json:"default_php"`
	WebhookSecret string    `json:"webhook_secret"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type GitHubAccount struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Username  string    `json:"username"`
	Token     string    `json:"-"` // encrypted, never exposed in JSON
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
	Username  string    `json:"username"`
	IPAddress string    `json:"ip_address"`
	CreatedAt time.Time `json:"created_at"`
}
