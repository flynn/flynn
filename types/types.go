package strowger

type HTTPRoute struct {
	Service string
	Domain  string

	TLSCert string
	TLSKey  string
}

type Event struct {
	Event string
	ID    string
	Error error
}
