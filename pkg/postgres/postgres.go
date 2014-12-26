package postgres

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq"
	"github.com/flynn/flynn/discoverd/client"
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
	set, err := discoverd.NewServiceSet(service)
	if err != nil {
		log.Fatal(err)
	}
	defer set.Close()
	ch := set.Watch(true)
	for u := range ch {
		l := set.Leader()
		if l == nil {
			continue
		}
		if u.Online && u.Addr == l.Addr && u.Attrs["up"] == "true" && u.Attrs["username"] != "" && u.Attrs["password"] != "" {
			return u.Attrs["username"], u.Attrs["password"]
		}
	}
	panic("discoverd disconnected before postgres came up")
}

func Open(service, dsn string) (*DB, error) {
	if service == "" {
		service = os.Getenv("FLYNN_POSTGRES")
	}
	set, err := discoverd.NewServiceSet(service)
	if err != nil {
		return nil, err
	}
	db := &DB{
		set:       set,
		dsnSuffix: dsn,
		stmts:     make(map[string]*sql.Stmt),
	}
	firstErr := make(chan error)
	go db.followLeader(firstErr)
	return db, <-firstErr
}

type DB struct {
	*sql.DB

	set discoverd.ServiceSet

	dsnSuffix string

	mtx sync.RWMutex
	dsn string

	stmts map[string]*sql.Stmt
}

var ErrNoServers = errors.New("postgres: no servers found")

func (db *DB) followLeader(firstErr chan<- error) {
	for update := range db.set.Watch(true) {
		leader := db.set.Leader()
		if leader == nil || leader.Attrs["up"] != "true" {
			if firstErr != nil {
				firstErr <- ErrNoServers
				return
			}
			continue
		}
		if !update.Online || update.Addr != leader.Addr {
			continue
		}

		dsn := fmt.Sprintf("host=%s port=%s %s", leader.Host, leader.Port, db.dsnSuffix)
		db.mtx.Lock()
		db.dsn = dsn
		db.mtx.Unlock()

		if db.DB == nil {
			var err error
			db.DB, err = sql.Open("postgres", dsn)
			firstErr <- err
			if err != nil {
				return
			}
		} else {
			db.DB.SetDSN(dsn)
		}
	}
	// TODO: reconnect to discoverd here
}

func (db *DB) DSN() string {
	db.mtx.RLock()
	defer db.mtx.RUnlock()
	return db.dsn
}

func (db *DB) Close() error {
	if db.set != nil {
		db.set.Close()
	}
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
