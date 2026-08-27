package runtimeenv

type Request struct {
	ProjectPath string `json:"projectPath"`
}

type Config struct {
	ProjectID    string
	ProjectPath  string
	ProjectName  string
	ProjectKind  string
	DocumentRoot string
	PHPVersion   string
	VMName       string
	SnapshotID   string
	SnapshotHash string
	SnapshotPath string
}

type Plan struct {
	ProjectPath string   `json:"projectPath"`
	Adapter     string   `json:"adapter"`
	Runtime     string   `json:"runtime"`
	Database    string   `json:"database"`
	Services    int      `json:"services"`
	Packages    []string `json:"packages"`
	Download    string   `json:"download"`
	Resources   string   `json:"resources"`
	Network     string   `json:"network"`
	Public      bool     `json:"public"`
	Existing    bool     `json:"existing"`
}

type Progress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	Percent int    `json:"percent"`
}

type Result struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	Detail       string `json:"detail,omitempty"`
	Adapter      string `json:"adapter,omitempty"`
	Health       string `json:"health,omitempty"`
	Services     int    `json:"services,omitempty"`
	SnapshotID   string `json:"snapshotId,omitempty"`
	RuntimeState string `json:"runtimeState,omitempty"`
}

type Reporter func(Progress)
