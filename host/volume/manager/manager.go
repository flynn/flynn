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

	"github.com/boltdb/bolt"
	"github.com/flynn/flynn/host/volume"
	"gopkg.in/inconshreveable/log15.v2"
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

	subscribers  map[chan *volume.Event]struct{}
	subscribeMtx sync.RWMutex

	dbPath string
	db     *bolt.DB
	dbMtx  sync.RWMutex

	logger log15.Logger

	defaultProvider func() (volume.Provider, error)
}

var (
	ErrNoSuchProvider = errors.New("no such provider")
	ErrProviderExists = errors.New("provider exists")
	ErrVolumeExists   = errors.New("volume exists")
)

func New(dbPath string, logger log15.Logger, defaultProvider func() (volume.Provider, error)) *Manager {
	return &Manager{
		providers:       make(map[string]volume.Provider),
		providerIDs:     make(map[volume.Provider]string),
		volumes:         make(map[string]volume.Volume),
		subscribers:     make(map[chan *volume.Event]struct{}),
		dbPath:          dbPath,
		logger:          logger,
		defaultProvider: defaultProvider,
	}
}

var ErrDBClosed = errors.New("volume persistence db is closed")

// OpenDB opens and initialises the persistence DB, if not already open.
func (m *Manager) OpenDB() error {
	if m.dbPath == "" {
		return nil
	}
	m.dbMtx.Lock()
	defer m.dbMtx.Unlock()
	// open/initialize db
	if err := os.MkdirAll(filepath.Dir(m.dbPath), 0755); err != nil {
		return fmt.Errorf("could not mkdir for volume persistence db: %s", err)
	}
	db, err := bolt.Open(m.dbPath, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("could not open volume persistence db: %s", err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		// idempotently create buckets.  (errors ignored because they're all compile-time impossible args checks.)
		tx.CreateBucketIfNotExists([]byte("volumes"))
		tx.CreateBucketIfNotExists([]byte("providers"))
		return nil
	}); err != nil {
		return fmt.Errorf("could not initialize volume persistence db: %s", err)
	}
	m.db = db
	if err := m.restore(); err != nil {
		return err
	}
	return m.maybeInitDefaultProvider()
}

// CloseDB closes the persistence DB.
//
// The DB mutex is locked to protect m.db, but also prevents closing the
// DB when it could still be needed to service API requests (see LockDB).
func (m *Manager) CloseDB() error {
	m.dbMtx.Lock()
	defer m.dbMtx.Unlock()
	if m.db == nil {
		return nil
	}
	if err := m.db.Close(); err != nil {
		return err
	}
	m.db = nil
	return nil
}

// LockDB acquires a read lock on the DB mutex so that it cannot be closed
// until the caller has finished performing actions which will lead to changes
// being persisted to the DB.
//
// For example, creating a volume first delegates to the provider to create the
// volume and then persists to the DB, but if the DB is closed in that time
// then the volume state will be lost.
//
// ErrDBClosed is returned if the DB is already closed so API requests will
// fail before any actions are performed.
func (m *Manager) LockDB() error {
	m.dbMtx.RLock()
	if m.db == nil {
		m.dbMtx.RUnlock()
		return ErrDBClosed
	}
	return nil
}

// UnlockDB releases a read lock on the DB mutex, previously acquired by a call
// to LockDB.
func (m *Manager) UnlockDB() {
	m.dbMtx.RUnlock()
}

func (m *Manager) Subscribe() chan *volume.Event {
	m.subscribeMtx.Lock()
	defer m.subscribeMtx.Unlock()
	ch := make(chan *volume.Event, 100)
	m.subscribers[ch] = struct{}{}
	return ch
}

func (m *Manager) Unsubscribe(ch chan *volume.Event) {
	go func() {
		// drain channel to prevent deadlock
		for range ch {
		}
	}()
	m.subscribeMtx.Lock()
	defer m.subscribeMtx.Unlock()
	delete(m.subscribers, ch)
}

