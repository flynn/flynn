package random

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"io"
	mathrand "math/rand"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/wadey/cryptorand"
)

var Math = mathrand.New(cryptorand.Source)

func String(n int) string {
	return Hex(n/2 + 1)[:n]
}

func Hex(bytes int) string {
	return hex.EncodeToString(Bytes(bytes))
}

func Base64(bytes int) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(Bytes(bytes)), "=")
}

func Bytes(n int) []byte {
	data := make([]byte, n)
	_, err := io.ReadFull(rand.Reader, data)
	if err != nil {
		panic(err)
	}
	return data
}

func UUID() string {
	id := Bytes(16)
	id[6] &= 0x0F // clear version
	id[6] |= 0x40 // set version to 4 (random uuid)
	id[8] &= 0x3F // clear variant
	id[8] |= 0x80 // set to IETF variant
	return hex.EncodeToString(id)
}
