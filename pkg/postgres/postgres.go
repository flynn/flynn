package postgres

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/dialer"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/sirenia/state"
)

const (
	InvalidTextRepresentation = "22P02"
	CheckViolation            = "23514"
	UniqueViolation           = "23505"
	RaiseException            = "P0001"
	ForeignKeyViolation       = "23503"
)

type Conf struct {
	Service  string
	User     string
	Password string
	Database string
}

var connectAttempts = attempt.Strategy{
	Min:   5,
	Total: 5 * time.Minute,
	Delay: 200 * time.Millisecond,
}

func New(connPool *pgx.ConnPool, conf *Conf) *DB {
	return &DB{connPool, conf}
}

func Wait(conf *Conf, afterConn func(*pgx.Conn) error) *DB {
	if conf == nil {
		conf = &Conf{
			Service:  os.Getenv("FLYNN_POSTGRES"),
			User:     os.Getenv("PGUSER"),
			Password: os.Getenv("PGPASSWORD"),
			Database: os.Getenv("PGDATABASE"),
		}
	}
	events := make(chan *discoverd.Event)
	stream, err := discoverd.NewService(conf.Service).Watch(events)
	if err != nil {
		shutdown.Fatal(err)
	}
	// wait for service meta that has sync or singleton primary
	for e := range events {
		if e.Kind&discoverd.EventKindServiceMeta == 0 || e.ServiceMeta == nil || len(e.ServiceMeta.Data) == 0 {
			continue
		}
		state := &state.State{}
		json.Unmarshal(e.ServiceMeta.Data, state)
		if state.Singleton || state.Sync != nil {
			break
		}
	}
	stream.Close()
	// TODO(titanous): handle discoverd disconnection

	// retry here as authentication may fail if DB is still
	// starting up.
	// TODO(jpg): switch this to use pgmanager to check if user
	// exists, we can also check for r/w with pgmanager
	var db *DB
	err = connectAttempts.Run(func() error {
		db, err = Open(conf, afterConn)
		return err
	})
	if err != nil {
		panic(err)
	}
	for {
		var readonly string
		// wait until read-write transactions are allowed
		if err := db.QueryRow("SHOW default_transaction_read_only").Scan(&readonly); err != nil || readonly == "on" {
			time.Sleep(100 * time.Millisecond)
			// TODO(titanous): add max wait here
			continue
		}
		return db
	}
}

func Open(conf *Conf, afterConn func(*pgx.Conn) error) (*DB, error) {
	connConfig := pgx.ConnConfig{
		Host:     fmt.Sprintf("leader.%s.discoverd", conf.Service),
		User:     conf.User,
		Database: conf.Database,
		Password: conf.Password,
		Dial:     dialer.Retry.Dial,
	}
	connPool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig:     connConfig,
		AfterConnect:   afterConn,
		MaxConnections: 20,
	})
	db := &DB{connPool, conf}
	return db, err
}

type DB struct {
	*pgx.ConnPool
	conf *Conf
}

func (db *DB) Exec(query string, args ...interface{}) error {
	_, err := db.ConnPool.Exec(query, args...)
	return err
}

func (db *DB) ExecRetry(query string, args ...interface{}) error {
	retries := 0
	max := 30
	for {
		_, err := db.ConnPool.Exec(query, args...)
		if err == pgx.ErrDeadConn && retries < max {
			retries++
			time.Sleep(1 * time.Second)
			continue
		}
		return err
	}
}

type Scanner interface {
	Scan(...interface{}) error
}

func (db *DB) QueryRow(query string, args ...interface{}) Scanner {
	return rowErrFixer{db.ConnPool.QueryRow(query, args...)}
}

func (db *DB) Begin() (*DBTx, error) {
	tx, err := db.ConnPool.Begin()
	return &DBTx{tx}, err
}

type DBTx struct{ *pgx.Tx }

func (tx *DBTx) Exec(query string, args ...interface{}) error {
	_, err := tx.Tx.Exec(query, args...)
	return err
}

func (tx *DBTx) QueryRow(query string, args ...interface{}) Scanner {
	return rowErrFixer{tx.Tx.QueryRow(query, args...)}
}

type rowErrFixer struct {
	s Scanner
}

func (f rowErrFixer) Scan(args ...interface{}) error {
	err := f.s.Scan(args...)
	if e, ok := err.(pgx.PgError); ok && e.Code == InvalidTextRepresentation && e.File == "uuid.c" && e.Routine == "string_to_uuid" {
		// invalid input syntax for uuid
		err = pgx.ErrNoRows
	}
	return err
}

func IsUniquenessError(err error, constraint string) bool {
	if e, ok := err.(pgx.PgError); ok && e.Code == UniqueViolation {
		return constraint == "" || constraint == e.ConstraintName
	}
	return false
}

func IsPostgresCode(err error, code string) bool {
	if e, ok := err.(pgx.PgError); ok && e.Code == code {
		return true
	}
	return false
}
