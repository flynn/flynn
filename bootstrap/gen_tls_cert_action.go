package bootstrap

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"fmt"

	"github.com/flynn/flynn/pkg/certgen"
)

type GenTLSCertAction struct {
	ID    string   `json:"id"`
	Hosts []string `json:"hosts"`

	CACert     string `json:"ca_cert"`
	Cert       string `json:"cert"`
	PrivateKey string `json:"key"`
}

func init() {
	Register("gen-tls-cert", &GenTLSCertAction{})
}

type TLSCert struct {
	CACert string `json:"ca_cert"`
	Cert   string `json:"cert"`
	Pin    string `json:"pin"`

	PrivateKey string `json:"-"`
}

func (c *TLSCert) String() string {
	return fmt.Sprintf("pin: %s", c.Pin)
}

func (a *GenTLSCertAction) Run(s *State) (err error) {
	data := &TLSCert{}
	s.StepData[a.ID] = data

	a.CACert = interpolate(s, a.CACert)
	a.Cert = interpolate(s, a.Cert)
	a.PrivateKey = interpolate(s, a.PrivateKey)
	if a.CACert != "" && a.Cert != "" && a.PrivateKey != "" {
		data.CACert = a.CACert
		data.Cert = a.Cert
		data.PrivateKey = a.PrivateKey

		// calculate cert pin
		b, _ := pem.Decode([]byte(data.Cert))
		sha := sha256.Sum256(b.Bytes)
		data.Pin = base64.StdEncoding.EncodeToString(sha[:])
		return nil
	}

	for i, h := range a.Hosts {
		a.Hosts[i] = interpolate(s, h)
	}
	ca, err := certgen.Generate(certgen.Params{IsCA: true})
	if err != nil {
		return err
	}
	cert, err := certgen.Generate(certgen.Params{Hosts: a.Hosts, CA: ca})
	if err != nil {
		return err
	}
	data.CACert = ca.PEM
	data.Cert = cert.PEM
	data.Pin = cert.Pin
	data.PrivateKey = cert.KeyPEM

	return err
}
