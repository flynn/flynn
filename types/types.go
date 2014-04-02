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
	HTTPSCert  string // PEM encoded certificate
	HTTPSKey   string // PEM encoded private key
}

type Event struct {
	Event string
	ID    string
	Error error
}
