package strowger

type FrontendType uint8

const (
	FrontendHTTP FrontendType = iota
	FrontendTCP
)

type Config struct {
	Service string
	Type    FrontendType

	// HTTP
	HTTPDomain string
	HTTPSCert  []byte // DER encoded certificate
	HTTPSKey   []byte // DER encoded private key
}

type Event struct {
	Event  string
	Domain string
}
