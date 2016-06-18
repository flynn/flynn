package testutils

import (
	"os"

	. "github.com/flynn/go-check"
)

/*
	Skips a test if the UID isn't 0.

	Use in a suite's `SetUpSuite` method for great effect.
*/
func SkipIfNotRoot(t *C) {
	if os.Getuid() != 0 {
		t.Skip("cannot perform operations requiring root")
	}
}
