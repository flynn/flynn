package router

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ID is a slice of bytes used to identify TLS certificates and keys that
// encodes as a base64url string
type ID []byte

// NewID decodes an ID from the given base64url encoded string
func NewID(encoded string) (ID, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	return ID(decoded), nil
}

// Equals returns true if the ID equals the other ID
func (id ID) Equals(other ID) bool {
	return bytes.Equal(id, other)
}

// String returns the ID as a base64url encoded string
func (id ID) String() string {
	return base64.RawURLEncoding.EncodeToString(id)
}

// Bytes converts the ID to a slice of bytes
func (id ID) Bytes() []byte {
	return []byte(id)
}

// MarshalJSON encodes the ID as a JSON string
func (id ID) MarshalJSON() ([]byte, error) {
	return json.Marshal(id.String())
}

// UnmarshalJSON decodes the ID from a JSON string
func (id *ID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	newID, err := NewID(s)
	if err != nil {
		return err
	}
	*id = newID
	return nil
}

// Certificate describes a TLS certificate for one or more routes
type Certificate struct {
	// Routes contains the IDs of routes assigned to this cert
	Routes []string
	// Chain is the DER-encoded TLS certificate chain.
	Chain [][]byte
	// Key is the DER-encoded TLS private key.
	Key []byte
	// NoStrict is whether to skip performing strict checks on the
	// certificate
	NoStrict bool
	// CreatedAt is the time this cert was created.
	CreatedAt time.Time
	// UpdatedAt is the time this cert was last updated.
	UpdatedAt time.Time
}

// ID returns the unique ID of this Certificate
func (c *Certificate) ID() ID {
	digest := sha256.Sum256(bytes.Join(c.Chain, []byte{}))
	return ID(digest[:])
}

