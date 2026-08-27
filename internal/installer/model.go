package installer

// Plan is the fixed, reviewable operation shown before a dependency changes
// the host. The frontend cannot provide a command or download URL.
type Plan struct {
	Action         string `json:"action"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	Publisher      string `json:"publisher"`
	Version        string `json:"version"`
	Source         string `json:"source"`
	DownloadSize   string `json:"downloadSize"`
	RequiresAdmin  bool   `json:"requiresAdmin"`
	RebootPossible bool   `json:"rebootPossible"`
	Supported      bool   `json:"supported"`
}

// Progress is emitted to the desktop UI while an installation is running.
type Progress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	Percent int    `json:"percent"`
}

// Result is the final, employee-friendly outcome of an installation.
type Result struct {
	Success        bool   `json:"success"`
	RebootRequired bool   `json:"rebootRequired"`
	Message        string `json:"message"`
	Detail         string `json:"detail,omitempty"`
}

type Reporter func(Progress)
