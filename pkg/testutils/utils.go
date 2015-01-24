package testutils

import (
	"fmt"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq"
)

func SetupPostgres(dbname string) error {
	if os.Getenv("PGDATABASE") != "" {
		dbname = os.Getenv("PGDATABASE")
	} else {
		os.Setenv("PGDATABASE", dbname)
	}
	if os.Getenv("PGSSLMODE") == "" {
		os.Setenv("PGSSLMODE", "disable")
	}

	db, err := sql.Open("postgres", "dbname=postgres")
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbname)); err != nil {
		return err
	}
	if _, err := db.Exec(fmt.Sprintf("CREATE DATABASE %s", dbname)); err != nil {
		return err
	}
	return nil
}
