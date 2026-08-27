package datastore

// Status describes where Multipass stores VM disks, image caches and guest
// data. It never exposes credentials or the contents of a VM.
type Status struct {
	Platform        string `json:"platform"`
	Supported       bool   `json:"supported"`
	Configured      bool   `json:"configured"`
	Locked          bool   `json:"locked"`
	CanConfigure    bool   `json:"canConfigure"`
	RequiresAdmin   bool   `json:"requiresAdmin"`
	CurrentPath     string `json:"currentPath"`
	DefaultPath     string `json:"defaultPath"`
	RecommendedPath string `json:"recommendedPath"`
	FreeBytes       uint64 `json:"freeBytes"`
	InstanceCount   int    `json:"instanceCount"`
	ProjectCount    int    `json:"projectCount"`
	Message         string `json:"message"`
	Detail          string `json:"detail,omitempty"`
}

type Progress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	Percent int    `json:"percent"`
}

type Result struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
	Status  Status `json:"status"`
}

type Reporter func(Progress)
