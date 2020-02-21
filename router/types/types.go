package router

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Certificate describes a TLS certificate for one or more routes
type Certificate struct {
	// ID is the unique ID of this Certificate
	ID string `json:"id,omitempty"`
	// Routes contains the IDs of routes assigned to this cert
	Routes []string `json:"routes,omitempty"`
	// TLSCert is the optional TLS public certificate. It is only used for HTTP routes.
	Cert string `json:"cert,omitempty"`
	// TLSCert is the optional TLS private key. It is only used for HTTP routes.
	Key string `json:"key,omitempty"`
	// CreatedAt is the time this cert was created.
	CreatedAt time.Time `json:"created_at,omitempty"`
	// UpdatedAt is the time this cert was last updated.
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

func CertificateID(pemData []byte) string {
	var chain [][]byte
	for {
		var block *pem.Block
		block, pemData = pem.Decode(pemData)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			chain = append(chain, block.Bytes)
		}
	}
	if len(chain) == 0 {
		return ""
	}
	digest := sha256.Sum256(bytes.Join(chain, []byte{}))
	return hex.EncodeToString(digest[:])
}

// KeyAlgorithm is the algorithm used by a TLS key
type KeyAlgorithm string

const (
	// KeyAlgorithm_ECC_P256 represents the NIST ECC P-256 curve
	KeyAlgorithm_ECC_P256 KeyAlgorithm = "ecc-p256"

	// KeyAlgorithm_RSA_2048 represents RSA with 2048-bit keys
	KeyAlgorithm_RSA_2048 KeyAlgorithm = "rsa-2048"

	// KeyAlgorithm_RSA_4096 represents RSA with 4096-bit keys
	KeyAlgorithm_RSA_4096 KeyAlgorithm = "rsa-4096"
)

// NewKey parses the private key contained in the given PEM-encoded data.
//
// The data is expected to contain a DER-encoded PKCS#1 (RSA), PKCS#8 (RSA/ECC),
// or SEC1 (ECC) private key, and the key must use either the RSA 2048 bit, RSA
// 4096 bit or ECC P256 key algorithm.
//
// The returned key's ID is a hex encoded sha256 digest of the PKIX encoded
// public key.
func NewKey(pemData []byte) (*Key, error) {
	// decode the PEM block
	var (
		keyData []byte
		skipped []string
	)
	for {
		var block *pem.Block
		block, pemData = pem.Decode(pemData)
		if block == nil {
			break
		}
		if block.Type == "PRIVATE KEY" || strings.HasSuffix(block.Type, " PRIVATE KEY") {
			keyData = block.Bytes
			break
		}
		skipped = append(skipped, block.Type)
	}
	if keyData == nil {
		if len(skipped) > 0 {
			return nil, fmt.Errorf("missing PRIVATE KEY block in PEM input, got %s", strings.Join(skipped, ", "))
		}
		return nil, errors.New("invalid PEM data")
	}

	// parse the private key from the contained DER data
	privKey, err := parsePrivateKey(keyData)
	if err != nil {
		return nil, err
	}

	// determine the key ID and type
	var (
		keyID   string
		keyAlgo KeyAlgorithm
	)
	switch k := privKey.(type) {
	case *rsa.PrivateKey:
		var err error
		keyID, err = KeyID(&k.PublicKey)
		if err != nil {
			return nil, err
		}
		size := k.N.BitLen()
		switch size {
		case 2048:
			keyAlgo = KeyAlgorithm_RSA_2048
		case 4096:
			keyAlgo = KeyAlgorithm_RSA_4096
		default:
			return nil, fmt.Errorf("unsupported RSA key size: %d", size)
		}
	case *ecdsa.PrivateKey:
		var err error
		keyID, err = KeyID(&k.PublicKey)
		if err != nil {
			return nil, err
		}
		switch k.Curve {
		case elliptic.P256():
			keyAlgo = KeyAlgorithm_ECC_P256
		default:
			return nil, fmt.Errorf("unsupported ECDSA curve: %v", k.Curve)
		}
	default:
		return nil, fmt.Errorf("unsupported key type %T, expected RSA or ECC", privKey)
	}

	// return the Key
	return &Key{
		ID:        keyID,
		Algorithm: keyAlgo,
		Key:       keyData,
	}, nil
}

