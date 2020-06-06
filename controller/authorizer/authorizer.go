package authorizer

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes"
	"golang.org/x/crypto/cryptobyte"
	"golang.org/x/crypto/cryptobyte/asn1"
	"google.golang.org/protobuf/proto"
)

var ErrInvalid = errors.New("invalid authorization")

type Authorizer struct {
	authKeys []string
	authIDs  []string

	tokenKey         *ecdsa.PublicKey
	tokenMaxValidity time.Duration
}

type Token struct {
	ID   string
	User string
}

func New(authKeys, authIDs []string, tokenKey *ecdsa.PublicKey, tokenMaxValidity time.Duration) *Authorizer {
	return &Authorizer{
		authKeys:         authKeys,
		authIDs:          authIDs,
		tokenKey:         tokenKey,
		tokenMaxValidity: tokenMaxValidity,
	}
}

func (a *Authorizer) AuthorizeKey(key string) (*Token, error) {
	for i, k := range a.authKeys {
		if len(key) == len(k) && subtle.ConstantTimeCompare([]byte(key), []byte(k)) == 1 {
			token := &Token{}
			if len(a.authIDs) == len(a.authKeys) {
				token.ID = a.authIDs[i]
			}
			return token, nil
		}
	}
	return nil, ErrInvalid
}

func (a *Authorizer) AuthorizeToken(token string) (*Token, error) {
	b, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return nil, ErrInvalid
	}

	t := &api.AccessToken{}
	if err := protoVerifyUnmarshal(a.tokenKey, b, t); err != nil {
		return nil, ErrInvalid
	}

	iss, _ := ptypes.Timestamp(t.IssueTime)
	exp, _ := ptypes.Timestamp(t.ExpireTime)
	if iss.IsZero() || exp.IsZero() || exp.Sub(iss) > a.tokenMaxValidity || time.Now().After(exp) {
		return nil, ErrInvalid
	}

	idBytes := sha256.Sum256(b)
	return &Token{
		ID:   strings.TrimRight(base64.URLEncoding.EncodeToString(idBytes[:]), "="),
		User: t.UserEmail,
	}, nil
}

func protoVerifyUnmarshal(k *ecdsa.PublicKey, b []byte, m proto.Message) error {
	signed := &api.SignedData{}
	if err := proto.Unmarshal(b, signed); err != nil {
		return err
	}

	h := sha256.New()
	h.Write(signed.Data)

	if !verifyASN1(k, h.Sum(nil), signed.Signature) {
		return ErrInvalid
	}

	return proto.Unmarshal(signed.Data, m)
}

// This should be replaced with ecdsa.VerifyASN1 when Go 1.15 is available
// https://go.googlesource.com/go/+/8c09e8af3633b0c08d2c309e56a58124dfee3d7c
func verifyASN1(pub *ecdsa.PublicKey, hash, sig []byte) bool {
	var (
		r, s  = &big.Int{}, &big.Int{}
		inner cryptobyte.String
	)
	input := cryptobyte.String(sig)
	if !input.ReadASN1(&inner, asn1.SEQUENCE) ||
		!input.Empty() ||
		!inner.ReadASN1Integer(r) ||
		!inner.ReadASN1Integer(s) ||
		!inner.Empty() {
		return false
	}
	return ecdsa.Verify(pub, hash, r, s)
}
