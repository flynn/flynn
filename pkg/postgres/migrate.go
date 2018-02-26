package postgres

import (
	"strconv"
	"time"

	"github.com/jackc/pgx"
	"github.com/inconshreveable/log15"
)

type Step func(*DBTx) error

type Migration struct {
	ID    int
	Steps []Step
}

func NewMigrations() *Migrations {
	l := make(Migrations, 0)
	return &l
}

type Migrations []Migration

func (m *Migrations) Add(id int, stmts ...string) {
	steps := make([]Step, len(stmts))
	for i := range stmts {
		stmt := stmts[i]
		steps[i] = func(tx *DBTx) error { return tx.Exec(stmt) }
	}
	m.AddSteps(id, steps...)
}

func (m *Migrations) AddSteps(id int, steps ...Step) {
	*m = append(*m, Migration{ID: id, Steps: steps})
}

func (m Migrations) Migrate(db *DB) error {
	var initialized bool
	for _, migration := range m {
		if !initialized {
			db.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (id bigint PRIMARY KEY)")
			initialized = true
		}

		tx, err := db.Begin()
		if err != nil {
			return err
		}

		if err := tx.Exec("LOCK TABLE schema_migrations IN EXCLUSIVE MODE"); err != nil {
			tx.Rollback()
			return err
		}
		var tmp bool
		if err := tx.QueryRow("SELECT true FROM schema_migrations WHERE id = $1", migration.ID).Scan(&tmp); err != pgx.ErrNoRows {
			tx.Rollback()
			if err == nil {
				continue
			}
			return err
		}

		for _, step := range migration.Steps {
			if err := step(tx); err != nil {
				tx.Rollback()
				return err
			}
		}

		if err := tx.Exec("INSERT INTO schema_migrations (id) VALUES ($1)", migration.ID); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Exec("SELECT pg_notify('schema_migrations', $1)", strconv.Itoa(migration.ID)); err != nil {
			tx.Rollback()
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func ResetOnMigration(db *DB, log log15.Logger, doneCh chan struct{}) {
	for {
		listener, err := db.Listen("schema_migrations", log)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}
	outer:
		for {
			select {
			case n, ok := <-listener.Notify:
				if !ok {
					log.Warn("migration listener disconnected, reconnecting in 5 seconds")
					time.Sleep(5 * time.Second)
					listener.Close()
					break outer
				}
				log.Warn("new schema migration, resetting conn pool", "id", n.Payload)
				db.Reset()
			case <-doneCh:
				listener.Close()
				return
			}
		}
	}
}
