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

	c.MinVersion = tls.VersionTLS10 // disable SSLv3
	c.MaxVersion = tls.VersionTLS12 // enable TLS_FALLBACK_SCSV: https://go-review.googlesource.com/#/c/1776/

	// Use all available ciphersuites minus RC4 and 3DES in a sane order.
	// This configuration has forward secrecy for supported browsers, and gets
	// an ‘A’ grade on SSL Labs.
	c.CipherSuites = []uint16{
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
		tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
		tls.TLS_RSA_WITH_AES_128_CBC_SHA,
		tls.TLS_RSA_WITH_AES_256_CBC_SHA,
	}
	c.PreferServerCipherSuites = true

	return c
}
