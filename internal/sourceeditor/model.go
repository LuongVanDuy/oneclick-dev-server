package sourceeditor

type Config struct {
	ProjectID   string
	ProjectPath string
	ProjectName string
	BackupRoot  string
}

type Entry struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	Size          int64  `json:"size,omitempty"`
	ModifiedAt    string `json:"modifiedAt,omitempty"`
	Editable      bool   `json:"editable"`
	BlockedReason string `json:"blockedReason,omitempty"`
}

type Listing struct {
	ProjectPath string  `json:"projectPath"`
	Directory   string  `json:"directory"`
	Parent      string  `json:"parent"`
	Entries     []Entry `json:"entries"`
}

type File struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	SHA256     string `json:"sha256"`
	Bytes      int64  `json:"bytes"`
	ModifiedAt string `json:"modifiedAt"`
}

type SaveRequest struct {
	ProjectPath string `json:"projectPath"`
	Path        string `json:"path"`
	Content     string `json:"content"`
	ExpectedSHA string `json:"expectedSha"`
}

type SaveResult struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	Detail     string `json:"detail,omitempty"`
	Path       string `json:"path,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	Bytes      int64  `json:"bytes,omitempty"`
	BackupPath string `json:"backupPath,omitempty"`
	SavedAt    string `json:"savedAt,omitempty"`
}