// KeyID return a hex encoded sha256 digest of the PKIX encoding of the given
// public key
func KeyID(pubKey interface{}) (string, error) {
	switch pubKey.(type) {
	case *rsa.PublicKey, *ecdsa.PublicKey:
	default:
		return "", fmt.Errorf("unsupported key type %T, expected RSA or ECC", pubKey)
	}
	data, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

// CertificateKeyID returns the expected key ID for the public key contained in
// the given chain's leaf certificate (which is expected to be the first
// CERTIFICATE PEM block)
func CertificateKeyID(chainPEM []byte) string {
	var leafDER []byte
	for {
		var block *pem.Block
		block, chainPEM = pem.Decode(chainPEM)
		if block == nil {
			return ""
		}
		if block.Type == "CERTIFICATE" {
			leafDER = block.Bytes
			break
		}
	}
	cert, err := x509.ParseCertificate(leafDER)
	if err != nil {
		return ""
	}
	keyID, _ := KeyID(cert.PublicKey)
	return keyID
}

func parsePrivateKey(der []byte) (crypto.PrivateKey, error) {
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		switch key := key.(type) {
		case *rsa.PrivateKey, *ecdsa.PrivateKey:
			return key, nil
		default:
			return nil, fmt.Errorf("unsupported PKCS#8 private key %T, expected RSA or ECC", key)
		}
	}
	if key, err := x509.ParseECPrivateKey(der); err == nil {
		return key, nil
	}
	return nil, errors.New("failed to parse private key (tried PKCS#1, PKCS#8 and SEC1)")
}

type Key struct {
	// ID is the unique ID of this key
	ID string `json:"id,omitempty"`

	// Algorithm is the key algorithm used by this key
	Algorithm KeyAlgorithm `json:"algorithm,omitempty"`

	// Certificates contains the IDs of certificates using this key
	Certificates []string `json:"certificates,omitempty"`

	// CreatedAt is the time this key was created
	CreatedAt time.Time `json:"created_at,omitempty"`

	Key []byte `json:"-"`
}

// Route is a struct that combines the fields of HTTPRoute and TCPRoute
// for easy JSON marshaling.
type Route struct {
	// Type is the type of Route, either "http" or "tcp".
	Type string `json:"type"`
	// ID is the unique ID of this route.
	ID string `json:"id,omitempty"`
	// ParentRef is an external opaque identifier used by the route creator for
	// filtering and correlation. It typically contains the app ID.
	ParentRef string `json:"parent_ref,omitempty"`
	// Service is the ID of the service.
	Service string `json:"service"`
	// Port is the TCP port to listen on.
	Port int32 `json:"port,omitempty"`
	// Leader is whether or not traffic should only be routed to the leader or
	// all instances
	Leader bool `json:"leader"`
	// CreatedAt is the time this Route was created.
	CreatedAt time.Time `json:"created_at,omitempty"`
	// UpdatedAt is the time this Route was last updated.
	UpdatedAt time.Time `json:"updated_at,omitempty"`

	// Domain is the domain name of this Route. It is only used for HTTP routes.
	Domain string `json:"domain,omitempty"`

	// Certificate contains TLSCert and TLSKey
	Certificate *Certificate `json:"certificate,omitempty"`

	// Deprecated in favor of Certificate
	LegacyTLSCert string `json:"tls_cert,omitempty"`
	LegacyTLSKey  string `json:"tls_key,omitempty"`

	// Sticky is whether or not to use sticky sessions for this route. It is only
	// used for HTTP routes.
	Sticky bool `json:"sticky,omitempty"`
	// Path is the optional prefix to route to this service. It's exclusive with
	// the TLS options and can only be set if a "default" route with the same domain
	// and no Path already exists in the route table.
	Path string `json:"path,omitempty"`

	// DrainBackends is whether or not to track requests and trigger
	// drain events on backend shutdown when all requests have completed
	// (used by the scheduler to only stop jobs once all requests have
	// completed).
	DrainBackends bool `json:"drain_backends,omitempty"`

	// DisableKeepAlives when set will disable keep-alives between the
	// router and backends for this route
	DisableKeepAlives bool `json:"disable_keep_alives,omitempty"`
}

