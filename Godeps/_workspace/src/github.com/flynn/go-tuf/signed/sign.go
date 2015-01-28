package signed

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/agl/ed25519"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-tuf/data"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/tent/canonical-json-go"
)

func Sign(s *data.Signed, k *data.Key) {
	id := k.ID()
	signatures := make([]data.Signature, 0, len(s.Signatures)+1)
	for _, sig := range s.Signatures {
		if sig.KeyID == id {
			continue
		}
		signatures = append(signatures, sig)
	}
	priv := [ed25519.PrivateKeySize]byte{}
	copy(priv[:], k.Value.Private)
	sig := ed25519.Sign(&priv, s.Signed)
	s.Signatures = append(signatures, data.Signature{
		KeyID:     id,
		Method:    "ed25519",
		Signature: sig[:],
	})
}

func Marshal(v interface{}, keys ...*data.Key) (*data.Signed, error) {
	b, err := cjson.Marshal(v)
	if err != nil {
		return nil, err
	}
	s := &data.Signed{Signed: b}
	for _, k := range keys {
		Sign(s, k)
	}
	return s, nil
}
