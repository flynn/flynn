package cluster

import (
	"crypto/rand"
	"encoding/hex"
	"io"
)

func RandomJobID(prefix string) string { return prefix + randomID() }

// generate a UUIDv4
func randomID() string {
	id := make([]byte, 16)
	_, err := io.ReadFull(rand.Reader, id)
	if err != nil {
		panic(err) // This shouldn't ever happen, right?
	}
	id[6] &= 0x0F // clear version
	id[6] |= 0x40 // set version to 4 (random uuid)
	id[8] &= 0x3F // clear variant
	id[8] |= 0x80 // set to IETF variant
	return hex.EncodeToString(id)
}
