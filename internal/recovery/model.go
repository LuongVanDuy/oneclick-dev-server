package recovery

const SchemaVersion = 1

type Snapshot struct {
	Schema      int       `json:"schema"`
	UpdatedAt   string    `json:"updatedAt"`
	StoragePath string    `json:"storagePath"`
	Projects    []Project `json:"projects"`
}

type ProjectInput struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Host          string `json:"host"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	Protocol      string `json:"protocol"`
	Port          int    `json:"port"`
	RemotePath    string `json:"remotePath"`
	SiteURL       string `json:"siteUrl"`
	Workers       int    `json:"workers"`
	Passive       bool   `json:"passive"`
	AllowInsecure bool   `json:"allowInsecure"`
}

type Project struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Host            string         `json:"host"`
	Username        string         `json:"username"`
	Protocol        string         `json:"protocol"`
	Port            int            `json:"port"`
	RemotePath      string         `json:"remotePath"`
	SiteURL         string         `json:"siteUrl,omitempty"`
	Workers         int            `json:"workers"`
	Passive         bool           `json:"passive"`
	Status          string         `json:"status"`
	LastError       string         `json:"lastError,omitempty"`
	LastCheckedAt   string         `json:"lastCheckedAt,omitempty"`
	LastBackupID    string         `json:"lastBackupId,omitempty"`
	BackupFiles     int            `json:"backupFiles,omitempty"`
	BackupBytes     int64          `json:"backupBytes,omitempty"`
	FindingCount    int            `json:"findingCount,omitempty"`
	CriticalCount   int            `json:"criticalCount,omitempty"`
	BackupCreatedAt string         `json:"backupCreatedAt,omitempty"`
	CreatedAt       string         `json:"createdAt"`
	UpdatedAt       string         `json:"updatedAt"`
	History         []HistoryEntry `json:"history"`
	PasswordStored  bool           `json:"passwordStored,omitempty"`
	DataPath        string         `json:"dataPath,omitempty"`
}

type HistoryEntry struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	Detail    string `json:"detail,omitempty"`
	CreatedAt string `json:"createdAt"`
}

type ConnectionResult struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	Detail     string `json:"detail,omitempty"`
	RemotePath string `json:"remotePath,omitempty"`
	Secure     bool   `json:"secure"`
	CheckedAt  string `json:"checkedAt,omitempty"`
}

type BackupProgress struct {
	Stage      string `json:"stage"`
	Message    string `json:"message"`
	Percent    int    `json:"percent"`
	FilesDone  int    `json:"filesDone,omitempty"`
	FilesTotal int    `json:"filesTotal,omitempty"`
	BytesDone  int64  `json:"bytesDone,omitempty"`
	BytesTotal int64  `json:"bytesTotal,omitempty"`
}

type BackupResult struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	Detail       string `json:"detail,omitempty"`
	BackupID     string `json:"backupId,omitempty"`
	Path         string `json:"path,omitempty"`
	ManifestPath string `json:"manifestPath,omitempty"`
	ReportPath   string `json:"reportPath,omitempty"`
	Files        int    `json:"files,omitempty"`
	Bytes        int64  `json:"bytes,omitempty"`
	Findings     int    `json:"findings,omitempty"`
	Critical     int    `json:"critical,omitempty"`
	CreatedAt    string `json:"createdAt,omitempty"`
}

type BackupPlan struct {
	ProjectID   string `json:"projectId"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Protocol    string `json:"protocol"`
	RemotePath  string `json:"remotePath"`
	Destination string `json:"destination"`
	Workers     int    `json:"workers"`
	ReadOnly    bool   `json:"readOnly"`
	StorageMode string `json:"storageMode"`
}
