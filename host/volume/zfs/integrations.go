package zfs

import (
	"strings"

	gzfs "github.com/mistifyio/go-zfs"
	log "github.com/inconshreveable/log15"
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
