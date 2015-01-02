package cluster

import (
	"errors"
	"strings"

	"github.com/flynn/flynn/pkg/random"
)

// ParseJobID splits a compound job ID into its host and job ID components
// returning an error if the ID is invalid.
func ParseJobID(id string) (string, string, error) {
	ids := strings.SplitN(id, "-", 2)
	if len(ids) != 2 || ids[0] == "" || ids[1] == "" {
		return "", "", errors.New("invalid ID")
	}
	return ids[0], ids[1], nil
}

// RandomJobID returns a random job identifier with an optional prefix.
func RandomJobID(prefix string) string { return prefix + random.UUID() }
