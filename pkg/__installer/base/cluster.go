package basecluster

import (
	"bytes"
	"encoding/base64"
	"text/template"

	"github.com/flynn/flynn/bootstrap/discovery"
	"github.com/flynn/flynn/pkg/installer"
	domain "github.com/flynn/flynn/pkg/installer/domain"
)

type BaseCluster struct {
	NumInstances        int
	ControllerKey       string
	ControllerPin       string
	DashboardLoginToken string
	Domain              *domain.Domain
	CACert              string
	DiscoveryToken      string
	HasBackup           bool
	SSHUsername         string
	InstanceIPs         []string
}

func (bc *BaseCluster) Bootstrap(t installer.TargetServer, ic *installer.Client, progress chan<- int) error {
	return nil
}

func (c *BaseCluster) GenerateStartScript(dataDisk string) (string, string, error) {
	data := struct {
		DiscoveryToken, DataDisk string
	}{DataDisk: dataDisk}
	var err error
	data.DiscoveryToken, err = discovery.NewToken()
	if err != nil {
		return "", "", err
	}
	buf := &bytes.Buffer{}
	w := base64.NewEncoder(base64.StdEncoding, buf)
	err = startScript.Execute(w, data)
	w.Close()

	return buf.String(), data.DiscoveryToken, err
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
