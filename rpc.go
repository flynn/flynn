package main

import (
	"errors"

	"github.com/flynn/strowger/types"
)

type Router struct {
	s Server
}

func (r *Router) AddFrontend(config *strowger.Config, res *struct{}) error {
	switch config.Type {
	case strowger.FrontendHTTP:
		if err := r.s.AddHTTPDomain(config.HTTPDomain, config.Service, config.HTTPSCert, config.HTTPSKey); err != nil {
			return err
		}
	default:
		return errors.New("unsupported frontend type")
	}
	return nil
}

func (r *Router) RemoveFrontend(config *strowger.Config, res *struct{}) error {
	return nil
}

// TODO: change tls certificate
