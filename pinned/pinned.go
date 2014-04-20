// Package pinned provides a dial function that checks TLS server certificates against local pins.
package pinned

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"hash"
	"net"
)

type Config struct {
	// Hash specifies the hash function to use to check the Pin, it defaults to
	// sha256.New.
	Hash func() hash.Hash

	// Pin defines the expected certificate digest of the peer's leaf certificate.
	Pin []byte

	// Config is used as the base TLS configuration, if set.
	Config *tls.Config
}

var ErrPinFailure = errors.New("pinned: the peer leaf certificate did not match the provided pin")

// Dial establishes a TLS connection to addr and checks the peer leaf
// certificate against the configured pin. The underlying type of the returned
// net.Conn is guaranteed to be *tls.Conn.
func (c *Config) Dial(network, addr string) (net.Conn, error) {
	var conf tls.Config
	if c.Config != nil {
		conf = *c.Config
	}
	conf.InsecureSkipVerify = true

	conn, err := tls.Dial(network, addr, &conf)
	if err != nil {
		return nil, err
	}

	if err := conn.Handshake(); err != nil {
		conn.Close()
		return nil, err
	}

	state := conn.ConnectionState()
	hashFunc := c.Hash
	if hashFunc == nil {
		hashFunc = sha256.New
	}
	h := hashFunc()
	h.Write(state.PeerCertificates[0].Raw)
	if !bytes.Equal(h.Sum(nil), c.Pin) {
		conn.Close()
		return nil, ErrPinFailure
	}
	return conn, nil
}
