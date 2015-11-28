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

// ExtractUUID returns the UUID component of a job ID, returning an error if
// the given ID is invalid.
func ExtractUUID(id string) (string, error) {
	ids := strings.SplitN(id, "-", 2)
	if len(ids) != 2 || ids[0] == "" || ids[1] == "" {
		return "", errors.New("invalid ID")
	}
	return ids[1], nil
}

// GenerateJobID returns a random job identifier, prefixed with the given host ID.
func GenerateJobID(hostID, uuid string) string {
	if uuid == "" {
		uuid = random.UUID()
	}
	return hostID + "-" + uuid
}