type certificateJSON struct {
	ID             string    `json:"id,omitempty"`
	Routes         []string  `json:"routes,omitempty"`
	Chain          string    `json:"chain,omitempty"`
	KeyID          string    `json:"key_id,omitempty"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
	DeprecatedCert string    `json:"cert,omitempty"`
}

func (c *Certificate) MarshalJSON() ([]byte, error) {
	return json.Marshal(&certificateJSON{
		ID:             c.ID().String(),
		Routes:         c.Routes,
		Chain:          c.ChainPEM(),
		DeprecatedCert: c.ChainPEM(),
		KeyID:          c.KeyID().String(),
		CreatedAt:      c.CreatedAt,
		UpdatedAt:      c.UpdatedAt,
	})
}

func (c *Certificate) UnmarshalJSON(data []byte) error {
	var cert certificateJSON
	if err := json.Unmarshal(data, &cert); err != nil {
		return err
	}
	if cert.Chain == "" && cert.DeprecatedCert != "" {
		cert.Chain = cert.DeprecatedCert
	}
	chainPEM := []byte(cert.Chain)
	var chainDER [][]byte
	for {
		var block *pem.Block
		block, chainPEM = pem.Decode(chainPEM)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			chainDER = append(chainDER, block.Bytes)
		}
	}
	*c = Certificate{
		Routes:    cert.Routes,
		Chain:     chainDER,
		CreatedAt: cert.CreatedAt,
		UpdatedAt: cert.UpdatedAt,
	}
	return nil
}

func NewCertificateFromKeyPair(chainPEM, keyPEM []byte) (*Certificate, error) {
	cert, err := NewCertificateFromPEM(chainPEM)
	if err != nil {
		return nil, err
	}
	key, err := NewKeyFromPEM(keyPEM)
	if err != nil {
		return nil, err
	}
	cert.Key = key.Key
	return cert, nil
}

func NewCertificateFromPEM(chainPEM []byte) (*Certificate, error) {
	var (
		chain   [][]byte
		skipped []string
	)
	for {
		var block *pem.Block
		block, chainPEM = pem.Decode(chainPEM)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			chain = append(chain, block.Bytes)
			continue
		}
		skipped = append(skipped, block.Type)
	}
	if len(chain) == 0 {
		if len(skipped) > 0 {
			return nil, fmt.Errorf("missing CERTIFICATE block in PEM input, got %s", strings.Join(skipped, ", "))
		}
		return nil, errors.New("invalid PEM data")
	}
	return &Certificate{Chain: chain}, nil
}

// KeyID returns the expected key ID for the certificate's public key
func (c *Certificate) KeyID() ID {
	if len(c.Chain) == 0 {
		return nil
	}
	cert, err := x509.ParseCertificate(c.Chain[0])
	if err != nil {
		return nil
	}
	keyID, _ := KeyID(cert.PublicKey)
	return keyID
}

func (c *Certificate) ChainPEM() string {
	if len(c.Chain) == 0 {
		return ""
	}
	var chain strings.Builder
	for i, cert := range c.Chain {
		pem.Encode(&chain, &pem.Block{
			Type:  "CERTIFICATE",
			Bytes: cert,
		})
		if i != len(c.Chain)-1 {
			chain.WriteString("\n")
		}
	}
	return chain.String()
}

func (c *Certificate) KeyPEM() string {
	if len(c.Key) == 0 {
		return ""
	}
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: c.Key,
	}))
}

// KeyAlgo is the algorithm used by a TLS key
type KeyAlgo string

const (
	// KeyAlgo_ECC_P256 represents the NIST ECC P-256 curve
	KeyAlgo_ECC_P256 KeyAlgo = "ecc-p256"

	// KeyAlgo_RSA_2048 represents RSA with 2048-bit keys
	KeyAlgo_RSA_2048 KeyAlgo = "rsa-2048"

	// KeyAlgo_RSA_4096 represents RSA with 4096-bit keys
	KeyAlgo_RSA_4096 KeyAlgo = "rsa-4096"
)

// NewKeyFromPEM returns the key contained in the given PEM-encoded data.
func NewKeyFromPEM(keyPEM []byte) (*Key, error) {
	var (
		keyDER  []byte
		skipped []string
	)
	for {
		var block *pem.Block
		block, keyPEM = pem.Decode(keyPEM)
		if block == nil {
			break
		}
		if block.Type == "PRIVATE KEY" || strings.HasSuffix(block.Type, " PRIVATE KEY") {
			keyDER = block.Bytes
			break
		}
		skipped = append(skipped, block.Type)
	}
	if keyDER == nil {
		if len(skipped) > 0 {
			return nil, fmt.Errorf("missing PRIVATE KEY block in PEM input, got %s", strings.Join(skipped, ", "))
		}
		return nil, errors.New("invalid PEM data")
	}
	return NewKey(keyDER)
}

// NewKey parses the private key contained in the given DER-encoded data.
//
// The data is expected to contain a PKCS#1 (RSA), PKCS#8 (RSA/ECC), or SEC1
// (ECC) private key, and the key must use either the RSA 2048 bit, RSA 4096
// bit or ECC P256 key algorithm.
//
// The returned key's ID is the sha256 digest of the PKIX encoded public key.
func NewKey(keyDER []byte) (*Key, error) {
	// parse the private key from the DER-encoded data
	privKey, err := parsePrivateKey(keyDER)
	if err != nil {
		return nil, err
	}

	// determine the key ID and algorithm
	var (
		keyID   ID
		keyAlgo KeyAlgo
	)
	switch k := privKey.(type) {
	case *rsa.PrivateKey:
		var err error
		keyID, err = KeyID(&k.PublicKey)
		if err != nil {
			return nil, err
		}
		keyAlgo, err = KeyAlgorithm(&k.PublicKey)
		if err != nil {
			return nil, err
		}
	case *ecdsa.PrivateKey:
		var err error
		keyID, err = KeyID(&k.PublicKey)
		if err != nil {
			return nil, err
		}
		keyAlgo, err = KeyAlgorithm(&k.PublicKey)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported key %T, expected RSA or ECC", privKey)
	}

	// return the Key
	return &Key{
		ID:        keyID,
		Algorithm: keyAlgo,
		Key:       keyDER,
	}, nil
}

// KeyID returns the sha256 digest of the PKIX encoding of the given public key
func KeyID(pubKey interface{}) (ID, error) {
	switch pubKey.(type) {
	case *rsa.PublicKey, *ecdsa.PublicKey:
	default:
		return nil, fmt.Errorf("unsupported key type %T, expected RSA or ECC", pubKey)
	}
	data, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	return ID(digest[:]), nil
}

// KeyAlgorithm returns the key algorithm of the given public key
func KeyAlgorithm(pubKey interface{}) (keyAlgo KeyAlgo, err error) {
	switch k := pubKey.(type) {
	case *rsa.PublicKey:
		size := k.N.BitLen()
		switch size {
		case 2048:
			keyAlgo = KeyAlgo_RSA_2048
		case 4096:
			keyAlgo = KeyAlgo_RSA_4096
		default:
			err = fmt.Errorf("unsupported RSA key size: %d", size)
		}
	case *ecdsa.PublicKey:
		switch k.Curve {
		case elliptic.P256():
			keyAlgo = KeyAlgo_ECC_P256
		default:
			err = fmt.Errorf("unsupported ECDSA curve: %v", k.Curve)
		}
	default:
		err = fmt.Errorf("unsupported key %T, expected RSA or ECC", pubKey)
	}
	return
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
	return nil, errors.New("failed to parse private key (tried PKCS#1, PKCS#8, and SEC1)")
}

type Key struct {
	// ID is the unique ID of this key
	ID ID `json:"id,omitempty"`

	// Algorithm is the key algorithm used by this key
	Algorithm KeyAlgo `json:"algorithm,omitempty"`

	// Certificates contains the IDs of certificates using this key
	Certificates []ID `json:"certificates,omitempty"`

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

	// ManagedCertificateDomain is the domain of the route's associated
	// managed certificate
	ManagedCertificateDomain *string `json:"managed_certificate_domain,omitempty"`

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
