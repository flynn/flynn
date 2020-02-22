package acme

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	acme "github.com/eggsampler/acme/v3"
)

// Account is an ACME account
type Account struct {
	Key                  string   `json:"key,omitempty"`
	Contacts             []string `json:"contacts,omitempty"`
	TermsOfServiceAgreed bool     `json:"terms_of_service_agreed,omitempty"`
}

// NewAccountFromEnv returns an ACME account with values taken from environment
// variables
func NewAccountFromEnv() (*Account, error) {
	account := &Account{
		Key: os.Getenv("ACCOUNT_KEY"),
	}
	if account.Key == "" {
		return nil, errors.New("missing ACCOUNT_KEY")
	}
	if contacts := os.Getenv("ACCOUNT_CONTACTS"); contacts != "" {
		account.Contacts = strings.Split(contacts, ",")
	}
	if v, err := strconv.ParseBool(os.Getenv("TERMS_OF_SERVICE_AGREED")); err == nil {
		account.TermsOfServiceAgreed = v
	}
	return account, nil
}

// KeyID returns the JWK Thumbprint of the account's private key
// (see https://tools.ietf.org/html/rfc7638)
func (a *Account) KeyID() string {
	privKey, err := a.PrivateKey()
	if err != nil {
		return ""
	}
	id, _ := acme.JWKThumbprint(privKey.Public())
	return id
}

// PrivateKey returns the account's private key
func (a *Account) PrivateKey() (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(a.Key))
	if block == nil {
		return nil, errors.New("error loading ACME account key: no PEM data found")
	}
	privKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("error loading ACME account key: %s", err)
	}
	return privKey, nil
}
