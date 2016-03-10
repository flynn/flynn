package main

import (
	"github.com/flynn/flynn/pkg/postgres"
)

var migrations *postgres.Migrations

func init() {
	migrations = postgres.NewMigrations()
	migrations.Add(1,
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
	migrations.Add(2,
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
	migrations.Add(3,
		`ALTER TABLE apps ADD COLUMN deploy_timeout integer NOT NULL DEFAULT 30`,
		`UPDATE apps SET deploy_timeout = 120 WHERE name = 'controller'`,
		`UPDATE apps SET deploy_timeout = 120 WHERE name = 'postgres'`,
	)
	migrations.Add(4,
		`CREATE TABLE deployment_strategies (name text PRIMARY KEY)`,
		`INSERT INTO deployment_strategies (name) VALUES
			('all-at-once'), ('one-by-one'), ('postgres')`,
		`ALTER TABLE apps ALTER COLUMN strategy TYPE text`,
		`ALTER TABLE apps ALTER COLUMN strategy SET DEFAULT 'all-at-once'`,
		`ALTER TABLE apps ADD CONSTRAINT apps_strategy_fkey FOREIGN KEY (strategy) REFERENCES deployment_strategies (name)`,
		`ALTER TABLE deployments ALTER COLUMN strategy TYPE text`,
		`ALTER TABLE deployments ADD CONSTRAINT deployments_strategy_fkey FOREIGN KEY (strategy) REFERENCES deployment_strategies (name)`,
		`DROP TYPE deployment_strategy`,

		`CREATE TABLE event_types (name text PRIMARY KEY)`,
		`INSERT INTO event_types (name) VALUES
			('app_deletion'), ('app'), ('app_release'), ('deployment'),
			('job'),('scale'), ('release'), ('artifact'), ('provider'),
			('resource'), ('resource_deletion'), ('key'), ('key_deletion'),
			('route'), ('route_deletion'), ('domain_migration')`,
		`ALTER TABLE events ALTER COLUMN object_type TYPE text`,
		`ALTER TABLE events ADD CONSTRAINT events_object_type_fkey FOREIGN KEY (object_type) REFERENCES event_types (name)`,
		`DROP TYPE event_type`,
	)
	migrations.Add(5,
		`ALTER TABLE deployments ADD COLUMN deploy_timeout integer NOT NULL DEFAULT 30`,
	)
	migrations.Add(6,
		`INSERT INTO deployment_strategies (name) VALUES ('discoverd-meta')`,
	)
	migrations.Add(7,
		`ALTER TABLE job_cache ADD COLUMN exit_status integer`,
		`ALTER TABLE job_cache ADD COLUMN host_error text`,
	)
	migrations.Add(8,
		`CREATE TABLE job_states (name text PRIMARY KEY)`,
		`INSERT INTO job_states (name) VALUES ('starting'), ('up'), ('down'), ('crashed'), ('failed')`,
		`ALTER TABLE job_cache ALTER COLUMN state TYPE text`,
		`ALTER TABLE job_cache ADD CONSTRAINT job_state_fkey FOREIGN KEY (state) REFERENCES job_states (name)`,
		`DROP TRIGGER job_state_trigger ON job_cache`,
		`DROP TYPE job_state`,
	)
	migrations.Add(9,
		`INSERT INTO job_states (name) VALUES ('pending')`,
		`ALTER TABLE job_cache ADD COLUMN run_at timestamptz`,
		`ALTER TABLE job_cache ADD COLUMN restarts integer`,
		`ALTER TABLE job_cache RENAME COLUMN job_id TO cluster_id`,
		`ALTER TABLE job_cache ALTER COLUMN cluster_id TYPE text`,
		`ALTER TABLE job_cache DROP CONSTRAINT job_cache_pkey`,
		`ALTER TABLE job_cache ADD COLUMN job_id uuid PRIMARY KEY DEFAULT uuid_generate_v4()`,
		`ALTER TABLE job_cache ADD COLUMN host_id text`,
		`UPDATE job_cache SET host_id = s.split[1], job_id = s.split[2]::uuid FROM (SELECT cluster_id, regexp_matches(cluster_id, '([^-]+)-(.*)') AS split FROM job_cache) AS s WHERE job_cache.cluster_id = s.cluster_id`,
	)
	migrations.Add(10,
		`ALTER TABLE formations ADD COLUMN tags jsonb`,
	)
	migrations.Add(11,
		`INSERT INTO deployment_strategies VALUES ('sirenia')`,
		`UPDATE apps SET strategy = 'sirenia' WHERE name = 'postgres'`,
		`DELETE FROM deployment_strategies WHERE name = 'postgres'`,
	)
}

func migrateDB(db *postgres.DB) error {
	return migrations.Migrate(db)
}
