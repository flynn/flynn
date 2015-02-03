package volumemanager

import (
	"errors"
	"fmt"
	"sync"

	"github.com/flynn/flynn/host/volume"
)

/*
	volume.Manager providers interfaces for both provisioning volume backends, and then creating volumes using them.

	There is one volume.Manager per host daemon process.
*/
type Manager struct {
	mutex sync.Mutex

	// `map[providerName]provider`
	//
	// It's possible to configure multiple volume providers for a flynn-host daemon.
	// This can be used to create volumes using providers backed by different storage resources,
	// or different volume backends entirely.
	providers   map[string]volume.Provider
	providerIDs map[volume.Provider]string

	// `map[volume.Id]volume`
	volumes map[string]volume.Volume

	// `map[wellKnownName]volume.Id`
	namedVolumes map[string]string
}

func New(defProvFn func() (volume.Provider, error)) (*Manager, error) {
	m := &Manager{
		providers:    make(map[string]volume.Provider),
		providerIDs:  make(map[volume.Provider]string),
		volumes:      make(map[string]volume.Volume),
		namedVolumes: make(map[string]string),
	}
	if _, ok := m.providers["default"]; !ok {
		p, err := defProvFn()
		if err != nil {
			return nil, fmt.Errorf("could not initialize default provider: %s", err)
		}
		m.providers["default"] = p
		m.providerIDs[p] = "default"
	}
	return m, nil
}

var NoSuchProvider = errors.New("no such provider")
var ProviderAlreadyExists = errors.New("that provider id already exists")

func (m *Manager) AddProvider(id string, p volume.Provider) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if _, ok := m.providers[id]; ok {
		return ProviderAlreadyExists
	}
	m.providers[id] = p
	m.providerIDs[p] = id
	return nil
}

func (m *Manager) Volumes() map[string]volume.Volume {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	r := make(map[string]volume.Volume)
	for k, v := range m.volumes {
		r[k] = v
	}
	return r
}

/*
	volume.Manager implements the volume.Provider interface by
	delegating NewVolume requests to the default Provider.
*/
func (m *Manager) NewVolume() (volume.Volume, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.newVolumeFromProviderLocked("")
}

/*
	volume.Manager implements the volume.Provider interface by
	delegating NewVolume requests to the named Provider.
*/
func (m *Manager) NewVolumeFromProvider(providerID string) (volume.Volume, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.newVolumeFromProviderLocked(providerID)
}

func (m *Manager) newVolumeFromProviderLocked(providerID string) (volume.Volume, error) {
	if providerID == "" {
		providerID = "default"
	}
	if p, ok := m.providers[providerID]; ok {
		return managerProviderProxy{p, m}.NewVolume()
	} else {
		return nil, NoSuchProvider
	}
}

func (m *Manager) GetVolume(id string) volume.Volume {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.volumes[id]
}

/*
	Proxies `volume.Provider` while making sure the manager remains
	apprised of all volume lifecycle events.
*/
type managerProviderProxy struct {
	volume.Provider
	m *Manager
}

func (p managerProviderProxy) NewVolume() (volume.Volume, error) {
	v, err := p.Provider.NewVolume()
	if err != nil {
		return v, err
	}
	p.m.volumes[v.Info().ID] = v
	return v, err
}

/*
	Gets a reference to a volume by name if that exists; if no volume is so named,
	it is created using the named provider (the zero string can be used to invoke
	the default provider).
*/
func (m *Manager) CreateOrGetNamedVolume(name string, providerID string) (volume.Volume, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if v, ok := m.namedVolumes[name]; ok {
		return m.volumes[v], nil
	}
	v, err := m.newVolumeFromProviderLocked(providerID)
	if err != nil {
		return nil, err
	}
	m.namedVolumes[name] = v.Info().ID
	return v, nil
}
