package database

// Request contains only the saved project path and the selected import source.
// Database credentials are detected natively and never cross the Go/JavaScript
// bridge.
type Request struct {
	ProjectPath string `json:"projectPath"`
	Mode        string `json:"mode"`
	ImportPath  string `json:"importPath,omitempty"`
}

type Config struct {
	ProjectID     string
	ProjectPath   string
	ProjectName   string
	ProjectKind   string
	DocumentRoot  string
	VMName        string
	RuntimeState  string
	RuntimeHealth string
	TempRoot      string
}

type Plan struct {
	ProjectPath       string `json:"projectPath"`
	Mode              string `json:"mode"`
	Ready             bool   `json:"ready"`
	Source            string `json:"source"`
	Database          string `json:"database,omitempty"`
	TablePrefix       string `json:"tablePrefix,omitempty"`
	Tool              string `json:"tool,omitempty"`
	ImportPath        string `json:"importPath,omitempty"`
	ImportBytes       int64  `json:"importBytes,omitempty"`
	AutomaticMessage  string `json:"automaticMessage,omitempty"`
	ReplacementNotice string `json:"replacementNotice"`
}

type Progress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	Percent int    `json:"percent"`
}

type Result struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	Detail      string `json:"detail,omitempty"`
	Source      string `json:"source,omitempty"`
	Tables      int    `json:"tables,omitempty"`
	Bytes       int64  `json:"bytes,omitempty"`
	TablePrefix string `json:"tablePrefix,omitempty"`
}

type Reporter func(Progress)
