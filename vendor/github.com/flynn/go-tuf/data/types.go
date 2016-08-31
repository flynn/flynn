package data

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	cjson "github.com/tent/canonical-json-go"
)

const (
	KeyIDLength            = sha256.Size * 2
	KeyTypeEd25519         = "ed25519"
	KeyTypeECDSA_SHA2_P256 = "ecdsa-sha2-nistp256"
)

type Signed struct {
	Signed     json.RawMessage `json:"signed"`
	Signatures []Signature     `json:"signatures"`
}

type Signature struct {
	KeyID     string   `json:"keyid"`
	Method    string   `json:"method"`
	Signature HexBytes `json:"sig"`
}

type Key struct {
	Type  string   `json:"keytype"`
	Value KeyValue `json:"keyval"`

	id     string
	idOnce sync.Once
}

func (k *Key) ID() string {
	k.idOnce.Do(func() {
		data, _ := cjson.Marshal(k)
		digest := sha256.Sum256(data)
		k.id = hex.EncodeToString(digest[:])
	})
	return k.id
}

type KeyValue struct {
	Public HexBytes `json:"public"`
}

func DefaultExpires(role string) time.Time {
	var t time.Time
	switch role {
	case "root":
		t = time.Now().AddDate(1, 0, 0)
	case "targets":
		t = time.Now().AddDate(0, 3, 0)
	case "snapshot":
		t = time.Now().AddDate(0, 0, 7)
	case "timestamp":
		t = time.Now().AddDate(0, 0, 1)
	}
	return t.UTC().Round(time.Second)
}

type Root struct {
	Type    string           `json:"_type"`
	Version int              `json:"version"`
	Expires time.Time        `json:"expires"`
	Keys    map[string]*Key  `json:"keys"`
	Roles   map[string]*Role `json:"roles"`

	ConsistentSnapshot bool `json:"consistent_snapshot"`
}

func NewRoot() *Root {
	return &Root{
		Type:               "Root",
		Expires:            DefaultExpires("root"),
		Keys:               make(map[string]*Key),
		Roles:              make(map[string]*Role),
		ConsistentSnapshot: true,
	}
}

type Role struct {
	KeyIDs    []string `json:"keyids"`
	Threshold int      `json:"threshold"`
}

type Files map[string]FileMeta

type Snapshot struct {
	Type    string    `json:"_type"`
	Version int       `json:"version"`
	Expires time.Time `json:"expires"`
	Meta    Files     `json:"meta"`
}

func NewSnapshot() *Snapshot {
	return &Snapshot{
		Type:    "Snapshot",
		Expires: DefaultExpires("snapshot"),
		Meta:    make(Files),
	}
}

type Hashes map[string]HexBytes

type FileMeta struct {
	Length int64            `json:"length"`
	Hashes Hashes           `json:"hashes"`
	Custom *json.RawMessage `json:"custom,omitempty"`
}

func (f FileMeta) HashAlgorithms() []string {
	funcs := make([]string, 0, len(f.Hashes))
	for name := range f.Hashes {
		funcs = append(funcs, name)
	}
	return funcs
}

type Targets struct {
	Type    string    `json:"_type"`
	Version int       `json:"version"`
	Expires time.Time `json:"expires"`
	Targets Files     `json:"targets"`
}

func NewTargets() *Targets {
	return &Targets{
		Type:    "Targets",
		Expires: DefaultExpires("targets"),
		Targets: make(Files),
	}
}

type Timestamp struct {
	Type    string    `json:"_type"`
	Version int       `json:"version"`
	Expires time.Time `json:"expires"`
	Meta    Files     `json:"meta"`
}

func NewTimestamp() *Timestamp {
	return &Timestamp{
		Type:    "Timestamp",
		Expires: DefaultExpires("timestamp"),
		Meta:    make(Files),
	}
}
