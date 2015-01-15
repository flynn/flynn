package zfs

import (
	"fmt"
	"strings"

	gzfs "github.com/flynn/flynn/Godeps/_workspace/src/github.com/mistifyio/go-zfs"
)

type Logger struct{}

func (*Logger) Log(msg []string) {
	fmt.Printf("[zfs] %s\n", strings.Join(msg, " ")) // TODO replace with log15
}

func init() {
	logger := Logger{}
	gzfs.SetLogger(&logger) // package wide global.  ugh.  have to propagate their singleton mistakes out and do this once somewhere.
	// it may also be the case that you don't actually want to set up the logger or such here.  we might have references to this package without actually ever invoking it.
}

/*
	Returns the error string from the zfs command.  (Pretty much everything added by
	go-zfs is cute for debugging, but fairly useless for parsing and handling.)
*/
func eunwrap(e error) error {
	if e2, ok := e.(*gzfs.Error); ok {
		return fmt.Errorf("%s", e2.Stderr)
	} else {
		return e
	}
}
