// Package pinned provides a dial function that checks TLS server certificates against local pins.
package pinned

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"hash"
	"net"

	"github.com/flynn/flynn/pkg/dialer"
)

// A Config structure provides pinning and TLS connection information used to
// dial a server. A Config may be reused, the pinned package will not modify it.
type Config struct {
	// Hash specifies the hash function to use to check the Pin, it defaults to
	// sha256.New.
	Hash func() hash.Hash

	// Pin defines the expected digest of the peer's leaf certificate.
	Pin []byte

	// Config is used as the base TLS configuration, if set.
	Config *tls.Config
}

// ErrPinFailure is returned by Config.Dial if the TLS handshake succeeded but
// the peer certificate did not match the pin.
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

	cn, err := dialer.Retry.Dial(network, addr)
	if err != nil {
		return nil, err
	}

	conn := Conn{
		Conn: tls.Client(cn, &conf),
		Wire: cn,
	}

	if conf.ServerName == "" {
		conf.ServerName, _, _ = net.SplitHostPort(addr)
	}

	if err = conn.Handshake(); err != nil {
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

// A Conn represents a secured connection. It implements the net.Conn interface.
type Conn struct {
	// Conn is the actual TLS connection.
	*tls.Conn

	// Wire is the network connection underlying the TLS connection.
	Wire net.Conn
}

// CloseWrite shuts down the writing side of the connection.
func (c Conn) CloseWrite() error {
	if cw, ok := c.Wire.(interface {
		CloseWrite() error
	}); ok {
		return cw.CloseWrite()
	}
	return errors.New("pinned: underlying connection does not support CloseWrite")
}
