package main

import (
	"errors"

	"github.com/flynn/strowger/types"
)

type RPCHandler struct {
	r Router
}

func (h *RPCHandler) AddRoute(config *strowger.Config, res *struct{}) error {
	switch config.Type {
	case strowger.FrontendHTTP:
		if err := h.r.AddHTTPDomain(config.HTTPDomain, config.Service, config.HTTPSCert, config.HTTPSKey); err != nil {
			return err
		}
	default:
		return errors.New("unsupported route type")
	}
	return nil
}

func (h *RPCHandler) RemoveRoute(config *strowger.Config, res *struct{}) error {
	return nil
}

// TODO: change tls certificate
