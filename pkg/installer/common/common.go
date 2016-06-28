package common

import (
	"bytes"
	"encoding/base64"
	"text/template"

	"github.com/flynn/flynn/bootstrap/discovery"
)

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

type StartScriptData struct {
	Script         string
	DiscoveryToken string
}

func (c *BaseCluster) GenerateStartScript(dataDisk string) (*StartScriptData, error) {
	data := struct {
		DiscoveryToken, DataDisk string
	}{DataDisk: dataDisk}
	var err error
	data.DiscoveryToken, err = discovery.NewToken()
	if err != nil {
		return nil, err
	}
	buf := &bytes.Buffer{}
	w := base64.NewEncoder(base64.StdEncoding, buf)
	err = startScript.Execute(w, data)
	w.Close()

	return &StartScriptData{
		Script:         buf.String(),
		DiscoveryToken: data.DiscoveryToken,
	}, err
}

var startScript = template.Must(template.New("start.sh").Parse(`
#!/bin/sh
set -e -x

FIRST_BOOT="/var/lib/flynn/first-boot"
mkdir -p /var/lib/flynn

if [ ! -f "${FIRST_BOOT}" ]; then
  {{if .DataDisk}}
  zpool create -f flynn-default {{.DataDisk}}
  {{end}}

  # wait for libvirt
  while ! [ -e /var/run/libvirt/libvirt-sock ]; do
    sleep 0.1
  done

  flynn-host init --discovery={{.DiscoveryToken}}
  start flynn-host
  sed -i 's/#start on/start on/' /etc/init/flynn-host.conf
  touch "${FIRST_BOOT}"
fi
`[1:]))