func (m *Manager) sendEvent(vol volume.Volume, typ volume.EventType) {
	m.subscribeMtx.RLock()
	defer m.subscribeMtx.RUnlock()
	for ch := range m.subscribers {
		ch <- &volume.Event{
			Volume: vol.Info(),
			Type:   typ,
		}
	}
}

func (m *Manager) AddProvider(id string, p volume.Provider) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if _, ok := m.providers[id]; ok {
		return ErrProviderExists
	}
	if err := m.LockDB(); err != nil {
		return err
	}
	defer m.UnlockDB()
	m.addProviderLocked(id, p)
	return nil
}

func (m *Manager) addProviderLocked(id string, p volume.Provider) {
	m.providers[id] = p
	m.providerIDs[p] = id
	m.persist(func(tx *bolt.Tx) error { return m.persistProvider(tx, id) })
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
	return m.NewVolumeFromProvider("default")
}

/*
	volume.Manager implements the volume.Provider interface by
	delegating NewVolume requests to the named Provider.
*/
func (m *Manager) NewVolumeFromProvider(providerID string) (volume.Volume, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if p, ok := m.providers[providerID]; ok {
		return managerProviderProxy{p, m}.NewVolume()
	}
	return nil, ErrNoSuchProvider
}

func (m *Manager) GetVolume(id string) volume.Volume {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.volumes[id]
}

func (m *Manager) ImportFilesystem(providerID string, fs *volume.Filesystem) (volume.Volume, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, ok := m.volumes[fs.ID]; ok {
		return nil, ErrVolumeExists
	}

	if providerID == "" {
		providerID = "default"
	}
	provider, ok := m.providers[providerID]
	if !ok {
		return nil, ErrNoSuchProvider
	}

	if err := m.LockDB(); err != nil {
		return nil, err
	}
	defer m.UnlockDB()

	vol, err := provider.ImportFilesystem(fs)
	if err != nil {
		return nil, err
	}

	m.volumes[fs.ID] = vol
	m.persist(func(tx *bolt.Tx) error {
		return m.persistVolume(tx, vol)
	})
	m.sendEvent(vol, volume.EventTypeCreate)

	return vol, nil
}

func (m *Manager) DestroyVolume(id string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	vol := m.volumes[id]
	if vol == nil {
		return volume.ErrNoSuchVolume
	}
	if err := m.LockDB(); err != nil {
		return err
	}
	defer m.UnlockDB()
	if err := vol.Provider().DestroyVolume(vol); err != nil {
		return err
	}
	delete(m.volumes, id)
	// commit both changes
	m.persist(func(tx *bolt.Tx) error {
		return m.persistVolume(tx, vol)
	})
	m.sendEvent(vol, volume.EventTypeDestroy)
	return nil
}

func (m *Manager) CreateSnapshot(id string) (volume.Volume, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	vol := m.volumes[id]
	if vol == nil {
		return nil, volume.ErrNoSuchVolume
	}
	if err := m.LockDB(); err != nil {
		return nil, err
	}
	defer m.UnlockDB()
	snap, err := vol.Provider().CreateSnapshot(vol)
	if err != nil {
		return nil, err
	}
	m.volumes[snap.Info().ID] = snap
	m.persist(func(tx *bolt.Tx) error { return m.persistVolume(tx, snap) })
	m.sendEvent(snap, volume.EventTypeCreate)
	return snap, nil
}

func (m *Manager) ForkVolume(id string) (volume.Volume, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	vol := m.volumes[id]
	if vol == nil {
		return nil, volume.ErrNoSuchVolume
	}
	if err := m.LockDB(); err != nil {
		return nil, err
	}
	defer m.UnlockDB()
	vol2, err := vol.Provider().ForkVolume(vol)
	if err != nil {
		return nil, err
	}
	m.volumes[vol2.Info().ID] = vol2
	m.persist(func(tx *bolt.Tx) error { return m.persistVolume(tx, vol2) })
	m.sendEvent(vol2, volume.EventTypeCreate)
	return vol2, nil
}

