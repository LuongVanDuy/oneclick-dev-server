package backup

type Config struct {
	ProjectID          string
	ProjectPath        string
	ProjectName        string
	ProjectKind        string
	VMName             string
	Stage              string
	RuntimeState       string
	RuntimeHealth      string
	DatabaseState      string
	DatabaseTables     int
	DatabaseImportedAt string
	BackupRoot         string
}

type Status struct {
	ProjectPath        string `json:"projectPath"`
	SourcePath         string `json:"sourcePath"`
	DeployedPath       string `json:"deployedPath"`
	Database           string `json:"database"`
	DatabaseTables     int    `json:"databaseTables"`
	DatabaseImportedAt string `json:"databaseImportedAt,omitempty"`
	BackupRoot         string `json:"backupRoot"`
	BackupCount        int    `json:"backupCount"`
	LastBackupAt       string `json:"lastBackupAt,omitempty"`
	LastBackupPath     string `json:"lastBackupPath,omitempty"`
	CanBackup          bool   `json:"canBackup"`
	CanReplaceDatabase bool   `json:"canReplaceDatabase"`
	Message            string `json:"message"`
}

type Progress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	Percent int    `json:"percent"`
}

type Result struct {
	Success       bool   `json:"success"`
	Message       string `json:"message"`
	Detail        string `json:"detail,omitempty"`
	Path          string `json:"path,omitempty"`
	SourceBytes   int64  `json:"sourceBytes,omitempty"`
	DatabaseBytes int64  `json:"databaseBytes,omitempty"`
	CreatedAt     string `json:"createdAt,omitempty"`
}

type Reporter func(Progress)

type Manifest struct {
	Schema      int          `json:"schema"`
	ProjectID   string       `json:"projectId"`
	ProjectName string       `json:"projectName"`
	CreatedAt   string       `json:"createdAt"`
	Source      ManifestFile `json:"source"`
	Database    ManifestFile `json:"database"`
}

type ManifestFile struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}
