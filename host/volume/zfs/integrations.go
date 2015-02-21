package zfs

import (
	"fmt"
	"strings"

	gzfs "github.com/flynn/flynn/Godeps/_workspace/src/github.com/mistifyio/go-zfs"
	log "github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
)

type Logger struct {
	logger log.Logger
}

func (l *Logger) Log(msg []string) {
	l.logger.Debug(strings.Join(msg, " "))
}

func init() {
	logger := Logger{
		logger: log.New(log.Ctx{"package": "zfs"}),
	}
	gzfs.SetLogger(&logger)
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

func isDatasetNotExistsError(e error) bool {
	return strings.HasSuffix(e.Error(), "dataset does not exist\n")
}

/*
	"dataset is busy" errors from ZFS typically indicate that there are open
	files in that dataset mount.
*/
func IsDatasetBusyError(e error) bool {
	return strings.HasSuffix(e.Error(), "dataset is busy\n")
}

/*
	"has children" errors from ZFS occur when removing a volume that has
	snapshots.  ZFS requires snapshots of a volume to be deleted first.
*/
func IsDatasetHasChildrenError(e error) bool {
	lines := strings.SplitN(e.Error(), "\n", 2)
	return strings.HasSuffix(lines[0], "has children")
}