func (m *Manager) ListHaves(id string) ([]json.RawMessage, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	vol := m.volumes[id]
	if vol == nil {
		return nil, volume.ErrNoSuchVolume
	}
	haves, err := vol.Provider().ListHaves(vol)
	if err != nil {
		return nil, err
	}
	return haves, nil
}

func (m *Manager) SendSnapshot(id string, haves []json.RawMessage, stream io.Writer) error {
	m.mutex.Lock()
	vol := m.volumes[id]
	if vol == nil {
		return volume.ErrNoSuchVolume
	}
	m.mutex.Unlock() // don't lock the manager for the duration of the send operation.
	return vol.Provider().SendSnapshot(vol, haves, stream)
}

func (m *Manager) ReceiveSnapshot(id string, stream io.Reader) (volume.Volume, error) {
	m.mutex.Lock()
	vol := m.volumes[id]
	if vol == nil {
		return nil, volume.ErrNoSuchVolume
	}
	if err := m.LockDB(); err != nil {
		return nil, err
	}
	defer m.UnlockDB()
	m.mutex.Unlock() // don't lock the manager for the duration of the recv operation.
	snap, err := vol.Provider().ReceiveSnapshot(vol, stream)
	if err != nil {
		return nil, err
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.volumes[snap.Info().ID] = snap
	m.persist(func(tx *bolt.Tx) error { return m.persistVolume(tx, snap) })
	m.sendEvent(snap, volume.EventTypeCreate)
	return snap, nil
}

func (m *Manager) restore() error {
	if err := m.db.View(func(tx *bolt.Tx) error {
		volumesBucket := tx.Bucket([]byte("volumes"))
		providersBucket := tx.Bucket([]byte("providers"))

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
				if err == volume.ErrNoSuchVolume {
					// ignore volumes which no longer exist (this can be the
					// case if a volume was destroyed but the change was not
					// persisted, for example during a hard stop)
					m.logger.Warn("skipping restore of non-existent volume", "vol.id", volID)
					return nil
				} else if err != nil {
					return err
				}
				m.volumes[vol.Info().ID] = vol
				m.sendEvent(vol, volume.EventTypeCreate)
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

		return nil
	}); err != nil && err != io.EOF {
		return fmt.Errorf("could not restore from volume persistence db: %s", err)
	}
	return nil
}

func (m *Manager) maybeInitDefaultProvider() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if _, ok := m.providers["default"]; ok {
		return nil
	}
	p, err := m.defaultProvider()
	if err != nil {
		return fmt.Errorf("could not initialize default provider: %s", err)
	}
	if p != nil {
		m.addProviderLocked("default", p)
	}
	return nil
}

func (m *Manager) persist(fn func(*bolt.Tx) error) {
	// maintenance note: db update calls should generally immediately follow
	// the matching in-memory map updates, *and be under the same mutex*.
	if err := m.db.Update(func(tx *bolt.Tx) error {
		return fn(tx)
	}); err != nil {
		panic(fmt.Errorf("could not commit volume persistence update: %s", err))
	}
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

/*
	Proxies `volume.Provider` while making sure the manager remains
	apprised of all volume lifecycle events.
*/
type managerProviderProxy struct {
	volume.Provider
	m *Manager
}

func (p managerProviderProxy) NewVolume() (volume.Volume, error) {
	if err := p.m.LockDB(); err != nil {
		return nil, err
	}
	defer p.m.UnlockDB()
	v, err := p.Provider.NewVolume()
	if err != nil {
		return v, err
	}
	p.m.volumes[v.Info().ID] = v
	p.m.persist(func(tx *bolt.Tx) error { return p.m.persistVolume(tx, v) })
	p.m.sendEvent(v, volume.EventTypeCreate)
	return v, err
}
