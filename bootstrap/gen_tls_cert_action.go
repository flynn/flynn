package bootstrap

import (
	"fmt"
	"time"

	"github.com/flynn/flynn/pkg/certgen"
)

type GenTLSCertAction struct {
	ID    string   `json:"id"`
	Hosts []string `json:"hosts"`
}

func init() {
	Register("gen-tls-cert", &GenTLSCertAction{})
}

type TLSCert struct {
	Cert string `json:"cert"`
	Pin  string `json:"pin"`

	PrivateKey string `json:"-"`
}

func (c *TLSCert) String() string {
	return fmt.Sprintf("pin: %s", c.Pin)
}

func (a *GenTLSCertAction) Run(s *State) (err error) {
	data := &TLSCert{}
	s.StepData[a.ID] = data
	data.Cert, data.PrivateKey, data.Pin, err = a.generateCert(s)
	return
}

func (a *GenTLSCertAction) generateCert(s *State) (cert, privKey, pin string, err error) {
	certOptions := certgen.Certificate{
		Lifespan: 365 * 24 * time.Hour,
		Hosts:    a.Hosts,
	}
	cert, privKey, pin, err = certgen.Generate(certOptions)
	return
}
