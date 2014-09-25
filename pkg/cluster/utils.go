package cluster

import (
	"errors"
	"strings"
)

func ParseJobID(id string) (string, string, error) {
	ids := strings.SplitN(id, "-", 2)
	if len(ids) != 2 || ids[0] == "" || ids[1] == "" {
		return "", "", errors.New("invalid ID")
	}
	return ids[0], ids[1], nil
}
