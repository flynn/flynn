package main

import (
	"github.com/flynn/flynn/pkg/postgres"
)

func migrateDB(db *postgres.DB) error {
	m := postgres.NewMigrations()
	m.Add(1,
		`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,
		`CREATE FUNCTION set_updated_at_column() RETURNS TRIGGER AS $$
	BEGIN
		NEW.updated_at = CURRENT_TIMESTAMP AT TIME ZONE 'UTC';
		RETURN NEW;
	END;
$$ language 'plpgsql'`,

		// tcp routes

		`
CREATE TABLE tcp_routes (
	id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
	parent_ref varchar(255) NOT NULL,
	service varchar(255) NOT NULL CHECK (service <> ''),
	port integer NOT NULL CHECK (port > 0 AND port < 65535),
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz
)`,
		`
CREATE UNIQUE INDEX tcp_routes_port_key ON tcp_routes
USING btree (port) WHERE deleted_at IS NULL`,
		`
CREATE TRIGGER set_updated_at_tcp_routes
	BEFORE UPDATE ON tcp_routes FOR EACH ROW
	EXECUTE PROCEDURE set_updated_at_column()`,
		`
CREATE OR REPLACE FUNCTION notify_tcp_route_update() RETURNS TRIGGER AS $$
BEGIN
	PERFORM pg_notify('tcp_routes', NEW.id::varchar);
	RETURN NULL;
END;
$$ LANGUAGE plpgsql`,
		`
CREATE TRIGGER notify_tcp_route_update
	AFTER INSERT OR UPDATE OR DELETE ON tcp_routes
	FOR EACH ROW EXECUTE PROCEDURE notify_tcp_route_update()`,

		// http routes

		`
CREATE TABLE http_routes (
	id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
	parent_ref varchar(255) NOT NULL,
	service varchar(255) NOT NULL CHECK (service <> ''),
	domain varchar(255) NOT NULL CHECK (domain <> ''),
	sticky bool NOT NULL DEFAULT FALSE,
	tls_cert text,
	tls_key text,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz
)`,
		`
CREATE UNIQUE INDEX http_routes_domain_key ON http_routes
	USING btree (domain) WHERE deleted_at IS NULL`,
		`
CREATE TRIGGER set_updated_at_http_routes
	BEFORE UPDATE ON http_routes FOR EACH ROW
	EXECUTE PROCEDURE set_updated_at_column()`,
		`
CREATE OR REPLACE FUNCTION notify_http_route_update() RETURNS TRIGGER AS $$
BEGIN
	PERFORM pg_notify('http_routes', NEW.id::varchar);
	RETURN NULL;
END;
$$ LANGUAGE plpgsql`,
		`
CREATE TRIGGER notify_http_route_update
	AFTER INSERT OR UPDATE OR DELETE ON http_routes
	FOR EACH ROW EXECUTE PROCEDURE notify_http_route_update()`,
	)
	return m.Migrate(db)
}
