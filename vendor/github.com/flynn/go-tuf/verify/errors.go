package verify

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrMissingKey       = errors.New("tuf: missing key")
	ErrNoSignatures     = errors.New("tuf: data has no signatures")
	ErrInvalid          = errors.New("tuf: signature verification failed")
	ErrWrongMethod      = errors.New("tuf: invalid signature type")
	ErrUnknownRole      = errors.New("tuf: unknown role")
	ErrRoleThreshold    = errors.New("tuf: valid signatures did not meet threshold")
	ErrWrongMetaType    = errors.New("tuf: meta file has wrong type")
	ErrExists           = errors.New("tuf: key already in db")
	ErrWrongID          = errors.New("tuf: key id mismatch")
	ErrInvalidKey       = errors.New("tuf: invalid key")
	ErrInvalidRole      = errors.New("tuf: invalid role")
	ErrInvalidKeyID     = errors.New("tuf: invalid key id")
	ErrInvalidThreshold = errors.New("tuf: invalid role threshold")
)

type ErrExpired struct {
	Expired time.Time
}

func (e ErrExpired) Error() string {
	return fmt.Sprintf("expired at %s", e.Expired)
}

type ErrLowVersion struct {
	Actual  int
	Current int
}

func (e ErrLowVersion) Error() string {
	return fmt.Sprintf("version %d is lower than current version %d", e.Actual, e.Current)
}
