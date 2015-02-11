package volumemanager

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/boltdb/bolt"
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

	stateDB *bolt.DB
}

var NoSuchProvider = errors.New("no such provider")
var ProviderAlreadyExists = errors.New("that provider id already exists")
var NoSuchVolume = errors.New("no such volume")

func New(stateFilePath string, defProvFn func() (volume.Provider, error)) (*Manager, error) {
	stateDB, err := initializePersistence(stateFilePath)
	if err != nil {
		return nil, err
	}
	m := &Manager{
		providers:    make(map[string]volume.Provider),
		providerIDs:  make(map[volume.Provider]string),
		volumes:      make(map[string]volume.Volume),
		namedVolumes: make(map[string]string),
		stateDB:      stateDB,
	}
	if err := m.restore(); err != nil {
		return nil, err
	}
	if _, ok := m.providers["default"]; !ok {
		p, err := defProvFn()
		if err != nil {
			return nil, fmt.Errorf("could not initialize default provider: %s", err)
		}
		if p != nil {
			if err := m.AddProvider("default", p); err != nil {
				panic(err)
			}
		}
	}
	return m, nil
}

func (m *Manager) AddProvider(id string, p volume.Provider) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if _, ok := m.providers[id]; ok {
		return ProviderAlreadyExists
	}
	m.providers[id] = p
	m.providerIDs[p] = id
	m.persist(func(tx *bolt.Tx) error { return m.persistProvider(tx, id) })
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

func (m *Manager) DestroyVolume(id string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	vol := m.volumes[id]
	if vol == nil {
		return NoSuchVolume
	}
	if err := vol.Provider().DestroyVolume(vol); err != nil {
		return err
	}
	delete(m.volumes, id)
	// clean up named volumes
	namesToRemove := make([]string, 0, 1)
	for name, volID := range m.namedVolumes {
		if volID == id {
			namesToRemove = append(namesToRemove, name)
		}
		delete(m.namedVolumes, name)
	}
	// commit both changes
	m.persist(func(tx *bolt.Tx) error {
		for _, id := range namesToRemove {
			if err := m.persistNamedVolume(tx, id); err != nil {
				return err
			}
		}
		return m.persistVolume(tx, vol)
	})
	return nil
}

func initializePersistence(stateFilePath string) (*bolt.DB, error) {
	if stateFilePath == "" {
		return nil, nil
	}
	// open/initialize db
	if err := os.MkdirAll(filepath.Dir(stateFilePath), 0755); err != nil {
		return nil, fmt.Errorf("could not mkdir for volume persistence db: %s", err)
	}
	stateDB, err := bolt.Open(stateFilePath, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("could not open volume persistence db: %s", err)
	}
	if err := stateDB.Update(func(tx *bolt.Tx) error {
		// idempotently create buckets.  (errors ignored because they're all compile-time impossible args checks.)
		tx.CreateBucketIfNotExists([]byte("volumes"))
		tx.CreateBucketIfNotExists([]byte("providers"))
		tx.CreateBucketIfNotExists([]byte("namedVolumes"))
		return nil
	}); err != nil {
		return nil, fmt.Errorf("could not initialize volume persistence db: %s", err)
	}
	return stateDB, nil
}

func (m *Manager) restore() error {
	if m.stateDB == nil {
		return nil
	}
	if err := m.stateDB.View(func(tx *bolt.Tx) error {
		volumesBucket := tx.Bucket([]byte("volumes"))
		providersBucket := tx.Bucket([]byte("providers"))
		namedVolumesBucket := tx.Bucket([]byte("namedVolumes"))

		// restore volume info
		// keep this in a temporary map until we can get providers to transform them into reality
		volInfos := make(map[string]*volume.Info)
		if err := volumesBucket.ForEach(func(k, v []byte) error {
			volInfo := &volume.Info{}
			if err := json.Unmarshal(v, volInfo); err != nil {
				return fmt.Errorf("failed to deserialize volume info: %s", err)
			}
			volInfos[string(k)] = volInfo
			return nil
		}); err != nil {
			return err
		}

		// restore providers (and pass them the volume info)
		if err := providersBucket.ForEach(func(k, v []byte) error {
			// there is a bucket here named for each provider (all 'v' are nil)
			id := string(k)
			providerBucket := providersBucket.Bucket(k)
			// restore/construct provider with global config
			pspec := &volume.ProviderSpec{}
			if err := json.Unmarshal(providerBucket.Get([]byte("global")), pspec); err != nil {
				return fmt.Errorf("failed to deserialize provider info: %s", err)
			}
			provider, err := NewProvider(pspec)
			if err != nil {
				return fmt.Errorf("failed to initialize provider: %s", err)
			}
			// restore each piece of volume data to the provider
			if err := providerBucket.Bucket([]byte("volumes")).ForEach(func(k, v []byte) error {
				volID := string(k)
				volInfo, ok := volInfos[volID]
				if !ok {
					return fmt.Errorf("failed in restoring volumes: provider had unknown volumeID %q", volID)
				}
				vol, err := provider.RestoreVolumeState(volInfo, v)
				if err != nil {
					return err
				}
				m.volumes[vol.Info().ID] = vol
				return nil
			}); err != nil {
				return err
			}
			// done
			m.providers[id] = provider
			m.providerIDs[provider] = id
			return nil
		}); err != nil {
			return err
		}

		// restore named volumes mapping
		if err := namedVolumesBucket.ForEach(func(k, v []byte) error {
			m.namedVolumes[string(k)] = string(v)
			return nil
		}); err != nil {
			return err
		}

		return nil
	}); err != nil && err != io.EOF {
		return fmt.Errorf("could not restore from volume persistence db: %s", err)
	}
	return nil
}

