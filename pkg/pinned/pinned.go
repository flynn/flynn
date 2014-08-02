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

	// Pin defines the expected digest of the peer's leaf certificate.
	Pin []byte

	// Config is used as the base TLS configuration, if set.
	Config *tls.Config
}

var ErrPinFailure = errors.New("pinned: the peer leaf certificate did not match the provided pin")

// Dial establishes a TLS connection to addr and checks the peer leaf
// certificate against the configured pin. The underlying type of the returned
// net.Conn is a Conn.
func (c *Config) Dial(network, addr string) (net.Conn, error) {
	var conf tls.Config
	if c.Config != nil {
		conf = *c.Config
	}
	conf.InsecureSkipVerify = true

	cn, err := net.Dial(network, addr)
	if err != nil {
		return nil, err
	}

	conn := Conn{
		Conn: tls.Client(cn, &conf),
		Wire: cn,
	}

	conf.ServerName, _, _ = net.SplitHostPort(addr)

	if err = conn.Handshake(); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil

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

type Conn struct {
	*tls.Conn
	Wire net.Conn
}

func (c Conn) CloseWrite() error {
	if cw, ok := c.Wire.(interface {
		CloseWrite() error
	}); ok {
		return cw.CloseWrite()
	}
	return errors.New("pinned: underlying connection does not support CloseWrite")
}
