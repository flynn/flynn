package util

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"path"

	"github.com/flynn/go-tuf/data"
)

var ErrWrongLength = errors.New("wrong length")

type ErrWrongHash struct {
	Type     string
	Expected data.HexBytes
	Actual   data.HexBytes
}

func (e ErrWrongHash) Error() string {
	return fmt.Sprintf("wrong %s hash, expected %s got %s", e.Type, hex.EncodeToString(e.Expected), hex.EncodeToString(e.Actual))
}

type ErrNoCommonHash struct {
	Expected data.Hashes
	Actual   data.Hashes
}

func (e ErrNoCommonHash) Error() string {
	types := func(a data.Hashes) []string {
		t := make([]string, 0, len(a))
		for typ := range a {
			t = append(t, typ)
		}
		return t
	}
	return fmt.Sprintf("no common hash function, expected one of %s, got %s", types(e.Expected), types(e.Actual))
}

type ErrUnknownHashAlgorithm struct {
	Name string
}

func (e ErrUnknownHashAlgorithm) Error() string {
	return fmt.Sprintf("unknown hash algorithm: %s", e.Name)
}

type PassphraseFunc func(role string, confirm bool) ([]byte, error)

func FileMetaEqual(actual data.FileMeta, expected data.FileMeta) error {
	if actual.Length != expected.Length {
		return ErrWrongLength
	}
	hashChecked := false
	for typ, hash := range expected.Hashes {
		if h, ok := actual.Hashes[typ]; ok {
			hashChecked = true
			if !hmac.Equal(h, hash) {
				return ErrWrongHash{typ, hash, h}
			}
		}
	}
	if !hashChecked {
		return ErrNoCommonHash{expected.Hashes, actual.Hashes}
	}
	return nil
}

const defaultHashAlgorithm = "sha512"

func GenerateFileMeta(r io.Reader, hashAlgorithms ...string) (data.FileMeta, error) {
	if len(hashAlgorithms) == 0 {
		hashAlgorithms = []string{defaultHashAlgorithm}
	}
	hashes := make(map[string]hash.Hash, len(hashAlgorithms))
	for _, hashAlgorithm := range hashAlgorithms {
		var h hash.Hash
		switch hashAlgorithm {
		case "sha256":
			h = sha256.New()
		case "sha512":
			h = sha512.New()
		default:
			return data.FileMeta{}, ErrUnknownHashAlgorithm{hashAlgorithm}
		}
		hashes[hashAlgorithm] = h
		r = io.TeeReader(r, h)
	}
	n, err := io.Copy(ioutil.Discard, r)
	if err != nil {
		return data.FileMeta{}, err
	}
	m := data.FileMeta{Length: n, Hashes: make(data.Hashes, len(hashes))}
	for hashAlgorithm, h := range hashes {
		m.Hashes[hashAlgorithm] = h.Sum(nil)
	}
	return m, nil
}

func NormalizeTarget(p string) string {
	return path.Join("/", p)
}

func HashedPaths(p string, hashes data.Hashes) []string {
	paths := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		hashedPath := path.Join(path.Dir(p), hash.String()+"."+path.Base(p))
		paths = append(paths, hashedPath)
	}
	return paths
}
