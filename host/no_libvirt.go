// +build dockeronly

package main

import (
	"errors"

	"github.com/flynn/flynn/host/ports"
)

func NewLibvirtLXCBackend(state *State, portAlloc map[string]*ports.Allocator, volPath, logPath, initPath string) (Backend, error) {
	return nil, errors.New("flynn-host not compiled with libvirt")
}
