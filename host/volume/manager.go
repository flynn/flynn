package volume

import (
	"errors"
)

/*
	volume.Manager providers interfaces for both provisioning volume backends, and then creating volumes using them.

	There is one volume.Manager per host daemon process (though of course it's not an enforced singleton, because tests behave otherwise).
*/
type Manager struct {
	defaultProvider Provider

	// `map[providerName]provider`
	//
	// It's possible to configure multiple volume providers for a flynn-host daemon.
	// This can be used to create volumes using providers backed by different storage resources,
	// or different volume backends entirely.
	providers map[string]Provider

	// `map[volume.Id]volume`
	volumes map[string]Volume
}

var NoSuchProvider = errors.New("no such provider")
var ProviderAlreadyExists = errors.New("that provider id already exists")

func NewManager(p Provider) *Manager {
	return &Manager{
		defaultProvider: p,
		providers:       map[string]Provider{"default": p},
		volumes:         map[string]Volume{},
	}
}

func (m *Manager) AddProvider(id string, p Provider) error {
	if _, ok := m.providers[id]; ok {
		return ProviderAlreadyExists
	}
	m.providers[id] = p
	return nil
}

/*
	volume.Manager implements the volume.Provider interface by
	delegating NewVolume requests to the default Provider.
*/
func (m *Manager) NewVolume() (Volume, error) {
	return managerProviderProxy{m.defaultProvider, m}.NewVolume()
}

/*
	volume.Manager implements the volume.Provider interface by
	delegating NewVolume requests to the named Provider.
*/
func (m *Manager) NewVolumeFromProvider(providerID string) (Volume, error) {
	if p, ok := m.providers[providerID]; ok {
		return managerProviderProxy{p, m}.NewVolume()
	} else {
		return nil, NoSuchProvider
	}
}

func (m *Manager) GetVolume(id string) Volume {
	return m.volumes[id]
}

/*
	Proxies `volume.Provider` while making sure the manager remains
	apprised of all volume lifecycle events.

	@implements Provider
*/
type managerProviderProxy struct {
	Provider
	m *Manager
}

func (p managerProviderProxy) NewVolume() (Volume, error) {
	v, err := p.Provider.NewVolume()
	if err != nil {
		return v, err
	}
	p.m.volumes[v.Info().ID] = v
	return v, err
}

// TODO: lots of options for configuring, adding, and later selecting providers for use.  will take shape along with API.
