package store

import (
	"database/sql"
	"encoding/json"
	"time"
)

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	OrgID       string    `json:"org_id"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type App struct {
	ID                   string    `json:"id"`
	ProjectID            string    `json:"project_id"`
	Name                 string    `json:"name"`
	DeployType           string    `json:"deploy_type"` // "image", "git", "compose"
	Image                string    `json:"image"`
	GitRepo              string    `json:"git_repo"`
	GitBranch            string    `json:"git_branch"`
	DockerfilePath       string    `json:"dockerfile_path"`
	Domain               string    `json:"domain"`
	Port                 int       `json:"port"`
	Replicas             int       `json:"replicas"`
	EnvEncrypted         []byte    `json:"env_encrypted"`
	Status               string    `json:"status"` // "pending", "deploying", "running", "stopped", "failed"
	CPULimit             float64   `json:"cpu_limit"`
	MemoryLimit          int64     `json:"memory_limit"`
	HealthCheckPath      string    `json:"health_check_path"`
	HealthCheckInterval  int       `json:"health_check_interval"`
	HomepageLabels       json.RawMessage `json:"homepage_labels"`
	ExtraLabels          json.RawMessage `json:"extra_labels"`
	PlacementConstraints json.RawMessage `json:"placement_constraints"`
	PlacementPreferences json.RawMessage `json:"placement_preferences"`
	UpdateStrategy       string    `json:"update_strategy"`
	UpdateParallelism    int       `json:"update_parallelism"`
	UpdateDelay          string    `json:"update_delay"`
	UpdateFailureAction  string    `json:"update_failure_action"`
	UpdateOrder          string    `json:"update_order"`
	BuildCacheEnabled    bool      `json:"build_cache_enabled"`
	AutoDeployBranch     string    `json:"auto_deploy_branch"`
	PreviewEnvironments  bool      `json:"preview_environments"`
	TemplateName         string    `json:"template_name"`
	TemplateVersion      string    `json:"template_version"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type TemplateSource struct {
	ID           string       `json:"id"`
	OrgID        string       `json:"org_id"`
	Name         string       `json:"name"`
	URL          string       `json:"url"`
	Type         string       `json:"type"`
	LastSyncedAt sql.NullTime `json:"last_synced_at"`
	CreatedAt    time.Time    `json:"created_at"`
}

