package main

import (
	"github.com/flynn/go-flynn/migrate"
	"github.com/flynn/go-sql"
)

func migrateDB(db *sql.DB) error {
	m := migrate.NewMigrations()
	m.Add(1,
		`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,
		`CREATE EXTENSION IF NOT EXISTS "hstore"`,

		`CREATE TABLE artifacts (
    artifact_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    type text NOT NULL,
    uri text NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT current_timestamp,
    deleted_at timestamp with time zone,
    UNIQUE (type, uri)
)`,

		`CREATE TABLE releases (
    release_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    artifact_id uuid NOT NULL REFERENCES artifacts (artifact_id),
    data text NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT current_timestamp,
    deleted_at timestamp with time zone
)`,

		`CREATE TABLE apps (
    app_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    name text UNIQUE NOT NULL,
    release_id uuid REFERENCES releases (release_id),
    created_at timestamp with time zone NOT NULL DEFAULT current_timestamp,
    updated_at timestamp with time zone NOT NULL DEFAULT current_timestamp,
    deleted_at timestamp with time zone
)`,

		`CREATE TABLE formations (
    app_id uuid NOT NULL REFERENCES apps (app_id),
    release_id uuid NOT NULL REFERENCES releases (release_id),
    processes hstore,
    created_at timestamp with time zone NOT NULL DEFAULT current_timestamp,
    updated_at timestamp with time zone NOT NULL DEFAULT current_timestamp,
    deleted_at timestamp with time zone,
    PRIMARY KEY (app_id, release_id)
)`,

		`CREATE FUNCTION notify_formation() RETURNS TRIGGER AS $$
    BEGIN
        PERFORM pg_notify('formations', NEW.app_id || ':' || NEW.release_id);
        RETURN NULL;
    END;
$$ LANGUAGE plpgsql`,

		`CREATE TRIGGER notify_formation
    AFTER INSERT OR UPDATE ON formations
    FOR EACH ROW EXECUTE PROCEDURE notify_formation()`,

		`CREATE TABLE keys (
    key_id text PRIMARY KEY,
    key text NOT NULL,
    comment text,
    created_at timestamp with time zone NOT NULL DEFAULT current_timestamp,
    deleted_at timestamp with time zone
)`,

		`CREATE TABLE app_logs (
    app_id uuid NOT NULL REFERENCES apps (app_id),
    log_id bigint NOT NULL,
    event text NOT NULL,
    subject_id uuid,
    data text NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT current_timestamp,
    PRIMARY KEY (app_id, log_id)
)`,

		`CREATE TABLE app_log_ids (
    app_id uuid PRIMARY KEY REFERENCES apps (app_id),
    log_id bigint NOT NULL
)`,

		`CREATE FUNCTION next_log_id(uuid) RETURNS bigint AS $$
DECLARE
    in_app_id ALIAS FOR $1;
    next_log_id bigint;
BEGIN
    next_log_id := log_id FROM app_log_ids WHERE app_id = in_app_id FOR UPDATE;
    IF next_log_id IS NULL THEN
        next_log_id := 0;
        BEGIN
            INSERT INTO app_log_ids (app_id, log_id) VALUES (in_app_id, next_log_id+1);
            RETURN next_log_id;
        EXCEPTION WHEN unique_violation THEN
            next_log_id := log_id FROM app_log_ids WHERE app_id = in_app_id FOR UPDATE;
        END;
    END IF;

    UPDATE app_log_ids SET log_id = log_id+1 WHERE app_id = in_app_id;
    RETURN next_log_id;
END
$$ LANGUAGE plpgsql`,

		`CREATE TABLE providers (
    provider_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    name text NOT NULL UNIQUE,
    url text NOT NULL UNIQUE,
    created_at timestamp with time zone NOT NULL DEFAULT current_timestamp,
    updated_at timestamp with time zone NOT NULL DEFAULT current_timestamp,
    deleted_at timestamp with time zone
)`,
	)
	return m.Migrate(db)
}
