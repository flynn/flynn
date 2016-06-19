package signed

import (
	"fmt"
	"time"
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
