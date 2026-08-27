package state

const SchemaVersion = 3

type Snapshot struct {
	Schema      int                    `json:"schema"`
	UpdatedAt   string                 `json:"updatedAt"`
	StoragePath string                 `json:"storagePath"`
	Domains     []CloudflareConnection `json:"domains"`
	Projects    []ProjectRecord        `json:"projects"`
}

type CloudflareConnection struct {
	Zone        string   `json:"zone,omitempty"`
	ZoneID      string   `json:"zoneId,omitempty"`
	AccountID   string   `json:"accountId,omitempty"`
	ZoneStatus  string   `json:"zoneStatus,omitempty"`
	NameServers []string `json:"nameServers,omitempty"`
	ConnectedAt string   `json:"connectedAt,omitempty"`
	CheckedAt   string   `json:"checkedAt,omitempty"`
}

type ProjectInput struct {
	Path         string `json:"path"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	DocumentRoot string `json:"documentRoot"`
	PHPVersion   string `json:"phpVersion"`
	TunnelMode   string `json:"tunnelMode"`
}

type ProjectRecord struct {
	ID                 string         `json:"id"`
	Path               string         `json:"path"`
	Name               string         `json:"name"`
	Kind               string         `json:"kind"`
	KindLabel          string         `json:"kindLabel"`
	DocumentRoot       string         `json:"documentRoot"`
	PHPVersion         string         `json:"phpVersion"`
	TunnelMode         string         `json:"tunnelMode"`
	Stage              string         `json:"stage"`
	Status             string         `json:"status"`
	VMName             string         `json:"vmName,omitempty"`
	VMState            string         `json:"vmState,omitempty"`
	IP                 string         `json:"ip,omitempty"`
	SnapshotID         string         `json:"snapshotId,omitempty"`
	SnapshotHash       string         `json:"snapshotHash,omitempty"`
	SnapshotPath       string         `json:"snapshotPath,omitempty"`
	SnapshotFiles      int            `json:"snapshotFiles,omitempty"`
	SnapshotBytes      int64          `json:"snapshotBytes,omitempty"`
	RuntimeAdapter     string         `json:"runtimeAdapter,omitempty"`
	RuntimeState       string         `json:"runtimeState,omitempty"`
	RuntimeHealth      string         `json:"runtimeHealth,omitempty"`
	RuntimeContainers  int            `json:"runtimeContainers,omitempty"`
	RuntimeSnapshotID  string         `json:"runtimeSnapshotId,omitempty"`
	DatabaseState      string         `json:"databaseState,omitempty"`
	DatabaseSource     string         `json:"databaseSource,omitempty"`
	DatabaseTables     int            `json:"databaseTables,omitempty"`
	DatabaseBytes      int64          `json:"databaseBytes,omitempty"`
	DatabasePrefix     string         `json:"databasePrefix,omitempty"`
	DatabaseImportedAt string         `json:"databaseImportedAt,omitempty"`
	DomainZone         string         `json:"domainZone,omitempty"`
	Hostname           string         `json:"hostname,omitempty"`
	TunnelID           string         `json:"tunnelId,omitempty"`
	DNSRecordID        string         `json:"dnsRecordId,omitempty"`
	TunnelState        string         `json:"tunnelState,omitempty"`
	TunnelHealth       string         `json:"tunnelHealth,omitempty"`
	TunnelURL          string         `json:"tunnelUrl,omitempty"`
	TunnelStartedAt    string         `json:"tunnelStartedAt,omitempty"`
	LastError          string         `json:"lastError,omitempty"`
	CreatedAt          string         `json:"createdAt"`
	UpdatedAt          string         `json:"updatedAt"`
	History            []HistoryEntry `json:"history"`
}

type HistoryEntry struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	Detail    string `json:"detail,omitempty"`
	CreatedAt string `json:"createdAt"`
}

type EnvironmentOutcome struct {
	Success bool
	VMName  string
	VMState string
	IP      string
	Message string
	Detail  string
}

type CopyOutcome struct {
	Success    bool
	SnapshotID string
	Checksum   string
	GuestPath  string
	FileCount  int
	TotalBytes int64
	Message    string
	Detail     string
}

type RuntimeOutcome struct {
	Success    bool
	Adapter    string
	State      string
	Health     string
	Containers int
	SnapshotID string
	Message    string
	Detail     string
}

type DatabaseOutcome struct {
	Success     bool
	Source      string
	Tables      int
	Bytes       int64
	TablePrefix string
	Message     string
	Detail      string
}

type TunnelOutcome struct {
	Success     bool
	DomainZone  string
	Hostname    string
	TunnelID    string
	DNSRecordID string
	URL         string
	State       string
	Health      string
	Message     string
	Detail      string
}
