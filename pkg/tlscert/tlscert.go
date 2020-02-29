package tlscert

import (
	"encoding/pem"
	"fmt"

	"github.com/flynn/flynn/pkg/certgen"
)

type Cert struct {
	CACert string `json:"ca_cert"`
	Cert   string `json:"cert"`
	Pin    string `json:"pin"`

	PrivateKey string `json:"key"`
}

func (c *Cert) ChainPEM() string {
	return c.Cert + "\n" + c.CACert
}

func (c *Cert) Chain() [][]byte {
	chainPEM := []byte(c.ChainPEM())
	var chain [][]byte
	for {
		var block *pem.Block
		block, chainPEM = pem.Decode(chainPEM)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			chain = append(chain, block.Bytes)
		}
	}
	return chain
}

func (c *Cert) PrivateKeyDER() []byte {
	block, _ := pem.Decode([]byte(c.PrivateKey))
	return block.Bytes
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
