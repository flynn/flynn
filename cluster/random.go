package cluster

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"io"
)

func RandomJobID(prefix string) string { return prefix + randomID() }

func randomID() string {
	b := make([]byte, 16)
	enc := make([]byte, 24)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		panic(err) // This shouldn't ever happen, right?
	}
	base64.URLEncoding.Encode(enc, b)
	return string(bytes.TrimRight(enc, "="))
}
