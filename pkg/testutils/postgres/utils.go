package pgtestutils

import (
	"fmt"
	"os"

	"github.com/jackc/pgx"
)

func SetupPostgres(dbname string) error {
	if os.Getenv("PGSSLMODE") == "" {
		os.Setenv("PGSSLMODE", "disable")
	}

	connConfig := pgx.ConnConfig{
		Host:     "/var/run/postgresql",
		Database: "postgres",
	}

	db, err := pgx.Connect(connConfig)
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
