package cluster

import (
	"errors"
	"strings"

	"github.com/flynn/flynn/pkg/random"
)

// ExtractHostID returns the host ID component of a job ID, returning an error
// if the given ID is invalid.
func ExtractHostID(id string) (string, error) {
	ids := strings.SplitN(id, "-", 2)
	if len(ids) != 2 || ids[0] == "" || ids[1] == "" {
		return "", errors.New("invalid ID")
	}
	return ids[0], nil
}

// GenerateJobID returns a random job identifier, prefixed with the given host ID.
func GenerateJobID(hostID string) string {
	return hostID + "-" + random.UUID()
}
