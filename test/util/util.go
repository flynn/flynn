package util

import (
	"crypto/rand"
	"encoding/hex"
	"io"
)

func RandomString(size int) string {
	data := make([]byte, size/2+1)
	_, err := io.ReadFull(rand.Reader, data)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(data)[:size]
}
