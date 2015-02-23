package postgres

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/shutdown"
)

func New(db *sql.DB, dsn string) *DB {
	return &DB{
		DB:    db,
		dsn:   dsn,
		stmts: make(map[string]*sql.Stmt),
	}
}

func Wait(service string) (string, string) {
	if service == "" {
		service = os.Getenv("FLYNN_POSTGRES")
	}
	events := make(chan *discoverd.Event)
	stream, err := discoverd.NewService(service).Watch(events)
	if err != nil {
		shutdown.Fatal(err)
	}
	defer stream.Close()
	for e := range events {
		if e.Kind&(discoverd.EventKindUp|discoverd.EventKindUpdate) != 0 &&
			e.Instance.Meta["up"] == "true" &&
			e.Instance.Meta["username"] != "" &&
			e.Instance.Meta["password"] != "" {
			return e.Instance.Meta["username"], e.Instance.Meta["password"]
		}
	}
	panic("discoverd disconnected before postgres came up")
}

func Open(service, dsn string) (*DB, error) {
	if service == "" {
		service = os.Getenv("FLYNN_POSTGRES")
	}
	db := &DB{
		dsnSuffix: dsn,
		dsn:       fmt.Sprintf("host=leader.%s.discoverd sslmode=disable %s", service, dsn),
		addr:      fmt.Sprintf("leader.%s.discoverd", service),
		stmts:     make(map[string]*sql.Stmt),
	}
	var err error
	db.DB, err = sql.Open("postgres", db.dsn)
	return db, err
}

type DB struct {
	*sql.DB

	dsnSuffix string

	mtx  sync.RWMutex
	dsn  string
	addr string

	stmts map[string]*sql.Stmt
}

var ErrNoServers = errors.New("postgres: no servers found")

func (db *DB) DSN() string {
	db.mtx.RLock()
	defer db.mtx.RUnlock()
	return db.dsn
}

func (db *DB) Addr() string {
	db.mtx.RLock()
	defer db.mtx.RUnlock()
	return db.addr
}

func (db *DB) Close() error {
	return db.DB.Close()
}

func (db *DB) prepare(query string) (*sql.Stmt, error) {
	// Fast path: get cached prepared statement
	db.mtx.RLock()
	stmt, ok := db.stmts[query]
	db.mtx.RUnlock()

	if !ok {
		// Cache miss: prepare query, fill cache
		var err error
		stmt, err = db.DB.Prepare(query)
		if err != nil {
			return nil, err
		}
		db.mtx.Lock()
		if prevStmt, ok := db.stmts[query]; ok {
			stmt.Close()
			stmt = prevStmt
		} else {
			db.stmts[query] = stmt
		}
		db.mtx.Unlock()
	}
	return stmt, nil
}

func (db *DB) Query(query string, args ...interface{}) (*sql.Rows, error) {
	stmt, err := db.prepare(query)
	if err != nil {
		return nil, err
	}
	return stmt.Query(args...)
}

func (db *DB) Exec(query string, args ...interface{}) error {
	stmt, err := db.prepare(query)
	if err != nil {
		return err
	}
	_, err = stmt.Exec(args...)
	return err
}

type Scanner interface {
	Scan(...interface{}) error
}

func (db *DB) QueryRow(query string, args ...interface{}) Scanner {
	stmt, err := db.prepare(query)
	if err != nil {
		return errRow{err}
	}
	return rowErrFixer{stmt.QueryRow(args...)}
}

func (db *DB) Begin() (*dbTx, error) {
	tx, err := db.DB.Begin()
	return &dbTx{tx}, err
}

type dbTx struct{ *sql.Tx }

func (tx *dbTx) QueryRow(query string, args ...interface{}) Scanner {
	return rowErrFixer{tx.Tx.QueryRow(query, args...)}
}

type errRow struct {
	err error
}

func (r errRow) Scan(...interface{}) error {
	return r.err
}

type rowErrFixer struct {
	s Scanner
}

func (f rowErrFixer) Scan(args ...interface{}) error {
	err := f.s.Scan(args...)
	if e, ok := err.(*pq.Error); ok && e.Code.Name() == "invalid_text_representation" && e.File == "uuid.c" && e.Routine == "string_to_uuid" {
		// invalid input syntax for uuid
		err = sql.ErrNoRows
	}
	return err
}

func CleanUUID(u string) string {
	return strings.Replace(u, "-", "", -1)
}

func FormatUUID(s string) string {
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}
