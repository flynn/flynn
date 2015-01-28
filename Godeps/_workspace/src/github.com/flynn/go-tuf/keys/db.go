package keys

import (
	"crypto/rand"
	"errors"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/agl/ed25519"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-tuf/data"
)

var (
	ErrWrongType        = errors.New("tuf: invalid key type")
	ErrExists           = errors.New("tuf: key already in db")
	ErrWrongID          = errors.New("tuf: key id mismatch")
	ErrInvalidKey       = errors.New("tuf: invalid key")
	ErrInvalidRole      = errors.New("tuf: invalid role")
	ErrInvalidKeyID     = errors.New("tuf: invalid key id")
	ErrInvalidThreshold = errors.New("tuf: invalid role threshold")
)

func NewKey() (*Key, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	k := &Key{
		Public:  *pub,
		Private: priv,
	}
	k.ID = k.Serialize().ID()
	return k, nil
}

type Key struct {
	ID      string
	Public  [ed25519.PublicKeySize]byte
	Private *[ed25519.PrivateKeySize]byte
}

func (k *Key) Serialize() *data.Key {
	return &data.Key{
		Type:  "ed25519",
		Value: data.KeyValue{Public: k.Public[:]},
	}
}

func (k *Key) SerializePrivate() *data.Key {
	return &data.Key{
		Type: "ed25519",
		Value: data.KeyValue{
			Public:  k.Public[:],
			Private: k.Private[:],
		},
	}
}

type Role struct {
	KeyIDs    map[string]struct{}
	Threshold int
}

func (r *Role) ValidKey(id string) bool {
	_, ok := r.KeyIDs[id]
	return ok
}

type DB struct {
	roles map[string]*Role
	keys  map[string]*Key
}

func NewDB() *DB {
	return &DB{
		roles: make(map[string]*Role),
		keys:  make(map[string]*Key),
	}
}

func (db *DB) AddKey(id string, k *data.Key) error {
	if k.Type != "ed25519" {
		return ErrWrongType
	}
	if id != k.ID() {
		return ErrWrongID
	}
	if len(k.Value.Public) != ed25519.PublicKeySize {
		return ErrInvalidKey
	}

	var key Key
	copy(key.Public[:], k.Value.Public)
	key.ID = id
	db.keys[id] = &key
	return nil
}

var validRoles = map[string]struct{}{
	"root":      {},
	"targets":   {},
	"snapshot":  {},
	"timestamp": {},
}

func ValidRole(name string) bool {
	_, ok := validRoles[name]
	return ok
}

func (db *DB) AddRole(name string, r *data.Role) error {
	if !ValidRole(name) {
		return ErrInvalidRole
	}
	if r.Threshold < 1 {
		return ErrInvalidThreshold
	}

	role := &Role{
		KeyIDs:    make(map[string]struct{}),
		Threshold: r.Threshold,
	}
	for _, id := range r.KeyIDs {
		if len(id) != data.KeyIDLength {
			return ErrInvalidKeyID
		}
		role.KeyIDs[id] = struct{}{}
	}

	db.roles[name] = role
	return nil
}

func (db *DB) GetKey(id string) *Key {
	return db.keys[id]
}

func (db *DB) GetRole(name string) *Role {
	return db.roles[name]
}