func (r Route) FormattedID() string {
	return r.Type + "/" + r.ID
}

func (r Route) HTTPRoute() *HTTPRoute {
	return &HTTPRoute{
		ID:            r.ID,
		ParentRef:     r.ParentRef,
		Service:       r.Service,
		Port:          int(r.Port),
		Leader:        r.Leader,
		DrainBackends: r.DrainBackends,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,

		Domain:            r.Domain,
		Certificate:       r.Certificate,
		LegacyTLSCert:     r.LegacyTLSCert,
		LegacyTLSKey:      r.LegacyTLSKey,
		Sticky:            r.Sticky,
		Path:              r.Path,
		DisableKeepAlives: r.DisableKeepAlives,
	}
}

func (r Route) TCPRoute() *TCPRoute {
	return &TCPRoute{
		ID:            r.ID,
		ParentRef:     r.ParentRef,
		Service:       r.Service,
		Port:          int(r.Port),
		Leader:        r.Leader,
		DrainBackends: r.DrainBackends,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}

// HTTPRoute is an HTTP Route.
type HTTPRoute struct {
	ID            string
	ParentRef     string
	Service       string
	Port          int
	Leader        bool
	DrainBackends bool
	CreatedAt     time.Time
	UpdatedAt     time.Time

	Domain            string
	Certificate       *Certificate `json:"certificate,omitempty"`
	LegacyTLSCert     string       `json:"tls_cert,omitempty"`
	LegacyTLSKey      string       `json:"tls_key,omitempty"`
	Sticky            bool
	Path              string
	DisableKeepAlives bool
}

func (r HTTPRoute) FormattedID() string {
	return "http/" + r.ID
}

func (r HTTPRoute) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.ToRoute())
}

func (r HTTPRoute) ToRoute() *Route {
	return &Route{
		// common fields
		Type:          "http",
		ID:            r.ID,
		ParentRef:     r.ParentRef,
		Service:       r.Service,
		Port:          int32(r.Port),
		Leader:        r.Leader,
		DrainBackends: r.DrainBackends,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,

		// http-specific fields
		Domain:            r.Domain,
		Certificate:       r.Certificate,
		LegacyTLSCert:     r.LegacyTLSCert,
		LegacyTLSKey:      r.LegacyTLSKey,
		Sticky:            r.Sticky,
		Path:              r.Path,
		DisableKeepAlives: r.DisableKeepAlives,
	}
}

// TCPRoute is a TCP Route.
type TCPRoute struct {
	ID            string
	ParentRef     string
	Service       string
	Port          int
	Leader        bool
	DrainBackends bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (r TCPRoute) FormattedID() string {
	return "tcp/" + r.ID
}

func (r TCPRoute) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.ToRoute())
}

func (r TCPRoute) ToRoute() *Route {
	return &Route{
		Type:          "tcp",
		ID:            r.ID,
		ParentRef:     r.ParentRef,
		Service:       r.Service,
		Port:          int32(r.Port),
		Leader:        r.Leader,
		DrainBackends: r.DrainBackends,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}

type EventType string

const (
	EventTypeRouteSet       EventType = "set"
	EventTypeRouteRemove    EventType = "remove"
	EventTypeBackendUp      EventType = "backend-up"
	EventTypeBackendDown    EventType = "backend-down"
	EventTypeBackendDrained EventType = "backend-drained"
)

type Event struct {
	Event   EventType
	ID      string
	Route   *Route
	Backend *Backend
	Error   error
}

type Backend struct {
	Service string `json:"service"`
	Addr    string `json:"addr"`
	App     string `json:"app"`
	JobID   string `json:"job_id"`
}

type StreamEvent struct {
	Event   EventType `json:"event"`
	Route   *Route    `json:"route,omitempty"`
	Backend *Backend  `json:"backend,omitempty"`
	Error   error     `json:"error,omitempty"`
}

type StreamEventsOptions struct {
	EventTypes []EventType
}
