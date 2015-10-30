package main

import (
	"github.com/flynn/flynn/pkg/postgres"
)

func migrateDB(db *postgres.DB) error {
	m := postgres.NewMigrations()
	m.Add(1,
		`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,

		`CREATE TABLE artifacts (
    artifact_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    type text NOT NULL,
    uri text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
)`,
		`CREATE UNIQUE INDEX ON artifacts (type, uri) WHERE deleted_at IS NULL`,

		`CREATE TABLE releases (
    release_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    artifact_id uuid REFERENCES artifacts (artifact_id),
    meta jsonb,
    env jsonb,
    processes jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
)`,

		`CREATE TYPE deployment_strategy AS ENUM ('all-at-once', 'one-by-one', 'postgres')`,

		`CREATE TABLE apps (
    app_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    name text NOT NULL,
    release_id uuid REFERENCES releases (release_id),
	meta jsonb,
	strategy deployment_strategy NOT NULL DEFAULT 'all-at-once',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
)`,
		`CREATE UNIQUE INDEX ON apps (name) WHERE deleted_at IS NULL`,

		`CREATE SEQUENCE event_ids`,
		`CREATE TYPE event_type AS ENUM ('app_deletion', 'app', 'app_release', 'deployment', 'job', 'scale', 'release', 'artifact', 'provider', 'resource', 'resource_deletion', 'key', 'key_deletion', 'route', 'route_deletion', 'domain_migration')`,
		`CREATE TABLE events (
    event_id    bigint         PRIMARY KEY DEFAULT nextval('event_ids'),
    app_id      uuid           REFERENCES apps (app_id),
    object_type event_type NOT NULL,
    object_id   text           NOT NULL,
    unique_id   text,
    data        jsonb,
    created_at  timestamptz    NOT NULL DEFAULT now()
)`,

		`CREATE INDEX ON events (object_type)`,
		`CREATE UNIQUE INDEX ON events (unique_id)`,
		`CREATE FUNCTION notify_event() RETURNS TRIGGER AS $$
    BEGIN
  IF NEW.app_id IS NOT NULL THEN
    PERFORM pg_notify('events', NEW.event_id || ':' || NEW.app_id);
  ELSE
		PERFORM pg_notify('events', NEW.event_id::text);
  END IF;
	RETURN NULL;
    END;
$$ LANGUAGE plpgsql`,
		`CREATE TRIGGER notify_event
    AFTER INSERT ON events
    FOR EACH ROW EXECUTE PROCEDURE notify_event()`,

		`CREATE TABLE formations (
    app_id uuid NOT NULL REFERENCES apps (app_id),
    release_id uuid NOT NULL REFERENCES releases (release_id),
    processes jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
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

		`CREATE TABLE providers (
    provider_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    name text NOT NULL UNIQUE,
    url text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
)`,

		`CREATE TABLE resources (
    resource_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    provider_id uuid NOT NULL REFERENCES providers (provider_id),
    external_id text NOT NULL,
    env jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    UNIQUE (provider_id, external_id)
)`,

		`CREATE TABLE app_resources (
    app_id uuid NOT NULL REFERENCES apps (app_id),
    resource_id uuid NOT NULL REFERENCES resources (resource_id),
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    PRIMARY KEY (app_id, resource_id)
)`,
		`CREATE INDEX ON app_resources (resource_id)`,

		`CREATE TYPE job_state AS ENUM ('starting', 'up', 'down', 'crashed', 'failed')`,
		`CREATE TABLE job_cache (
    job_id text PRIMARY KEY,
    app_id uuid NOT NULL REFERENCES apps (app_id),
    release_id uuid NOT NULL REFERENCES releases (release_id),
    process_type text,
    state job_state NOT NULL,
    meta jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
)`,
		`CREATE FUNCTION check_job_state() RETURNS OPAQUE AS $$
    BEGIN
        IF NEW.state < OLD.state THEN
	    RAISE EXCEPTION 'invalid job state transition: % -> %', OLD.state, NEW.state USING ERRCODE = 'check_violation';
        ELSE
	    RETURN NEW;
        END IF;
    END;
$$ LANGUAGE plpgsql`,
		`CREATE TRIGGER job_state_trigger
    AFTER UPDATE ON job_cache
    FOR EACH ROW EXECUTE PROCEDURE check_job_state()`,

		`CREATE SEQUENCE name_ids MAXVALUE 4294967295`,
		`CREATE TABLE deployments (
    deployment_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    app_id uuid NOT NULL,
    old_release_id uuid REFERENCES releases (release_id),
    new_release_id uuid NOT NULL REFERENCES releases (release_id),
    strategy deployment_strategy NOT NULL,
    processes jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz)`,

		`CREATE UNIQUE INDEX isolate_deploys ON deployments (app_id)
    WHERE finished_at is NULL`,

		`CREATE TABLE domain_migrations (
			migration_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
			old_domain text NOT NULL,
			domain text NOT NULL,
			old_tls_cert jsonb,
			tls_cert jsonb,
			created_at timestamptz NOT NULL DEFAULT now(),
			finished_at timestamptz)`,
	)
	m.Add(2,
		`CREATE TABLE que_jobs (
    priority     smallint    NOT NULL DEFAULT 100,
    run_at       timestamptz NOT NULL DEFAULT now(),
    job_id       bigserial   NOT NULL,
    job_class    text        NOT NULL,
    args         json        NOT NULL DEFAULT '[]'::json,
    error_count  integer     NOT NULL DEFAULT 0,
    last_error   text,
    queue        text        NOT NULL DEFAULT '',
    locked_until timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT que_jobs_pkey PRIMARY KEY (queue, priority, run_at, job_id))`,
		`COMMENT ON TABLE que_jobs IS '3'`,
	)
	return m.Migrate(db)
}
