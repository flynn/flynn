package main

import (
	"net"
	"net/http"
	"strings"
)

const (
	fwdForHeaderName   = "X-Forwarded-For"
	fwdProtoHeaderName = "X-Forwarded-Proto"
	fwdPortHeaderName  = "X-Forwarded-Port"
)

// fwdProtoHandler is an http.Handler that sets the X-Forwarded-For header on
// inbound requests to match the remote IP address, and sets X-Forwarded-Proto
// and X-Forwarded-Port headers to match the values in Proto and Port. If those
// headers already exist, the new values will be appended.
type fwdProtoHandler struct {
	http.Handler
	Proto string
	Port  string
}

func (h fwdProtoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// If we aren't the first proxy retain prior X-Forwarded-* information as a
	// comma+space separated list and fold multiple headers into one.
	if clientIP, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		if prior, ok := r.Header[fwdForHeaderName]; ok {
			clientIP = strings.Join(prior, ", ") + ", " + clientIP
		}
		r.Header.Set(fwdForHeaderName, clientIP)
	}

	proto, port := h.Proto, h.Port
	if prior, ok := r.Header[fwdProtoHeaderName]; ok {
		proto = strings.Join(prior, ", ") + ", " + proto
	}
	if prior, ok := r.Header[fwdPortHeaderName]; ok {
		port = strings.Join(prior, ", ") + ", " + port
	}
	r.Header.Set(fwdProtoHeaderName, proto)
	r.Header.Set(fwdPortHeaderName, port)

	h.Handler.ServeHTTP(w, r)
}
