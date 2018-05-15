package main

import (
	"github.com/flynn/flynn/pkg/postgres"
)

var migrations *postgres.Migrations

func init() {
	migrations = postgres.NewMigrations()
	migrations.Add(1,
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
	migrations.Add(2,
		`ALTER TABLE http_routes ADD COLUMN path text NOT NULL DEFAULT '/'`,
		`DROP INDEX http_routes_domain_key`,
		`CREATE UNIQUE INDEX http_routes_domain_path_key ON http_routes
		 USING btree (domain, path) WHERE deleted_at IS NULL`,
		`
CREATE OR REPLACE FUNCTION check_http_route_update() RETURNS TRIGGER AS $$
DECLARE
	default_route RECORD;
	dependent_routes int;
BEGIN
    -- If NEW.deleted_at is NOT NULL then we are processing a delete
	-- We also catch entire row deletions here but they shouldn't occur.
    IF NEW IS NULL OR NEW.deleted_at IS NOT NULL THEN
		-- If we are removing a default route ensure no dependent routes left
		IF OLD.path = '/' THEN
			SELECT count(*) INTO dependent_routes FROM http_routes
			WHERE domain = OLD.domain AND path <> '/' AND deleted_at IS NULL;
			IF dependent_routes > 0 THEN
				RAISE EXCEPTION 'default route for % has dependent routes', OLD.domain;
			END IF;
		END IF;
		RETURN NEW;
	END IF;

	-- If no path supplied then override it to '/', the default path
	IF NEW.path = '' OR NULL THEN
		NEW.path := '/';
	END IF;

	-- If path isn't terminated by a slash then add it
	IF substring(NEW.path from '.$') != '/' THEN
		NEW.path := NEW.path || '/';
	END IF;

	-- Validate the path
	IF NEW.path !~* '^\/(.*\/)?$' THEN
		RAISE EXCEPTION 'path % is not valid', NEW.path;
	END IF;

	-- If path not the default then validate that a default route exists
	IF NEW.path <> '/' THEN
		SELECT INTO default_route FROM http_routes
		WHERE domain = NEW.domain AND path = '/' AND deleted_at IS NULL;
		IF NOT FOUND THEN
			RAISE EXCEPTION 'default route for domain % not found', NEW.domain;
		END IF;
	END IF;
	RETURN NEW;
END;
$$ LANGUAGE plpgsql`,
		`
CREATE TRIGGER check_http_route_update
	BEFORE INSERT OR UPDATE OR DELETE ON http_routes
	FOR EACH ROW
	EXECUTE PROCEDURE check_http_route_update()`,
	)
	migrations.Add(3,
		// Ensure the default is set on the path column. We set this above, but
		// releases v20151214.1, v20151214.0, v20151213.1, and v20151213.0
		// didn't have the default specified, so this will fix any databases
		// from those versions that have the broken release and have already run
		// migration 2.
		`ALTER TABLE http_routes ALTER COLUMN path SET DEFAULT '/'`,
	)
	migrations.Add(4,
		`ALTER TABLE tcp_routes ADD COLUMN leader boolean NOT NULL DEFAULT FALSE`,
		`ALTER TABLE http_routes ADD COLUMN leader boolean NOT NULL DEFAULT FALSE`,
	)
	migrations.Add(5,
		`CREATE EXTENSION IF NOT EXISTS "pgcrypto"`,
		`CREATE TABLE certificates (
			id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
			cert text NOT NULL,
			key text NOT NULL,
			cert_sha256 bytea NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			deleted_at timestamptz
		)`,
		`CREATE UNIQUE INDEX ON certificates (cert_sha256) WHERE deleted_at IS NULL`,
		`CREATE TABLE route_certificates (
			http_route_id uuid NOT NULL REFERENCES http_routes (id) ON DELETE CASCADE,
			certificate_id uuid NOT NULL REFERENCES certificates (id) ON DELETE RESTRICT,
			PRIMARY KEY (http_route_id, certificate_id)
		)`,
		// Create certificate for http_routes with tls_key set,
		// taking care not to create duplicates
		`DO $$
		DECLARE
			http_route RECORD;
			cert RECORD;
			certdigest bytea;
		BEGIN
			FOR http_route IN SELECT * FROM http_routes WHERE tls_key IS NOT NULL LOOP
				SELECT INTO certdigest digest(regexp_replace(regexp_replace(http_route.tls_cert, E'^[ \\n]+', '', ''), E'[ \\n]+$', '', ''), 'sha256');
				SELECT INTO cert * FROM certificates WHERE cert_sha256 = certdigest;

				IF NOT FOUND THEN
					INSERT INTO certificates (cert, key, cert_sha256)
					VALUES (http_route.tls_cert, http_route.tls_key, certdigest)
					RETURNING * INTO cert;
				END IF;

				INSERT INTO route_certificates (http_route_id, certificate_id) VALUES(http_route.id, cert.id);
			END LOOP;
		END $$`,
		`ALTER TABLE http_routes DROP COLUMN tls_cert`,
		`ALTER TABLE http_routes DROP COLUMN tls_key`,
		`
CREATE OR REPLACE FUNCTION notify_route_certificates_update() RETURNS TRIGGER AS $$
BEGIN
	IF (TG_OP = 'DELETE') THEN
		PERFORM pg_notify('http_routes', OLD.http_route_id::varchar);
	ELSIF (TG_OP = 'UPDATE') THEN
		PERFORM pg_notify('http_routes', OLD.http_route_id::varchar);
		PERFORM pg_notify('http_routes', NEW.http_route_id::varchar);
	ELSIF (TG_OP = 'INSERT') THEN
		PERFORM pg_notify('http_routes', NEW.http_route_id::varchar);
	END IF;
	RETURN NULL;
END;
$$ LANGUAGE plpgsql`,
		`
CREATE TRIGGER notify_route_certificates_update
	AFTER INSERT OR UPDATE OR DELETE ON route_certificates
	FOR EACH ROW EXECUTE PROCEDURE notify_route_certificates_update()`,
	)
	migrations.Add(6,
		`ALTER TABLE tcp_routes ADD COLUMN drain_backends boolean NOT NULL DEFAULT TRUE`,
		`ALTER TABLE http_routes ADD COLUMN drain_backends boolean NOT NULL DEFAULT TRUE`,
		`UPDATE http_routes SET drain_backends = false WHERE service = 'controller'`,
	)
	migrations.Add(7,
		`ALTER TABLE http_routes ADD COLUMN port integer NOT NULL DEFAULT 0 CHECK (port > -1 AND port < 65535)`,
	)
	migrations.Add(8,
		`DROP INDEX http_routes_domain_path_key`,
		`CREATE UNIQUE INDEX http_routes_domain_port_path_key ON http_routes
		USING btree (domain, port, path) WHERE deleted_at IS NULL`,
	)
	migrations.Add(9,
		`UPDATE http_routes r1 SET drain_backends = true WHERE (SELECT count(*) FROM http_routes r2 WHERE r2.service = r1.service AND drain_backends = true AND deleted_at IS NULL) > 0`,
		`UPDATE tcp_routes r1 SET drain_backends = true WHERE (SELECT count(*) FROM tcp_routes r2 WHERE r2.service = r1.service AND drain_backends = true AND deleted_at IS NULL) > 0`,
		`
CREATE OR REPLACE FUNCTION check_http_route_drain_backends() RETURNS TRIGGER AS $$
DECLARE
	drain_routes int;
BEGIN
	IF NEW IS NULL OR NEW.deleted_at IS NOT NULL THEN
		RETURN NEW;
	END IF;

	SELECT count(*) INTO drain_routes FROM http_routes
	WHERE service = NEW.service AND deleted_at IS NULL AND drain_backends <> NEW.drain_backends;
	IF drain_routes > 0 THEN
		RAISE EXCEPTION 'cannot create route with drain_backends mismatch, other routes for service % exist with drain_backends toggled', NEW.service;
	END IF;

	RETURN NEW;
END;
$$ LANGUAGE plpgsql`,
		`CREATE TRIGGER check_http_route_drain_backends
	BEFORE INSERT OR UPDATE OR DELETE ON http_routes
	FOR EACH ROW
	EXECUTE PROCEDURE check_http_route_drain_backends()`,
		`
CREATE OR REPLACE FUNCTION check_tcp_route_drain_backends() RETURNS TRIGGER AS $$
DECLARE
	drain_routes int;
BEGIN
	IF NEW IS NULL OR NEW.deleted_at IS NOT NULL THEN
		RETURN NEW;
	END IF;

	SELECT count(*) INTO drain_routes FROM tcp_routes
	WHERE service = NEW.service AND deleted_at IS NULL AND drain_backends <> NEW.drain_backends;
	IF drain_routes > 0 THEN
		RAISE EXCEPTION 'cannot create route with drain_backends mismatch, other routes for service % exist with drain_backends toggled', NEW.service;
	END IF;

	RETURN NEW;
END;
$$ LANGUAGE plpgsql`,
		`CREATE TRIGGER check_http_route_drain_backends
	BEFORE INSERT OR UPDATE OR DELETE ON tcp_routes
	FOR EACH ROW
	EXECUTE PROCEDURE check_http_route_drain_backends()`,
	)
}

func migrateDB(db *postgres.DB) error {
	return migrations.Migrate(db)
}
