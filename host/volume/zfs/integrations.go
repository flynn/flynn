package zfs

import (
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
