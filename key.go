package main

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"

	ct "github.com/flynn/flynn-controller/types"
)

type KeyRepo struct {
	keyIDs map[string]*ct.Key
	keys   []*ct.Key
	mtx    sync.RWMutex
}

func NewKeyRepo() *KeyRepo {
	return &KeyRepo{keyIDs: make(map[string]*ct.Key)}
}

func (r *KeyRepo) Add(data interface{}) error {
	key := data.(*ct.Key)
	// TODO: validate key type
	if key.Key == "" {
		return errors.New("controller: key must not be blank")
	}

	splitKey := strings.SplitN(key.Key, " ", 3)
	if len(splitKey) < 2 {
		return errors.New("controller: key is missing data")
	}
	fingerprint, err := fingerprintKey(splitKey[1])
	if err != nil {
		return errors.New("controller: error decoding key data")
	}

	key.ID = fingerprint
	key.Key = splitKey[0] + " " + splitKey[1]
	if len(splitKey) > 2 {
		key.Comment = splitKey[2]
	}

	r.mtx.Lock()
	defer r.mtx.Unlock()

	if _, exists := r.keyIDs[key.ID]; exists {
		return errors.New("controller: key already exists")
	}

	r.keyIDs[key.ID] = key
	r.keys = append(r.keys, key)

	return nil
}

func fingerprintKey(key string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return "", err
	}
	digest := md5.Sum(data)
	return hex.EncodeToString(digest[:]), nil
}

func (r *KeyRepo) Get(id string) (interface{}, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	key := r.keyIDs[id]
	if key == nil {
		return nil, ErrNotFound
	}
	return key, nil
}

func (r *KeyRepo) List() (interface{}, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	return r.keys, nil
}
