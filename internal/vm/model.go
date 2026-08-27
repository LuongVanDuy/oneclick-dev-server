package vm

// Request contains only project identity. The backend always targets the one
// fixed OneClick server, so JavaScript cannot choose a Multipass instance.
type Request struct {
	ProjectPath string `json:"projectPath"`
	ProjectName string `json:"projectName"`
}

type Plan struct {
	VMName    string `json:"vmName"`
	Image     string `json:"image"`
	CPUs      int    `json:"cpus"`
	Memory    string `json:"memory"`
	Disk      string `json:"disk"`
	Network   string `json:"network"`
	Isolation string `json:"isolation"`
	Existing  bool   `json:"existing"`
}

type Progress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	Percent int    `json:"percent"`
}

type Result struct {
	Success        bool   `json:"success"`
	Reused         bool   `json:"reused"`
	CanRecreate    bool   `json:"canRecreate,omitempty"`
	RebootRequired bool   `json:"rebootRequired,omitempty"`
	Message        string `json:"message"`
	Detail         string `json:"detail,omitempty"`
	VMName         string `json:"vmName,omitempty"`
	State          string `json:"state,omitempty"`
	IP             string `json:"ip,omitempty"`
	Release        string `json:"release,omitempty"`
}

type Reporter func(Progress)

type SnapshotTransfer struct {
	ProjectPath string
	ProjectName string
	ProjectID   string
	ArchivePath string
	Checksum    string
	SnapshotID  string
}

type TransferResult struct {
	Success   bool
	Message   string
	Detail    string
	GuestPath string
}
