package buildlog

import (
	"io"
	"mime/multipart"
	"time"

	"github.com/flynn/flynn/pkg/iotool"
)

type Log struct {
	mw *multipart.Writer
}

func NewLog(w io.Writer) *Log {
	return &Log{multipart.NewWriter(w)}
}

func (l *Log) NewFile(name string) (io.Writer, error) {
	return l.mw.CreateFormFile(name, name)
}

func (l *Log) NewFileWithTimeout(name string, timeout time.Duration) (io.Writer, error) {
	w, err := l.NewFile(name)
	if err != nil {
		return nil, err
	}
	return iotool.NewTimeoutWriter(w, timeout), nil
}

func (l *Log) Boundary() string {
	return l.mw.Boundary()
}

func (l *Log) Close() error {
	return l.mw.Close()
}
