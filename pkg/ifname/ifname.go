// Copyright 2015 Docker, Inc. Code released under the Apache 2.0 license.

package ifname

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"strings"
)

// generateRandomName returns a new name joined with a prefix.  This size
// specified is used to truncate the randomly generated value
func generateRandomName(prefix string, size int) (string, error) {
	id := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, id); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(id)[:size], nil
}

// Generate returns an interface name using the passed in
// prefix and the length of random bytes. The api ensures that the
// there are is no interface which exists with that name.
func Generate(prefix string, len int) (string, error) {
	for i := 0; i < 3; i++ {
		name, err := generateRandomName(prefix, len)
		if err != nil {
			continue
		}
		if _, err := net.InterfaceByName(name); err != nil {
			if strings.Contains(err.Error(), "no such") {
				return name, nil
			}
			return "", err
		}
	}
	return "", errors.New("ifname: could not generate interface name")
}
