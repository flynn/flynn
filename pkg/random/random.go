package random

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	mathrand "math/rand"
	"strings"
	"sync"
)

var Math *mathrand.Rand

func init() {
	maxInt63 := new(big.Int).SetUint64(1 << 63)
	seed, err := rand.Int(rand.Reader, maxInt63)
	if err != nil {
		panic(err)
	}
	Math = mathrand.New(&lockedSource{src: mathrand.NewSource(seed.Int64())})
}

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
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:])
}

type lockedSource struct {
	lk  sync.Mutex
	src mathrand.Source
}

func (r *lockedSource) Int63() (n int64) {
	r.lk.Lock()
	n = r.src.Int63()
	r.lk.Unlock()
	return
}

func (r *lockedSource) Seed(seed int64) {
	r.lk.Lock()
	r.src.Seed(seed)
	r.lk.Unlock()
}
