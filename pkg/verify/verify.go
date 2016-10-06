package verify

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
)

var (
	ErrNoHashes  = errors.New("verify: no hashes provided")
	ErrShortData = errors.New("verify: data too short")
)

type ErrInvalidSize struct {
	size int64
}

func (e *ErrInvalidSize) Error() string {
	return fmt.Sprintf("verify: invalid data size: %d", e.size)
}

type ErrHashMismatch struct {
	hash   *Hash
	actual string
}

func (e *ErrHashMismatch) Error() string {
	return fmt.Sprintf("verify: expected %s hash %q but got %q", e.hash.algorithm, e.hash.expected, e.actual)
}

type Hash struct {
	hash      hash.Hash
	algorithm string
	expected  string
}

type Verifier struct {
	hashes []*Hash
	size   int64
	reader *io.LimitedReader
}

func NewVerifier(hashes map[string]string, size int64) (*Verifier, error) {
	if size <= 0 {
		return nil, &ErrInvalidSize{size}
	}
	v := &Verifier{
		hashes: make([]*Hash, 0, len(hashes)),
		size:   size,
	}
	for algorithm, value := range hashes {
		h := &Hash{algorithm: algorithm, expected: value}
		switch algorithm {
		case "sha256":
			h.hash = sha256.New()
		case "sha512":
			h.hash = sha512.New()
		case "sha512_256":
			h.hash = sha512.New512_256()
		default:
			continue
		}
		v.hashes = append(v.hashes, h)
	}
	if len(v.hashes) == 0 {
		return nil, ErrNoHashes
	}
	return v, nil
}

func (v *Verifier) Reader(r io.Reader) io.Reader {
	writers := make([]io.Writer, len(v.hashes))
	for i, h := range v.hashes {
		writers[i] = h.hash
	}
	v.reader = &io.LimitedReader{R: r, N: v.size}
	return io.TeeReader(v.reader, io.MultiWriter(writers...))
}

func (v *Verifier) Verify() error {
	if v.reader == nil || v.reader.N != 0 {
		return ErrShortData
	}
	for _, h := range v.hashes {
		actual := hex.EncodeToString(h.hash.Sum(nil))
		if actual != h.expected {
			return &ErrHashMismatch{h, actual}
		}
	}
	return nil
}
