package util

import (
	"crypto/rand"
	"encoding/hex"
	"io"
)

func RandomString() string {
	data := make([]byte, 16)
	_, err := io.ReadFull(rand.Reader, data)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(data)
}
