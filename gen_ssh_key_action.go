package bootstrap

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"

	"github.com/flynn/go-crypto-ssh"
)

type GenSSHKeyAction struct {
	ID string `json:"id"`
}

type SSHKey struct {
	PrivateKeys string

	RSAPublic   string
	ECDSAPublic string
}

func init() {
	Register("gen-ssh-key", &GenSSHKeyAction{})
}

// This action generates two SSH keys using the same key strength as Ubuntu
// defaults:
// - RSA (2048-bit)
// - ECDSA (256-bit prime256v1)

func (a *GenSSHKeyAction) Run(s *State) error {
	data := &SSHKey{}
	s.StepData[a.ID] = data

	var pemBuf bytes.Buffer

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	pem.Encode(&pemBuf, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(rsaKey),
	})
	rsaPubKey, err := ssh.NewPublicKey(&rsaKey.PublicKey)
	if err != nil {
		return err
	}
	data.RSAPublic = string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(rsaPubKey)))

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	ecBytes, err := x509.MarshalECPrivateKey(ecKey)
	if err != nil {
		return err
	}
	pem.Encode(&pemBuf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: ecBytes})
	ecPubKey, err := ssh.NewPublicKey(&ecKey.PublicKey)
	if err != nil {
		return err
	}
	data.ECDSAPublic = string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(ecPubKey)))

	data.PrivateKeys = pemBuf.String()

	return nil
}
