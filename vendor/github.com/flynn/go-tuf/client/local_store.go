package client

import (
	"encoding/json"
	"time"

	"github.com/boltdb/bolt"
)

func MemoryLocalStore() LocalStore {
	return make(memoryLocalStore)
}

type memoryLocalStore map[string]json.RawMessage

func (m memoryLocalStore) GetMeta() (map[string]json.RawMessage, error) {
	return m, nil
}

func (m memoryLocalStore) SetMeta(name string, meta json.RawMessage) error {
	m[name] = meta
	return nil
}

const dbBucket = "tuf-client"

func FileLocalStore(path string) (LocalStore, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(dbBucket))
		return err
	}); err != nil {
		return nil, err
	}
	return &fileLocalStore{db: db}, nil
}

type fileLocalStore struct {
	db *bolt.DB
}

func (f *fileLocalStore) GetMeta() (map[string]json.RawMessage, error) {
	meta := make(map[string]json.RawMessage)
	if err := f.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(dbBucket))
		b.ForEach(func(k, v []byte) error {
			vcopy := make([]byte, len(v))
			copy(vcopy, v)
			meta[string(k)] = vcopy
			return nil
		})
		return nil
	}); err != nil {
		return nil, err
	}
	return meta, nil
}

func (f *fileLocalStore) SetMeta(name string, meta json.RawMessage) error {
	return f.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(dbBucket))
		return b.Put([]byte(name), meta)
	})
}
