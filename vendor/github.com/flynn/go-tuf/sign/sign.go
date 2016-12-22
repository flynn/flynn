package sign

import (
	"crypto"
	"crypto/rand"

	"github.com/flynn/go-tuf/data"
	"github.com/tent/canonical-json-go"
)

type Signer interface {
	// ID returns the TUF key id
	ID() string

	// Type returns the TUF key type
	Type() string

	// Signer is used to sign messages and provides access to the public key.
	// The signer is expected to do its own hashing, so the full message will be
	// provided as the message to Sign with a zero opts.HashFunc().
	crypto.Signer
}

func Sign(s *data.Signed, k Signer) error {
	id := k.ID()
	signatures := make([]data.Signature, 0, len(s.Signatures)+1)
	for _, sig := range s.Signatures {
		if sig.KeyID == id {
			continue
		}
		signatures = append(signatures, sig)
	}

	sig, err := k.Sign(rand.Reader, s.Signed, crypto.Hash(0))
	if err != nil {
		return err
	}

	s.Signatures = append(signatures, data.Signature{
		KeyID:     id,
		Method:    k.Type(),
		Signature: sig,
	})

	return nil
}

func Marshal(v interface{}, keys ...Signer) (*data.Signed, error) {
	b, err := cjson.Marshal(v)
	if err != nil {
		return nil, err
	}
	s := &data.Signed{Signed: b}
	for _, k := range keys {
		if err := Sign(s, k); err != nil {
			return nil, err
		}

	}
	return s, nil
}
