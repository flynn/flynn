package tlsconfig

import (
	"crypto/tls"
)

// SecureCiphers updates a *tls.Config with a secure ciphersuite configuration.
// If c is nil, a new config will be provided.
func SecureCiphers(c *tls.Config) *tls.Config {
	if c == nil {
		c = &tls.Config{}
	}

	c.MinVersion = tls.VersionTLS12

	c.CipherSuites = []uint16{
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
	}

	// These are the only curves that have constant-time ASM implementations on amd64
	c.CurvePreferences = []tls.CurveID{
		tls.X25519,
		tls.CurveP256,
	}

	return c
}
