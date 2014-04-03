package main

import (
	"github.com/flynn/strowger/types"
)

type RPCHandler struct {
	r Router
}

func (h *RPCHandler) AddHTTPRoute(r *strowger.HTTPRoute, res *struct{}) error {
	return h.r.HTTPListener.AddRoute(r)
}

func (h *RPCHandler) RemoveHTTPRoute(domain string, res *struct{}) error {
	return h.r.HTTPListener.RemoveRoute(domain)
}

// TODO: change tls certificate
