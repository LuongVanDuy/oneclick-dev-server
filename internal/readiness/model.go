package readiness

import "time"

type Status string

const (
	StatusReady       Status = "ready"
	StatusInstallable Status = "installable"
	StatusUserAction  Status = "user_action"
	StatusUnsupported Status = "unsupported"
	StatusBroken      Status = "broken"
)

type Check struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Status      Status `json:"status"`
	Summary     string `json:"summary"`
	Detail      string `json:"detail,omitempty"`
	ActionLabel string `json:"actionLabel,omitempty"`
	ActionKind  string `json:"actionKind,omitempty"`
	Required    bool   `json:"required"`
}

type Report struct {
	Platform  string    `json:"platform"`
	Arch      string    `json:"arch"`
	Ready     bool      `json:"ready"`
	CheckedAt time.Time `json:"checkedAt"`
	Checks    []Check   `json:"checks"`
}
