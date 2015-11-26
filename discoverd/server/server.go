package server

import (
	"net"

	"github.com/flynn/flynn/pkg/mux"
)

// Mux multiplexes a listener into two listeners.
func Mux(ln net.Listener) (storeLn, httpLn net.Listener) {
	m := mux.New(ln)

	// The store listens to everything prefixed with its header byte.
	storeLn = m.Listen([]byte{storeHdr})

	// HTTP listens to all methods: CONNECT, DELETE, GET, HEAD, OPTIONS, POST, PUT, TRACE.
	httpLn = m.Listen([]byte{'C', 'D', 'G', 'H', 'O', 'P', 'T'})

	go m.Serve()

	return
}
