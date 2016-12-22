package sign

import (
	"crypto/rand"
	"sync"

	"github.com/flynn/go-tuf/data"
	"golang.org/x/crypto/ed25519"
)

type PrivateKey struct {
	Type  string          `json:"keytype"`
	Value PrivateKeyValue `json:"keyval"`
}

type PrivateKeyValue struct {
	Public  data.HexBytes `json:"public"`
	Private data.HexBytes `json:"private"`
}

func (k *PrivateKey) PublicData() *data.Key {
	return &data.Key{
		Type:  k.Type,
		Value: data.KeyValue{Public: k.Value.Public},
	}
}

func (k *PrivateKey) Signer() Signer {
	return &ed25519Signer{PrivateKey: ed25519.PrivateKey(k.Value.Private)}
}

func GenerateEd25519Key() (*PrivateKey, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &PrivateKey{
		Type: data.KeyTypeEd25519,
		Value: PrivateKeyValue{
			Public:  data.HexBytes(public),
			Private: data.HexBytes(private),
		},
	}, nil
}

type ed25519Signer struct {
	ed25519.PrivateKey

	id     string
	idOnce sync.Once
}

var _ Signer = &ed25519Signer{}

func (s *ed25519Signer) ID() string {
	s.idOnce.Do(func() { s.id = s.publicData().ID() })
	return s.id
}

func (s *ed25519Signer) publicData() *data.Key {
	return &data.Key{
		Type:  data.KeyTypeEd25519,
		Value: data.KeyValue{Public: []byte(s.PrivateKey.Public().(ed25519.PublicKey))},
	}
}

func (s *ed25519Signer) Type() string {
	return data.KeyTypeEd25519
}
