package main

import (
	"errors"
	"regexp"
	"strings"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/go-sql"
)

var ErrNotFound = errors.New("common: not found")
var ErrInvalidLoginToken = ct.ValidationError{Field: "token", Message: "Incorrect token"}

type ServerError struct {
	Message string `json:"message"`
}

func CleanUUID(u string) string {
	return strings.Replace(u, "-", "", -1)
}

var validUUIDPattern = regexp.MustCompile("^[a-f0-9]{32}$")

func ValidUUID(u string) bool {
	return validUUIDPattern.MatchString(u)
}

type RowQueryer interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

type Execer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}
