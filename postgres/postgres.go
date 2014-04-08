package postgres

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-sql"
	_ "github.com/flynn/pq"
)

func Open(service, dsn string) (*DB, error) {
	if service == "" {
		service = os.Getenv("FLYNN_POSTGRES")
	}
	set, err := discoverd.NewServiceSet(service)
	if err != nil {
		return nil, err
	}
	db := &DB{set: set, dsnSuffix: dsn}
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

func (db *DB) Database() *sql.DB {
	return db.DB
}

func (db *DB) Close() error {
	db.set.Close()
	return db.DB.Close()
}
