package main

import (
	"strings"
	"sync"

	"github.com/flynn/go-sql"
	"github.com/flynn/pq"
)

type DB struct {
	stmts map[string]*sql.Stmt
	mtx   sync.RWMutex
	*sql.DB

	db dbWrapper
}

func NewDB(db dbWrapper) *DB {
	return &DB{
		stmts: make(map[string]*sql.Stmt),
		DB:    db.Database(),
	}
}

func (db *DB) DSN() string {
	return db.db.DSN()
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

func cleanUUID(u string) string {
	return strings.Replace(u, "-", "", -1)
}
