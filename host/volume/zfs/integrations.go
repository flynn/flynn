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
