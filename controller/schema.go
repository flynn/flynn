package main

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/pkg/postgres"
)

func migrateDB(db *sql.DB) error {
	m := postgres.NewMigrations()
	m.Add(1,
		`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,
		`CREATE EXTENSION IF NOT EXISTS "hstore"`,

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
    data text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
)`,

		`CREATE TYPE deployment_strategy AS ENUM ('all-at-once', 'one-by-one', 'postgres')`,

		`CREATE TABLE apps (
    app_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    name text NOT NULL,
    release_id uuid REFERENCES releases (release_id),
	meta hstore,
	strategy deployment_strategy NOT NULL DEFAULT 'all-at-once',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
)`,
		`CREATE UNIQUE INDEX ON apps (name) WHERE deleted_at IS NULL`,

		`CREATE TABLE formations (
    app_id uuid NOT NULL REFERENCES apps (app_id),
    release_id uuid NOT NULL REFERENCES releases (release_id),
    processes hstore,
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

		`CREATE TABLE keys (
    key_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    fingerprint text NOT NULL,
    key text NOT NULL,
    comment text,
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
)`,
		`CREATE UNIQUE INDEX ON keys (fingerprint) WHERE deleted_at IS NULL`,

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
    env hstore,
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
    job_id text NOT NULL,
    host_id text NOT NULL,
    app_id uuid NOT NULL REFERENCES apps (app_id),
    release_id uuid NOT NULL REFERENCES releases (release_id),
    process_type text,
    state job_state NOT NULL,
    meta hstore,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (job_id, host_id)
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

		`CREATE SEQUENCE job_event_ids`,
		`CREATE TABLE job_events (
    event_id bigint PRIMARY KEY DEFAULT nextval('job_event_ids'),
    job_id text NOT NULL,
    host_id text NOT NULL,
    app_id uuid NOT NULL REFERENCES apps (app_id),
    state job_state NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (job_id, host_id) REFERENCES job_cache (job_id, host_id)
)`,
		`CREATE UNIQUE INDEX ON job_events (job_id, host_id, app_id, state)`,
		`CREATE FUNCTION notify_job_event() RETURNS TRIGGER AS $$
    BEGIN
    PERFORM pg_notify('job_events:' || NEW.app_id, NEW.event_id || '');
        RETURN NULL;
    END;
$$ LANGUAGE plpgsql`,

		`CREATE TRIGGER notify_job_event
    AFTER INSERT ON job_events
    FOR EACH ROW EXECUTE PROCEDURE notify_job_event()`,

		`CREATE SEQUENCE name_ids MAXVALUE 4294967295`,
		`CREATE TABLE deployments (
    deployment_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    app_id uuid NOT NULL,
    old_release_id uuid REFERENCES releases (release_id),
    new_release_id uuid NOT NULL REFERENCES releases (release_id),
    strategy deployment_strategy NOT NULL,
    processes hstore,
    created_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz)`,

		`CREATE UNIQUE INDEX isolate_deploys ON deployments (app_id)
    WHERE finished_at is NULL`,

		`CREATE SEQUENCE deployment_event_ids`,
		`CREATE TYPE deployment_status AS ENUM ('running', 'complete', 'failed')`,
		`CREATE TABLE deployment_events (
    event_id bigint PRIMARY KEY DEFAULT nextval('deployment_event_ids'),
    deployment_id uuid NOT NULL REFERENCES deployments (deployment_id),
    release_id uuid NOT NULL REFERENCES releases (release_id),
    status deployment_status NOT NULL DEFAULT 'running',
    job_type text,
    job_state text,
    created_at timestamptz NOT NULL DEFAULT now())`,

		`CREATE FUNCTION notify_deployment_event() RETURNS TRIGGER AS $$
    BEGIN
    PERFORM pg_notify('deployment_events:' || NEW.deployment_id, NEW.event_id || '');
    RETURN NULL;
    END;
$$ LANGUAGE plpgsql`,

		`CREATE TRIGGER notify_deployment_event
    AFTER INSERT ON deployment_events
    FOR EACH ROW EXECUTE PROCEDURE notify_deployment_event()`,
	)
	m.Add(2,
		`CREATE TABLE que_jobs (
    priority    smallint    NOT NULL DEFAULT 100,
    run_at      timestamptz NOT NULL DEFAULT now(),
    job_id      bigserial   NOT NULL,
    job_class   text        NOT NULL,
    args        json        NOT NULL DEFAULT '[]'::json,
    error_count integer     NOT NULL DEFAULT 0,
    last_error  text,
    queue       text        NOT NULL DEFAULT '',

    CONSTRAINT que_jobs_pkey PRIMARY KEY (queue, priority, run_at, job_id))`,
		`COMMENT ON TABLE que_jobs IS '3'`,
	)
	return m.Migrate(db)
}
