package main

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/pkg/postgres"
)

func migrateDB(db *sql.DB) error {
	m := postgres.NewMigrations()
	m.Add(1,
		`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,
		`CREATE FUNCTION set_updated_at_column() RETURNS TRIGGER AS $$
	BEGIN
		NEW.updated_at = CURRENT_TIMESTAMP AT TIME ZONE 'UTC';
		RETURN NEW;
	END;
$$ language 'plpgsql'`,
		`CREATE TYPE route_type AS ENUM ('http', 'tcp')`,
		`CREATE TABLE routes (
	route_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
	parent_ref varchar(255) NOT NULL,
	type route_type NOT NULL,
	config json NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz
)`,
		`CREATE TRIGGER set_updated_at_routes
	BEFORE UPDATE ON routes FOR EACH ROW
	EXECUTE PROCEDURE set_updated_at_column()`,
		`CREATE FUNCTION notify_route() RETURNS TRIGGER AS $$
	BEGIN
		PERFORM pg_notify('routes', NEW.type::text || ':' || NEW.route_id::text);
		RETURN NULL;
	END;
$$ LANGUAGE plpgsql`,
		`CREATE TRIGGER notify_route
	AFTER INSERT OR UPDATE OR DELETE ON routes
	FOR EACH ROW EXECUTE PROCEDURE notify_route()`,
	)
	return m.Migrate(db)
}
