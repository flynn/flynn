package zfs

import (
	"fmt"
	"strings"

	zfs "github.com/flynn/flynn/Godeps/_workspace/src/github.com/mistifyio/go-zfs"
)

type Logger struct{}

func (*Logger) Log(msg []string) {
	fmt.Printf("[zfs] %s\n", strings.Join(msg, " ")) // TODO replace with log15
}

func init() {
	logger := Logger{}
	zfs.SetLogger(&logger) // package wide global.  ugh.  have to propagate their singleton mistakes out and do this once somewhere.
	// it may also be the case that you don't actually want to set up the logger or such here.  we might have references to this package without actually ever invoking it.
}
