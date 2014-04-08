package migrate

import "github.com/flynn/go-sql"

type Migration struct {
	ID    int
	Stmts []string
}

func NewMigrations() *Migrations {
	l := make(Migrations, 0)
	return &l
}

type Migrations []Migration

func (m *Migrations) Add(id int, stmts ...string) {
	*m = append(*m, Migration{ID: id, Stmts: stmts})
}

func (m Migrations) Migrate(db *sql.DB) error {
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

		if _, err := tx.Exec("LOCK TABLE schema_migrations IN ACCESS EXCLUSIVE MODE"); err != nil {
			tx.Rollback()
			return err
		}
		var tmp bool
		if err := tx.QueryRow("SELECT true FROM schema_migrations WHERE id = $1", migration.ID).Scan(&tmp); err != sql.ErrNoRows {
			tx.Rollback()
			if err == nil {
				continue
			}
			return err
		}

		for _, s := range migration.Stmts {
			_, err = tx.Exec(s)
			if err != nil {
				tx.Rollback()
				return err
			}
		}

		if _, err := tx.Exec("INSERT INTO schema_migrations (id) VALUES ($1)", migration.ID); err != nil {
			tx.Rollback()
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