type CustomTemplate struct {
	ID             string    `json:"id"`
	OrgID          string    `json:"org_id"`
	SourceID       string    `json:"source_id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Category       string    `json:"category"`
	Icon           string    `json:"icon"`
	Image          string    `json:"image"`
	Version        string    `json:"version"`
	Ports          string    `json:"ports"`
	Env            string    `json:"env"`
	Volumes        string    `json:"volumes"`
	Domain         string    `json:"domain"`
	Replicas       int       `json:"replicas"`
	IsStack        bool      `json:"is_stack"`
	ComposeContent string    `json:"compose_content"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Deployment struct {
	ID          string       `json:"id"`
	AppID       string       `json:"app_id"`
	Status      string       `json:"status"` // "building", "deploying", "success", "failed"
	CommitSHA   string       `json:"commit_sha"`
	ImageDigest string       `json:"image_digest"`
	Logs        string       `json:"logs"`
	StartedAt   time.Time    `json:"started_at"`
	FinishedAt  sql.NullTime `json:"finished_at"`
}

type ManagedDatabase struct {
	ID                  string    `json:"id"`
	ProjectID           string    `json:"project_id"`
	Name                string    `json:"name"`
	DBType              string    `json:"db_type"` // "postgres", "mysql", "redis", "mongo"
	Version             string    `json:"version"`
	Status              string    `json:"status"`
	ConnectionEncrypted []byte    `json:"connection_encrypted"`
	CreatedAt           time.Time `json:"created_at"`
}

type GitSource struct {
	ID                     string            `json:"id"`
	OrgID                  string            `json:"org_id"`
	Provider               string            `json:"provider"` // "github", "gitlab", "gitea"
	ProviderName           string            `json:"provider_name"`
	TokenEncrypted         []byte            `json:"token_encrypted"`
	WebhookSecretEncrypted []byte            `json:"-"`
	WebhookIDs             map[string]string `json:"-"` // repo full_name -> webhook_id
	CreatedAt              time.Time         `json:"created_at"`
}

type BackupConfig struct {
	ID         string    `json:"id"`
	ResourceID string    `json:"resource_id"`
	Schedule   string    `json:"schedule"`
	S3Bucket   string    `json:"s3_bucket"`
	S3Prefix   string    `json:"s3_prefix"`
	BackupType string    `json:"backup_type"`
	VolumeID   string    `json:"volume_id"`
	CreatedAt  time.Time `json:"created_at"`
}

type BackupRun struct {
	ID         string       `json:"id"`
	ConfigID   string       `json:"config_id"`
	Status     string       `json:"status"`
	Size       int64        `json:"size"`
	TargetPath string       `json:"target_path"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt sql.NullTime `json:"finished_at"`
}

type AuditLog struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	OrgID      string    `json:"org_id"`
	Action     string    `json:"action"`
	Resource   string    `json:"resource"`
	ResourceID string    `json:"resource_id"`
	Details    string    `json:"details"`
	CreatedAt  time.Time `json:"created_at"`
}

type Secret struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	Name           string    `json:"name"`
	DockerSecretID string    `json:"docker_secret_id"`
	Description    string    `json:"description"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Volume struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	Name          string    `json:"name"`
	Driver        string    `json:"driver"`
	DriverOpts    json.RawMessage `json:"driver_opts"`
	Labels        json.RawMessage `json:"labels"`
	MountType     string    `json:"mount_type"`
	RemoteHost    string    `json:"remote_host"`
	RemotePath    string    `json:"remote_path"`
	MountOptions  string    `json:"mount_options"`
	Scope         string    `json:"scope"`
	Status        string    `json:"status"`
	StorageHostID string    `json:"storage_host_id"`
	LocalPath     string    `json:"local_path"`
	CephPool      string    `json:"ceph_pool"`
	CephImage     string    `json:"ceph_image"`
	CephFSName    string    `json:"ceph_fs_name"`
	CreatedAt     time.Time `json:"created_at"`
}

type StorageHost struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	NodeID              string    `json:"node_id"`
	Address             string    `json:"address"`
	Type                string    `json:"type"`
	DefaultExportPath   string    `json:"default_export_path"`
	DefaultMountType    string    `json:"default_mount_type"`
	MountOptionsDefault string    `json:"mount_options_default"`
	CredentialsEncrypted []byte   `json:"-"`
	Capabilities        json.RawMessage `json:"capabilities"`
	NodeLabel           string    `json:"node_label"`
	Status              string    `json:"status"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type NodeMetricsSnapshot struct {
	ID          int64     `json:"id"`
	NodeID      string    `json:"node_id"`
	Metrics     []byte    `json:"metrics"`
	CollectedAt time.Time `json:"collected_at"`
}

type AppSecret struct {
	AppID    string `json:"app_id"`
	SecretID string `json:"secret_id"`
	Target   string `json:"target"`
	UID      string `json:"uid"`
	GID      string `json:"gid"`
	Mode     int    `json:"mode"`
}

type AppVolume struct {
	AppID         string `json:"app_id"`
	VolumeID      string `json:"volume_id"`
	ContainerPath string `json:"container_path"`
	ReadOnly      bool   `json:"read_only"`
}

type NotificationChannel struct {
	ID        string          `json:"id"`
	OrgID     string          `json:"org_id"`
	Name      string          `json:"name"`
	Type      string          `json:"type"` // "discord", "slack", "webhook", "email", "gotify"
	Config    json.RawMessage `json:"config"`
	CreatedAt time.Time       `json:"created_at"`
}

type NotificationEvent struct {
	ID        string    `json:"id"`
	ChannelID string    `json:"channel_id"`
	EventType string    `json:"event_type"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type ProxyRoute struct {
	ID               string    `json:"id"`
	ProjectID        string    `json:"project_id"`
	Name             string    `json:"name"`
	Domain           string    `json:"domain"`
	TargetService    string    `json:"target_service"`
	TargetPort       int       `json:"target_port"`
	Protocol         string    `json:"protocol"`
	UpstreamPort     *int      `json:"upstream_port"`
	SSLMode          string    `json:"ssl_mode"`
	CustomCertID     string    `json:"custom_cert_id"`
	MiddlewareConfig json.RawMessage `json:"middleware_config"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at"`
}

type CustomCertificate struct {
	ID              string       `json:"id"`
	ProjectID       string       `json:"project_id"`
	Domain          string       `json:"domain"`
	CertPEM         string       `json:"cert_pem"`
	KeyPEMEncrypted []byte       `json:"-"`
	IsWildcard      bool         `json:"is_wildcard"`
	Provider        string       `json:"provider"`
	ExpiresAt       sql.NullTime `json:"expires_at"`
	AutoRenew       bool         `json:"auto_renew"`
	DNSProviderID   string       `json:"dns_provider_id"`
	LastRenewedAt   sql.NullTime `json:"last_renewed_at"`
	RenewalError    string       `json:"renewal_error"`
	CreatedAt       time.Time    `json:"created_at"`
}

type Stack struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	Name           string    `json:"name"`
	ComposeContent string    `json:"compose_content"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AlertThreshold struct {
	ID              string       `json:"id"`
	OrgID           string       `json:"org_id"`
	Metric          string       `json:"metric"`
	Operator        string       `json:"operator"`
	Value           float64      `json:"value"`
	CooldownMinutes int          `json:"cooldown_minutes"`
	Enabled         bool         `json:"enabled"`
	LastFiredAt     sql.NullTime `json:"last_fired_at"`
	CreatedAt       time.Time    `json:"created_at"`
}

type DNSProvider struct {
	ID              string    `json:"id"`
	OrgID           string    `json:"org_id"`
	Name            string    `json:"name"`
	Type            string    `json:"type"`
	ConfigEncrypted []byte    `json:"-"`
	IsDefault       bool      `json:"is_default"`
	CreatedAt       time.Time `json:"created_at"`
}

type DNSRecord struct {
	ID         string    `json:"id"`
	ProviderID string    `json:"provider_id"`
	AppID      string    `json:"app_id"`
	Domain     string    `json:"domain"`
	RecordType string    `json:"record_type"`
	Value      string    `json:"value"`
	Proxied    bool      `json:"proxied"`
	Managed    bool      `json:"managed"`
	ExternalID string    `json:"external_id"`
	CreatedAt  time.Time `json:"created_at"`
}

type PreviewDeployment struct {
	ID          string    `json:"id"`
	AppID       string    `json:"app_id"`
	Branch      string    `json:"branch"`
	PRNumber    *int      `json:"pr_number"`
	Domain      string    `json:"domain"`
	Status      string    `json:"status"`
	ServiceName string    `json:"service_name"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ServiceLink struct {
	ID               string    `json:"id"`
	SourceAppID      string    `json:"source_app_id"`
	TargetAppID      string    `json:"target_app_id"`
	TargetDatabaseID string    `json:"target_database_id"`
	EnvPrefix        string    `json:"env_prefix"`
	CreatedAt        time.Time `json:"created_at"`
}

type OrgRole struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type MaintenanceTask struct {
	ID         string       `json:"id"`
	OrgID      string       `json:"org_id"`
	Type       string       `json:"type"`
	Schedule   string       `json:"schedule"`
	Enabled    bool         `json:"enabled"`
	LastRunAt  sql.NullTime `json:"last_run_at"`
	LastStatus string       `json:"last_status"`
	Config     json.RawMessage `json:"config"`
	CreatedAt  time.Time       `json:"created_at"`
}

type MaintenanceRun struct {
	ID         string       `json:"id"`
	TaskID     string       `json:"task_id"`
	Status     string       `json:"status"`
	Details    string       `json:"details"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt sql.NullTime `json:"finished_at"`
}

type AppEnvVar struct {
	ID             string    `json:"id"`
	AppID          string    `json:"app_id"`
	Key            string    `json:"key"`
	ValueEncrypted []byte    `json:"-"`
	IsSecret       bool      `json:"is_secret"`
	Source         string    `json:"source"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CephCluster struct {
	ID                    string    `json:"id"`
	Name                  string    `json:"name"`
	FSID                  string    `json:"fsid"`
	Status                string    `json:"status"`
	BootstrapNodeID       string    `json:"bootstrap_node_id"`
	MonHosts              []string  `json:"mon_hosts"`
	PublicNetwork         string    `json:"public_network"`
	ClusterNetwork        string    `json:"cluster_network"`
	CephConfEncrypted     []byte    `json:"-"`
	AdminKeyringEncrypted []byte    `json:"-"`
	ReplicationSize       int       `json:"replication_size"`
	StorageHostID         string    `json:"storage_host_id"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type CephOSD struct {
	ID         string    `json:"id"`
	ClusterID  string    `json:"cluster_id"`
	NodeID     string    `json:"node_id"`
	Hostname   string    `json:"hostname"`
	OsdID      *int      `json:"osd_id"`
	DevicePath string    `json:"device_path"`
	DeviceSize int64     `json:"device_size"`
	DeviceType string    `json:"device_type"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type CephPool struct {
	ID          string    `json:"id"`
	ClusterID   string    `json:"cluster_id"`
	Name        string    `json:"name"`
	PoolID      *int      `json:"pool_id"`
	PGNum       int       `json:"pg_num"`
	Size        int       `json:"size"`
	Type        string    `json:"type"`
	Application string    `json:"application"`
	CreatedAt   time.Time `json:"created_at"`
}

type LogEntry struct {
	ID          int64     `json:"id"`
	AppID       string    `json:"app_id"`
	ServiceName string    `json:"service_name"`
	NodeID      string    `json:"node_id"`
	Stream      string    `json:"stream"`
	Message     string    `json:"message"`
	Level       string    `json:"level"`
	Timestamp   time.Time `json:"timestamp"`
}

type LogForwardConfig struct {
	ID              string    `json:"id"`
	OrgID           string    `json:"org_id"`
	Name            string    `json:"name"`
	Type            string    `json:"type"`
	ConfigEncrypted []byte    `json:"-"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
}
