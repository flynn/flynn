// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bootstrap

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
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
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return
	}

	template := x509.Certificate{
		SerialNumber: new(big.Int).SetInt64(0),
		Subject: pkix.Name{
			Organization: []string{"Flynn"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	for _, h := range a.Hosts {
		host := interpolate(s, h)
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, host)
		}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return
	}

	h := sha256.New()
	h.Write(derBytes)
	pin = base64.StdEncoding.EncodeToString(h.Sum(nil))

	var buf bytes.Buffer
	pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	cert = buf.String()
	buf.Reset()
	pem.Encode(&buf, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	privKey = buf.String()

	return
}
