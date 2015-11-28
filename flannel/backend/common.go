package backend

import (
	"net"

	"github.com/flynn/flynn/flannel/pkg/ip"
)

type SubnetDef struct {
	Net ip.IP4Net
	MTU int
}

type Backend interface {
	Init(extIface *net.Interface, extIP net.IP, httpPort string, ipMasq bool) (*SubnetDef, error)
	Run()
	Stop()
	Name() string
}
