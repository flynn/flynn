package main

import (
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/jackc/pgx"
)

var migrations *postgres.Migrations

func init() {
	migrations = postgres.NewMigrations()
	migrations.Add(1, `
CREATE TABLE builds (
  id          text        PRIMARY KEY,
  commit      text        NOT NULL,
  branch      text        NOT NULL,
  merge       boolean     NOT NULL DEFAULT false,
  state       text        NOT NULL DEFAULT 'pending',
  version     text        NOT NULL DEFAULT 'v2',
  failures    jsonb       NOT NULL DEFAULT '[]'::jsonb,
  created_at  timestamptz NOT NULL DEFAULT now(),
  description text,
  log_url     text,
  log_file    text,
  duration    text,
  reason      text,
  issue_link  text
)
	`)
}

var preparedStatements = map[string]string{
	"build_list":    buildListQuery,
	"build_select":  buildSelectQuery,
	"build_pending": buildPendingQuery,
	"build_insert":  buildInsertQuery,
}

const (
	buildListQuery = `
SELECT id, commit, branch, merge, state, version, failures, created_at, description, log_url, log_file, duration, reason, issue_link
FROM builds
ORDER BY created_at DESC
LIMIT $1
	`
	buildSelectQuery = `
SELECT id, commit, branch, merge, state, version, failures, created_at, description, log_url, log_file, duration, reason, issue_link
FROM builds
WHERE id = $1
	`
	buildPendingQuery = `
SELECT id, commit, branch, merge, state, version, failures, created_at, description, log_url, log_file, duration, reason, issue_link
FROM builds
WHERE state = 'pending'
ORDER BY created_at DESC
	`
	buildInsertQuery = `
INSERT INTO builds (
  id, commit, branch, merge, state, version, failures, created_at, description, log_url, log_file, duration, reason, issue_link
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
ON CONFLICT (id) DO UPDATE SET
  state = $5, failures = $7, description = $9, log_url = $10, log_file = $11, duration = $12, reason = $13, issue_link = $14

	`
)

func PrepareStatements(conn *pgx.Conn) error {
	for name, sql := range preparedStatements {
		if _, err := conn.Prepare(name, sql); err != nil {
			return err
		}
	}
	return nil
}
