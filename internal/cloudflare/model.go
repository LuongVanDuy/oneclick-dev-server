package cloudflare

type ConnectionInput struct {
	Zone     string `json:"zone"`
	APIToken string `json:"apiToken"`
}

type Zone struct {
	Name        string   `json:"name"`
	ZoneID      string   `json:"zoneId"`
	AccountID   string   `json:"accountId"`
	Status      string   `json:"status"`
	NameServers []string `json:"nameServers"`
}

type ConnectionStatus struct {
	Connected           bool     `json:"connected"`
	TokenStored         bool     `json:"tokenStored"`
	Zone                string   `json:"zone"`
	ZoneID              string   `json:"zoneId,omitempty"`
	AccountID           string   `json:"accountId,omitempty"`
	ZoneStatus          string   `json:"zoneStatus,omitempty"`
	AssignedNameServers []string `json:"assignedNameServers,omitempty"`
	ActiveNameServers   []string `json:"activeNameServers,omitempty"`
	DNSReady            bool     `json:"dnsReady"`
	Message             string   `json:"message"`
}

type DeploymentSpec struct {
	ProjectID   string
	TunnelID    string
	DNSRecordID string
	Hostname    string
	Zone        Zone
}

type Allocation struct {
	TunnelID      string
	DNSRecordID   string
	TunnelToken   string
	TunnelCreated bool
	DNSCreated    bool
}
