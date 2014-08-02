package util

import (
	"crypto/rand"
	"encoding/hex"
	"io"
)

var Repos = map[string]string{
	"flynn-host":       "master",
	"docker-etcd":      "master",
	"discoverd":        "master",
	"flynn-bootstrap":  "master",
	"flynn-controller": "master",
	"flynn-postgres":   "master",
	"flynn-receive":    "master",
	"shelf":            "master",
	"strowger":         "master",
	"slugbuilder":      "master",
	"slugrunner":       "master",
}

func RandomString(size int) string {
	data := make([]byte, size/2+1)
	_, err := io.ReadFull(rand.Reader, data)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(data)[:size]
}
