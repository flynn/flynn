package tlscert

import (
	"fmt"

	"github.com/flynn/flynn/pkg/certgen"
)

type Cert struct {
	CACert string `json:"ca_cert"`
	Cert   string `json:"cert"`
	Pin    string `json:"pin"`

	PrivateKey string `json:"-"`
}

func (c *Cert) String() string {
	return fmt.Sprintf("pin: %s", c.Pin)
}

func Generate(hosts []string) (*Cert, error) {
	data := &Cert{}
	ca, err := certgen.Generate(certgen.Params{IsCA: true})
	if err != nil {
		return nil, err
	}
	cert, err := certgen.Generate(certgen.Params{Hosts: hosts, CA: ca})
	if err != nil {
		return nil, err
	}
	data.CACert = ca.PEM
	data.Cert = cert.PEM
	data.Pin = cert.Pin
	data.PrivateKey = cert.KeyPEM

	return data, err
}
