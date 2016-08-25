package backend

import (
	"errors"
	"io"
	"time"

	"github.com/flynn/flynn/pkg/postgres"
	"github.com/jackc/pgx"
)

var ErrNotFound = errors.New("file not found")

type File struct {
	FileStream
	FileInfo
}

type FileStream interface {
	io.ReadSeeker
	io.Closer
}

type Redirector interface {
	RedirectURL() string
}

type FileInfo struct {
	ID         string
	Name       string
	Size       int64
	ETag       string
	Type       string
	Oid        *pgx.Oid
	ExternalID string
	ModTime    time.Time
}

type Backend interface {
	Name() string
	Open(tx *postgres.DBTx, info FileInfo, txControl bool) (FileStream, error)
	Put(tx *postgres.DBTx, info FileInfo, r io.Reader, append bool) error
	Copy(tx *postgres.DBTx, dst, src FileInfo) error
	Delete(tx *postgres.DBTx, info FileInfo) error
}

var Backends = make(map[string]func(string, map[string]string) (Backend, error))
