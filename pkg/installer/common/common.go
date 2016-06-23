package common

// BaseCluster contains common fields and methods
type BaseCluster struct {
	NumInstances        int    `json:"num_instances"`
	ControllerKey       string `json:"controller_key,omitempty"`
	ControllerPin       string `json:"controller_pin,omitempty"`
	DashboardLoginToken string `json:"dashboard_login_token,omitempty"`
	//Domain              *domain.Domain `json:"domain,omitempty"`
	CACert         string   `json:"ca_cert,omitempty"`
	DiscoveryToken string   `json:"discovery_token,omitempty"`
	HasBackup      bool     `json:"has_backup"`
	SSHUsername    string   `json:"ssh_username,omitempty"`
	InstanceIPs    []string `json:"instance_ips"`
}
