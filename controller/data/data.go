package data

import (
	"fmt"
	"strings"

	controller "github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/inconshreveable/log15"
)

var ErrNotFound = controller.ErrNotFound
var logger = log15.New("component", "controller/data")

type rowQueryer interface {
	QueryRow(query string, args ...interface{}) postgres.Scanner
}

func OpenAndMigrateDB(conf *postgres.Conf) *postgres.DB {
	db := postgres.Wait(conf, nil)

	if err := MigrateDB(db); err != nil {
		shutdown.Fatal(err)
	}

	// Reconnect, preparing statements now that schema is migrated
	db.Close()
	db = postgres.Wait(conf, PrepareStatements)

	return db
}

func CreateEvent(dbExec func(string, ...interface{}) error, e *ct.Event, data interface{}) error {
	args := []interface{}{e.ObjectID, string(e.ObjectType), data}
	fields := []string{"object_id", "object_type", "data"}
	if e.AppID != "" {
		fields = append(fields, "app_id")
		args = append(args, e.AppID)
	}
	if e.UniqueID != "" {
		fields = append(fields, "unique_id")
		args = append(args, e.UniqueID)
	}
	if e.Op != "" {
		fields = append(fields, "op")
		args = append(args, e.Op)
	}
	query := "INSERT INTO events ("
	for i, n := range fields {
		if i > 0 {
			query += ","
		}
		query += n
	}
	query += ") VALUES ("
	for i := range fields {
		if i > 0 {
			query += ","
		}
		query += fmt.Sprintf("$%d", i+1)
	}
	query += ")"
	return dbExec(query, args...)
}

func split(s string, sep string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
