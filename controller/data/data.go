package data

import (
	"fmt"
	"strconv"
	"strings"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/inconshreveable/log15"
)

var ErrNotFound = ct.ErrNotFound
var logger = log15.New("component", "controller/data")

const DEFAULT_PAGE_SIZE = 1000

type PageToken struct {
	BeforeID *string
	Size     int

	beforeIDInt64 *int64
}

func ParsePageToken(tokenStr string) (*PageToken, error) {
	token := &PageToken{}
	if tokenStr == "" {
		token.Size = DEFAULT_PAGE_SIZE
		return token, nil
	}
	parts := strings.Split(tokenStr, ":")
	if len(parts) != 2 {
		return nil, fmt.Errorf("ParsePageToken Error: Invalid number of parts (%d != %d)", len(parts), 2)
	}
	if parts[0] != "" {
		token.BeforeID = &parts[0]
	}
	if parts[1] != "" && parts[1] != "0" {
		token.Size, _ = strconv.Atoi(parts[1])
	}
	if token.Size == 0 {
		token.Size = DEFAULT_PAGE_SIZE
	}
	return token, nil
}

func (t *PageToken) BeforeIDInt64() *int64 {
	if t.beforeIDInt64 != nil {
		return t.beforeIDInt64
	}
	if t.BeforeID == nil {
		return nil
	}
	beforeID, err := strconv.ParseInt(*t.BeforeID, 10, 64)
	if err != nil {
		return nil
	}
	t.beforeIDInt64 = &beforeID
	return t.beforeIDInt64
}

func (t *PageToken) String() string {
	if t == nil {
		return ""
	}
	var beforeID string
	if t.BeforeID != nil {
		beforeID = *t.BeforeID
	}
	return fmt.Sprintf("%s:%d", beforeID, t.Size)
}

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
	return strings.Split(s, sep)
}

func splitPGStringArray(artifactIDs string) []string {
	return split(artifactIDs[1:len(artifactIDs)-1], ",")
}