func (m *Manager) persist(fn func(*bolt.Tx) error) {
	// maintenance note: db update calls should generally immediately follow
	// the matching in-memory map updates, *and be under the same mutex*.
	if m.stateDB == nil {
		return
	}
	if err := m.stateDB.Update(func(tx *bolt.Tx) error {
		return fn(tx)
	}); err != nil {
		panic(fmt.Errorf("could not commit volume persistence update: %s", err))
	}
}

/*
	Close the DB that persists the volume state.
	This is not called in typical flow because there's no need to release this file descriptor,
	but it is needed in testing so that bolt releases locks such that the file can be reopened.
*/
func (m *Manager) PersistenceDBClose() error {
	return m.stateDB.Close()
}

func (m *Manager) getProviderBucket(tx *bolt.Tx, providerID string) (*bolt.Bucket, error) {
	// Schema is roughly `"providers" -> "$provID" -> { "global" -> literal, "volumes" -> "$volID" -> literals }`.
	// ... This is getting complicated enough it might make sense to split the whole bolt thing out into its own structure.
	providerKey := []byte(providerID)
	providerBucket, err := tx.Bucket([]byte("providers")).CreateBucketIfNotExists(providerKey)
	if err != nil {
		return nil, err
	}
	_, err = providerBucket.CreateBucketIfNotExists([]byte("volumes"))
	return providerBucket, err
}

// Called to sync changes to disk when a volume is updated
func (m *Manager) persistVolume(tx *bolt.Tx, vol volume.Volume) error {
	// Save the general volume info
	volumesBucket := tx.Bucket([]byte("volumes"))
	id := vol.Info().ID
	k := []byte(id)
	_, volExists := m.volumes[id]
	if !volExists {
		volumesBucket.Delete(k)
	} else {
		b, err := json.Marshal(vol.Info())
		if err != nil {
			return fmt.Errorf("failed to serialize volume info: %s", err)
		}
		err = volumesBucket.Put(k, b)
		if err != nil {
			return fmt.Errorf("could not persist volume info to boltdb: %s", err)
		}
	}
	// Save any provider-specific metadata associated with the volume.
	// These are saved per-provider since the deserialization is also only defined per-provider implementation.
	providerBucket, err := m.getProviderBucket(tx, m.providerIDs[vol.Provider()])
	if err != nil {
		return fmt.Errorf("could not persist provider volume info to boltdb: %s", err)
	}
	providerVolumesBucket := providerBucket.Bucket([]byte("volumes"))
	if !volExists {
		providerVolumesBucket.Delete(k)
	} else {
		b, err := vol.Provider().MarshalVolumeState(id)
		if err != nil {
			return fmt.Errorf("failed to serialize provider volume info: %s", err)
		}
		err = providerVolumesBucket.Put(k, b)
		if err != nil {
			return fmt.Errorf("could not persist provider volume info to boltdb: %s", err)
		}
	}
	return nil
}

func (m *Manager) persistProvider(tx *bolt.Tx, id string) error {
	// Note: This method does *not* include re-serializing per-volume state,
	// because we assume that hasn't changed unless the change request
	// for the volume came through us and was handled elsewhere already.
	provider, ok := m.providers[id]
	if !ok {
		return tx.Bucket([]byte("providers")).DeleteBucket([]byte(id))
	}
	providersBucket, err := m.getProviderBucket(tx, id)
	if err != nil {
		return fmt.Errorf("could not persist provider info to boltdb: %s", err)
	}
	pspec := &volume.ProviderSpec{}
	pspec.Kind = provider.Kind()
	b, err := provider.MarshalGlobalState()
	if err != nil {
		return fmt.Errorf("failed to serialize provider info: %s", err)
	}
	pspec.Config = b
	b, err = json.Marshal(pspec)
	if err != nil {
		return fmt.Errorf("failed to serialize provider info: %s", err)
	}
	err = providersBucket.Put([]byte("global"), b)
	if err != nil {
		return fmt.Errorf("could not persist provider info to boltdb: %s", err)
	}
	return nil
}

func (m *Manager) persistNamedVolume(tx *bolt.Tx, name string) error {
	namedVolumesBucket := tx.Bucket([]byte("namedVolumes"))
	k := []byte(name)
	volID, ok := m.namedVolumes[name]
	if !ok {
		return namedVolumesBucket.Delete(k)
	}
	err := namedVolumesBucket.Put(k, []byte(volID))
	if err != nil {
		return fmt.Errorf("could not persist namedVolume info to boltdb: %s", err)
	}
	return nil
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
	p.m.persist(func(tx *bolt.Tx) error { return p.m.persistVolume(tx, v) })
	return v, err
}

func (m *Manager) NamedVolumes() map[string]string {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	r := make(map[string]string)
	for k, v := range m.namedVolumes {
		r[k] = v
	}
	return r
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
	m.persist(func(tx *bolt.Tx) error { return m.persistNamedVolume(tx, name) })
	return v, nil
}
