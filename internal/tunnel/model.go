package tunnel

type Request struct {
	ProjectPath string `json:"projectPath"`
	Hostname    string `json:"hostname"`
}

type Config struct {
	ProjectID     string
	ProjectPath   string
	ProjectName   string
	VMName        string
	RuntimeState  string
	RuntimeHealth string
	Hostname      string
	TunnelToken   string
}

type Plan struct {
	ProjectPath string `json:"projectPath"`
	Hostname    string `json:"hostname"`
	Provider    string `json:"provider"`
	Mode        string `json:"mode"`
	Address     string `json:"address"`
	Image       string `json:"image"`
	Download    string `json:"download"`
	Network     string `json:"network"`
	Exposure    string `json:"exposure"`
	Existing    bool   `json:"existing"`
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
	URL     string `json:"url,omitempty"`
	State   string `json:"state,omitempty"`
	Health  string `json:"health,omitempty"`
}

type Reporter func(Progress)
