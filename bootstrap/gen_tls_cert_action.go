package bootstrap

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"

	"github.com/flynn/flynn/pkg/tlscert"
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

func (a *GenTLSCertAction) Run(s *State) (err error) {
	data := &tlscert.Cert{}
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
	c, err := tlscert.Generate(a.Hosts)
	if err != nil {
		return err
	}
	data.CACert = c.CACert
	data.Cert = c.Cert
	data.Pin = c.Pin
	data.PrivateKey = c.PrivateKey

	return err
}
