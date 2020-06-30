package data

import (
	"fmt"
	"strings"

	"github.com/flynn/flynn/pkg/postgres"
)

// RouterMigrationStart is the ID of the migration that starts the router
// migrations, and is used when restoring cluster backups to determine
// whether or not the restore should import the legacy router database
// into the controller (and thus perform the migrations that would otherwise
// be performed by migration 36 onwards)
const RouterMigrationStart = 36

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
	migrations.Add(12,
		`INSERT INTO event_types (name) VALUES ('resource_app_deletion')`,
	)
	migrations.Add(13,
		// define a function to merge two JSON objects, taken from:
		// https://gist.github.com/inindev/2219dff96851928c2282
		`CREATE OR REPLACE FUNCTION public.jsonb_merge(data jsonb, merge_data jsonb)
		RETURNS jsonb
		IMMUTABLE
		LANGUAGE sql
		AS $$
		    SELECT json_object_agg(key, value)::jsonb
		    FROM (
			WITH to_merge AS (
			    SELECT * FROM jsonb_each(merge_data)
			)
			SELECT *
			FROM jsonb_each(data)
			WHERE key NOT IN (SELECT key FROM to_merge)
			UNION ALL
			SELECT * FROM to_merge
		    ) t;
		$$`,
		`UPDATE apps SET meta = jsonb_merge(meta, '{"flynn-system-critical":"true"}') WHERE name IN ('discoverd', 'flannel', 'postgres', 'controller')`,
	)
	migrations.Add(14,
		`CREATE TABLE backup_statuses (name text PRIMARY KEY)`,
		`INSERT INTO backup_statuses (name) VALUES
			('running'), ('complete'), ('error')`,
		`CREATE TABLE backups (
			backup_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
			status text NOT NULL REFERENCES backup_statuses (name),
			sha512 text,
			size bigint,
			error text,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			completed_at timestamptz,
			deleted_at timestamptz
		)`,
		`INSERT INTO event_types (name) VALUES ('cluster_backup')`,
	)
	migrations.Add(15,
		`ALTER TABLE artifacts ADD COLUMN meta jsonb`,
		`CREATE TABLE release_artifacts (
			release_id uuid NOT NULL REFERENCES releases (release_id),
			artifact_id uuid NOT NULL REFERENCES artifacts (artifact_id),
			created_at timestamptz NOT NULL DEFAULT now(),
			deleted_at timestamptz,
			PRIMARY KEY (release_id, artifact_id))`,

		// add a check to ensure releases only have a single "docker"
		// artifact, and that artifact is added first
		`CREATE FUNCTION check_release_artifacts() RETURNS OPAQUE AS $$
			BEGIN
			    IF (
			      SELECT COUNT(*)
			      FROM release_artifacts r
			      INNER JOIN artifacts a ON r.artifact_id = a.artifact_id
			      WHERE r.release_id = NEW.release_id AND a.type = 'docker'
			    ) != 1 THEN
			      RAISE EXCEPTION 'must have exactly one artifact of type "docker"' USING ERRCODE = 'check_violation';
			    END IF;

			    RETURN NULL;
			END;
			$$ LANGUAGE plpgsql`,
		`CREATE TRIGGER release_artifacts_trigger
			AFTER INSERT ON release_artifacts
			FOR EACH ROW EXECUTE PROCEDURE check_release_artifacts()`,
		`INSERT INTO release_artifacts (release_id, artifact_id) (SELECT release_id, artifact_id FROM releases WHERE artifact_id IS NOT NULL)`,

		// set "git=true" for releases with SLUG_URL set
		`UPDATE releases SET meta = jsonb_merge(CASE WHEN meta = 'null' THEN '{}' ELSE meta END, '{"git":"true"}') WHERE env ? 'SLUG_URL'`,

		// create file artifacts for any releases with SLUG_URL set,
		// taking care not to create duplicate artifacts
		`DO $$
		DECLARE
			release RECORD;
			artifact uuid;
		BEGIN
			FOR release IN SELECT * FROM releases WHERE env ? 'SLUG_URL' LOOP
				SELECT INTO artifact artifact_id FROM artifacts WHERE type = 'file' AND uri = release.env->>'SLUG_URL';

				IF NOT FOUND THEN
					INSERT INTO artifacts (type, uri, meta)
					VALUES ('file', release.env->>'SLUG_URL', '{"blobstore":"true"}')
					RETURNING artifact_id INTO artifact;
				END IF;

				INSERT INTO release_artifacts (release_id, artifact_id) VALUES(release.release_id, artifact);
			END LOOP;
		END $$`,
		`ALTER TABLE releases DROP COLUMN artifact_id`,
	)
	migrations.Add(16,
		// set "blobstore=true" for artifacts stored in the blobstore
		`UPDATE artifacts SET meta = jsonb_merge(CASE WHEN meta = 'null' THEN '{}' ELSE meta END, '{"blobstore":"true"}') WHERE uri LIKE 'http://blobstore.discoverd/%'`,

		`INSERT INTO event_types (name) VALUES ('release_deletion')`,

		// add a trigger to prevent current app releases from being deleted
		`CREATE FUNCTION check_release_delete() RETURNS OPAQUE AS $$
			BEGIN
				IF NEW.deleted_at IS NOT NULL AND (SELECT COUNT(*) FROM apps WHERE release_id = NEW.release_id) != 0 THEN
					RAISE EXCEPTION 'cannot delete current app release' USING ERRCODE = 'check_violation';
				END IF;

				RETURN NULL;
			END;
		$$ LANGUAGE plpgsql`,
		`CREATE TRIGGER check_release_delete AFTER UPDATE ON releases FOR EACH ROW EXECUTE PROCEDURE check_release_delete()`,
	)
	migrations.Add(17,
		// add an "index" column to release_artifacts which is set
		// explicitly to the array index of release.ArtifactIDs
		`ALTER TABLE release_artifacts ADD COLUMN index integer`,
		`CREATE UNIQUE INDEX ON release_artifacts (release_id, index) WHERE deleted_at IS NULL`,
		`DO $$
		DECLARE
			row RECORD;
			release uuid;
			i int;
		BEGIN
			FOR release IN SELECT DISTINCT(release_id) FROM release_artifacts LOOP
				i := 0;
				FOR row IN SELECT * FROM release_artifacts WHERE release_id = release ORDER BY created_at ASC LOOP
					UPDATE release_artifacts SET index = i WHERE release_id = release AND artifact_id = row.artifact_id;
					i := i + 1;
				END LOOP;
			END LOOP;
		END $$`,
		`ALTER TABLE release_artifacts ALTER COLUMN index SET NOT NULL`,
	)
	migrations.Add(18,
		`INSERT INTO event_types (name) VALUES ('app_garbage_collection')`,
	)
	migrations.AddSteps(19,
		migrateProcessArgs,
	)
	migrations.Add(20,
		// update Redis app service name to match the app name
		`UPDATE releases
		SET processes = r.processes
		FROM (
			SELECT r.release_id AS id, jsonb_set(r.processes, '{redis,service}', ('"' || a.name || '"')::jsonb, true) AS processes
			FROM releases r
			INNER JOIN apps a USING (release_id)
			WHERE a.meta->>'flynn-system-app' = 'true' AND a.name LIKE 'redis-%'
		) r
		WHERE release_id = r.id`,
	)
	migrations.Add(21,
		`INSERT INTO job_states (name) VALUES ('stopping')`,
	)
	migrations.Add(22,
		`DROP TRIGGER notify_formation ON formations`,
		`DROP FUNCTION notify_formation()`,
	)
	migrations.Add(23,
		`ALTER TABLE job_cache ADD COLUMN args jsonb`,
	)
	migrations.Add(24,
		`UPDATE apps SET meta = jsonb_merge(CASE WHEN meta = 'null' THEN '{}' ELSE meta END, '{"gc.max_inactive_slug_releases":"10"}') WHERE meta->>'gc.max_inactive_slug_releases' IS NULL`,
	)
	migrations.AddSteps(25,
		migrateProcessData,
	)
	migrations.Add(26,
		`DROP TRIGGER release_artifacts_trigger ON release_artifacts`,
		`DROP FUNCTION check_release_artifacts()`,
		`ALTER TABLE artifacts ADD COLUMN manifest jsonb`,
		`ALTER TABLE artifacts ADD COLUMN hashes jsonb`,
		`ALTER TABLE artifacts ADD COLUMN size integer`,
		`ALTER TABLE artifacts ADD COLUMN layer_url_template text`,
		`CREATE FUNCTION check_artifact_manifest() RETURNS OPAQUE AS $$
			BEGIN
				IF NEW.type = 'flynn' AND NEW.manifest IS NULL THEN
					RAISE EXCEPTION 'flynn artifacts must have a manifest' USING ERRCODE = 'check_violation';
				END IF;

				RETURN NULL;
			END;
		$$ LANGUAGE plpgsql`,
		`CREATE TRIGGER check_artifact_manifest AFTER INSERT ON artifacts FOR EACH ROW EXECUTE PROCEDURE check_artifact_manifest()`,
	)
	migrations.Add(27,
		// Add an "app_id" column to releases to associate them
		// with apps directly (rather than indirectly through
		// formations).
		`ALTER TABLE releases ADD COLUMN app_id uuid REFERENCES apps (app_id)`,
		`DO $$
		DECLARE
			release uuid;
		BEGIN
			FOR release IN SELECT release_id FROM releases LOOP
				UPDATE releases
				SET app_id = app.app_id
				FROM (
					SELECT app_id
					FROM apps
					WHERE release_id = release
				) AS app
				WHERE release_id = release;

				UPDATE releases
				SET app_id = formation.app_id
				FROM (
					SELECT app_id
					FROM formations
					WHERE release_id = release
					ORDER BY created_at DESC
					LIMIT 1
				) AS formation
				WHERE release_id = release;
			END LOOP;
		END $$`,
		`UPDATE releases SET deleted_at = now() WHERE app_id IS NULL`,
		`CREATE INDEX ON releases (app_id) WHERE deleted_at IS NULL`,
		`ALTER TABLE releases ADD CHECK (app_id IS NOT NULL OR deleted_at IS NOT NULL)`,
	)
	migrations.Add(28,
		`CREATE TABLE sink_kinds (name text PRIMARY KEY)`,
		`INSERT INTO sink_kinds (name) VALUES ('syslog')`,
		`INSERT INTO event_types (name) VALUES ('sink'), ('sink_deletion')`,
		`CREATE TABLE sinks (
			sink_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
			kind text NOT NULL REFERENCES sink_kinds,
			config jsonb NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			deleted_at timestamptz
		)`,
	)
	migrations.Add(29,
		`ALTER TABLE deployments ADD COLUMN tags jsonb`,
	)
	migrations.Add(30,
		`INSERT INTO event_types (name) VALUES ('scale_request')`,
		`CREATE TABLE scale_request_states (name text PRIMARY KEY)`,
		`INSERT INTO scale_request_states (name) VALUES ('pending'), ('cancelled'), ('complete')`,
		`CREATE TABLE scale_requests (
			scale_request_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
			app_id           uuid NOT NULL REFERENCES apps (app_id),
			release_id       uuid NOT NULL REFERENCES releases (release_id),
			state            text NOT NULL REFERENCES scale_request_states (name),
			old_processes    jsonb,
			new_processes    jsonb,
			old_tags         jsonb,
			new_tags         jsonb,
			created_at       timestamptz NOT NULL DEFAULT now(),
			updated_at       timestamptz NOT NULL DEFAULT now()
		)`,
	)
	migrations.Add(31,
		`CREATE TABLE volume_types (name text PRIMARY KEY)`,
		`INSERT INTO volume_types (name) VALUES ('data'), ('squashfs'), ('ext2')`,
		`CREATE TABLE volume_states (name text PRIMARY KEY)`,
		`INSERT INTO volume_states (name) VALUES ('pending'), ('created'), ('destroyed')`,
		`INSERT INTO event_types (name) VALUES ('volume')`,
		`CREATE TABLE volumes (
			volume_id         uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
			host_id           text NOT NULL,
			type              text NOT NULL REFERENCES volume_types,
			state             text NOT NULL REFERENCES volume_states,
			app_id            uuid,
			release_id        uuid,
			job_id            uuid,
			job_type          text,
			path              text,
			delete_on_stop    boolean NOT NULL DEFAULT FALSE,
			meta              jsonb,
			created_at        timestamptz NOT NULL DEFAULT now(),
			updated_at        timestamptz NOT NULL DEFAULT now(),
			decommissioned_at timestamptz
		)`,
		`CREATE TABLE job_volumes (
			job_id     uuid        NOT NULL REFERENCES job_cache (job_id),
			volume_id  uuid        NOT NULL REFERENCES volumes (volume_id),
			index      integer     NOT NULL DEFAULT 0,
			PRIMARY KEY (job_id, volume_id)
		)`,
		`INSERT INTO job_states (name) VALUES ('blocked')`,
	)
	migrations.Add(32,
		`INSERT INTO deployment_strategies (name) VALUES ('one-down-one-up')`,
	)
	migrations.Add(33,
		`ALTER TABLE events ADD COLUMN op text`,
	)
	migrations.Add(34,
		`INSERT INTO event_types (name) VALUES ('scale_request_cancelation')`,
	)
	migrations.Add(35,
		`INSERT INTO deployment_strategies (name) VALUES ('in-batches')`,
		`ALTER TABLE deployments ADD COLUMN deploy_batch_size integer`,
	)
	// migrations 36 through 44 were imported from the router package when
	// merging the router database into the controller, and were kept
	// intact rather than just importing the schema in one migration so
	// that we can continue to restore backups from older clusters which
	// may have only run a subset of these migrations
	migrations.Add(36,
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
	migrations.Add(37,
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
	migrations.Add(38,
		// Ensure the default is set on the path column. We set this above, but
		// releases v20151214.1, v20151214.0, v20151213.1, and v20151213.0
		// didn't have the default specified, so this will fix any databases
		// from those versions that have the broken release and have already run
		// migration 2.
		`ALTER TABLE http_routes ALTER COLUMN path SET DEFAULT '/'`,
	)
	migrations.Add(39,
		`ALTER TABLE tcp_routes ADD COLUMN leader boolean NOT NULL DEFAULT FALSE`,
		`ALTER TABLE http_routes ADD COLUMN leader boolean NOT NULL DEFAULT FALSE`,
	)
	migrations.Add(40,
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
	migrations.Add(41,
		`ALTER TABLE tcp_routes ADD COLUMN drain_backends boolean NOT NULL DEFAULT TRUE`,
		`ALTER TABLE http_routes ADD COLUMN drain_backends boolean NOT NULL DEFAULT TRUE`,
		`UPDATE http_routes SET drain_backends = false WHERE service = 'controller'`,
	)
	migrations.Add(42,
		`ALTER TABLE http_routes ADD COLUMN port integer NOT NULL DEFAULT 0 CHECK (port > -1 AND port < 65535)`,
	)
	migrations.Add(43,
		`DROP INDEX http_routes_domain_path_key`,
		`CREATE UNIQUE INDEX http_routes_domain_port_path_key ON http_routes
		USING btree (domain, port, path) WHERE deleted_at IS NULL`,
	)
	migrations.Add(44,
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
	migrations.Add(45,
		`
CREATE FUNCTION set_tcp_route_port() RETURNS TRIGGER AS $$
  BEGIN
    IF NEW.port = 0 THEN
      SELECT INTO NEW.port * FROM generate_series(3000, 3500) AS port WHERE port NOT IN (SELECT port FROM tcp_routes) LIMIT 1;
    END IF;

    RETURN NEW;
  END;
$$ LANGUAGE plpgsql`,
		`
CREATE TRIGGER set_tcp_route_port
  BEFORE INSERT ON tcp_routes
  FOR EACH ROW EXECUTE PROCEDURE set_tcp_route_port()`,
	)
	migrations.Add(46,
		`
		CREATE OR REPLACE FUNCTION jsonb_array_to_text_array(jsonb)
			RETURNS text[] LANGUAGE sql IMMUTABLE AS
		'SELECT ARRAY(SELECT jsonb_array_elements_text($1))';

		CREATE FUNCTION match_label_expression(jsonb, jsonb) RETURNS boolean AS $$
		BEGIN
			CASE ($1->>'op')::integer
				WHEN 0 /* OP_IN */
					THEN IF ARRAY[$2->>($1->>'key'::text)] <@ jsonb_array_to_text_array($1->'values')
						THEN
							RETURN TRUE;
						ELSE
							RETURN FALSE;
					END IF;
				WHEN 1 /* OP_NOT_IN */
					THEN IF ARRAY[$2->>($1->>'key'::text)] <@ jsonb_array_to_text_array($1->'values')
						THEN
							RETURN FALSE;
						ELSE
							RETURN TRUE;
					END IF;
				WHEN 2 /* OP_EXISTS */
					THEN IF $2->($1->>'key'::text) IS NOT NULL
						THEN
							RETURN TRUE;
						ELSE
							RETURN FALSE;
					END IF;
				WHEN 3 /* OP_NOT_EXISTS */
					THEN IF $2->($1->>'key'::text) IS NULL
						THEN
							RETURN TRUE;
						ELSE
							RETURN FALSE;
					END IF;
				ELSE
					RAISE EXCEPTION 'Invalid label expression --> %', $1->>'op';
				END CASE;
		END;
		$$ LANGUAGE plpgsql;

		CREATE FUNCTION match_label_filter(jsonb, jsonb) RETURNS boolean AS $$
		DECLARE
			exp jsonb;
		BEGIN
			FOR exp IN SELECT * FROM jsonb_array_elements($1)
			LOOP
				IF NOT match_label_expression(exp, $2) THEN
					RETURN FALSE;
				END IF;
			END LOOP;
			RETURN TRUE;
		END;
		$$ LANGUAGE plpgsql;

		CREATE FUNCTION match_label_filters(jsonb, jsonb) RETURNS boolean AS $$
		DECLARE
			f jsonb;
		BEGIN
			IF (SELECT COUNT(*) FROM jsonb_array_elements($1)) = 0
			THEN
				RETURN TRUE;
			END IF;

			FOR f IN SELECT * FROM jsonb_array_elements($1)
			LOOP
				IF match_label_filter(f, $2) THEN
					RETURN TRUE;
				END IF;
			END LOOP;
			RETURN FALSE;
		END;
		$$ LANGUAGE plpgsql;
		`,
	)
	migrations.Add(47,
		// Add a "type" column to deployments to distinguish between code and
		// config releases (code releases are when the artifacts have changed, all
		// the rest are config releases)
		`CREATE TABLE release_types (name text PRIMARY KEY)`,
		`INSERT INTO release_types (name) VALUES ('code'), ('config')`,
		`ALTER TABLE deployments ADD COLUMN type text REFERENCES release_types (name)`,
		`
		UPDATE deployments AS target
				SET type = CASE WHEN d.old_artifact_ids = d.new_artifact_ids THEN 'config' ELSE 'code' END
		FROM (SELECT
				d.deployment_id,
				ARRAY(
					SELECT a.artifact_id
					FROM release_artifacts a
					WHERE a.release_id = old_r.release_id AND a.deleted_at IS NULL
					ORDER BY a.index
				) AS old_artifact_ids, old_r.env, old_r.processes, old_r.meta, old_r.created_at,
				ARRAY(
					SELECT a.artifact_id
					FROM release_artifacts a
					WHERE a.release_id = new_r.release_id AND a.deleted_at IS NULL
					ORDER BY a.index
				) AS new_artifact_ids
			FROM deployments d
			LEFT OUTER JOIN releases old_r
				ON d.old_release_id = old_r.release_id
			LEFT OUTER JOIN releases new_r
				ON d.new_release_id = new_r.release_id
			) AS d
			WHERE target.deployment_id = d.deployment_id;
		`,
		`ALTER TABLE deployments ALTER COLUMN type SET NOT NULL`,
	)
	migrations.Add(48, `
CREATE FUNCTION deployment_status(d_id uuid) RETURNS text AS $$
  SELECT data->>'status' FROM events WHERE object_type = 'deployment' AND object_id::uuid = d_id ORDER BY created_at DESC LIMIT 1;
$$ LANGUAGE SQL;
	`)
	migrations.Add(49, `
ALTER TABLE http_routes ADD COLUMN disable_keep_alives boolean NOT NULL DEFAULT false;
	`)
	migrations.Add(50,
		`ALTER TABLE events ADD COLUMN deployment_id uuid REFERENCES deployments (deployment_id)`,
		`ALTER TABLE scale_requests ADD COLUMN deployment_id uuid REFERENCES deployments (deployment_id)`,
		`ALTER TABLE job_cache ADD COLUMN deployment_id uuid REFERENCES deployments (deployment_id)`,
		// associate all existing job events and jobs with a deployment
		`DO $$
		DECLARE
			job RECORD;
			deployment uuid;
		BEGIN
			FOR job IN SELECT * FROM job_cache ORDER BY created_at DESC LOOP
				SELECT INTO deployment deployment_id FROM deployments
				WHERE app_id = job.app_id
					AND (old_release_id = job.release_id OR new_release_id = job.release_id)
					AND created_at <= job.created_at AND finished_at > job.created_at
				ORDER BY created_at DESC
				LIMIT 1;

				IF deployment IS NOT NULL THEN
					UPDATE job_cache SET deployment_id = deployment WHERE job_id = job.job_id;
					UPDATE events SET deployment_id = deployment WHERE object_type = 'job' AND object_id = job.job_id::text;
				END IF;
			END LOOP;
		END $$`,
		// associate all existing scale events and scale_requests with a deployment
		`DO $$
		DECLARE
			sr RECORD;
			deployment uuid;
		BEGIN
			FOR sr IN SELECT * FROM scale_requests ORDER BY created_at DESC LOOP
				SELECT INTO deployment deployment_id FROM deployments
				WHERE app_id = sr.app_id
					AND (old_release_id = sr.release_id OR new_release_id = sr.release_id)
					AND created_at <= sr.created_at AND finished_at > sr.created_at
				ORDER BY created_at DESC
				LIMIT 1;

				IF deployment IS NOT NULL THEN
					UPDATE scale_requests SET deployment_id = deployment WHERE scale_request_id = sr.scale_request_id;
					UPDATE events SET deployment_id = deployment WHERE object_type = 'scale_request' AND object_id = sr.scale_request_id::text;
				END IF;
			END LOOP;
		END $$`,
	)
}

func MigrateDB(db *postgres.DB) error {
	return migrations.Migrate(db)
}

// migrateProcessArgs sets ProcessType.Args from Entrypoint / Cmd for every
// release, and also prepends an explicit entrypoint for system and slug apps
// (they will no longer use the Dockerfile Entrypoint as they have some args
// like `scheduler` for the controller scheduler and `start web` for slugs).
func migrateProcessArgs(tx *postgres.DBTx) error {
	type Release struct {
		ID      string
		AppName *string
		AppMeta map[string]string
		Meta    map[string]string

		// use map[string]interface{} for process types so we can
		// just update Args and leave other fields untouched
		Processes map[string]map[string]interface{}
	}

	// get all the releases with the associated app name if set
	var releases []Release
	rows, err := tx.Query("SELECT r.release_id, r.meta, r.processes, a.name, a.meta FROM releases r LEFT JOIN apps a ON a.release_id = r.release_id")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var release Release
		if err := rows.Scan(&release.ID, &release.Meta, &release.Processes, &release.AppName, &release.AppMeta); err != nil {
			return err
		}
		releases = append(releases, release)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, release := range releases {
		for typ, proc := range release.Processes {
			// if the release is for a system app which has a Cmd,
			// explicitly set the Entrypoint
			var cmd []interface{}
			if v, ok := proc["cmd"]; ok {
				cmd = v.([]interface{})
			}
			if release.AppName != nil && release.AppMeta["flynn-system-app"] == "true" && len(cmd) > 0 {
				switch *release.AppName {
				case "postgres":
					proc["entrypoint"] = []interface{}{"/bin/start-flynn-postgres"}
				case "controller":
					proc["entrypoint"] = []interface{}{"/bin/start-flynn-controller"}
				case "redis":
					proc["entrypoint"] = []interface{}{"/bin/start-flynn-redis"}
				case "mariadb":
					proc["entrypoint"] = []interface{}{"/bin/start-flynn-mariadb"}
				case "mongodb":
					proc["entrypoint"] = []interface{}{"/bin/start-flynn-mongodb"}
				case "router":
					proc["entrypoint"] = []interface{}{"/bin/flynn-router"}
				case "logaggregator":
					proc["entrypoint"] = []interface{}{"/bin/logaggregator"}
				default:
					if strings.HasPrefix(*release.AppName, "redis-") {
						proc["entrypoint"] = []interface{}{"/bin/start-flynn-redis"}
					} else {
						panic(fmt.Sprintf("migration failed to set entrypoint for system app %s", *release.AppName))
					}
				}
			}

			// git releases use the slugrunner which need an Entrypoint
			if release.Meta["git"] == "true" {
				proc["entrypoint"] = []interface{}{"/runner/init"}
			}

			// construct Args by appending Cmd to Entrypoint
			var args []interface{}
			if v, ok := proc["entrypoint"]; ok {
				args = v.([]interface{})
			}
			proc["args"] = append(args, cmd...)
			release.Processes[typ] = proc
		}

		// save the processes back to the db
		if err := tx.Exec("UPDATE releases SET processes = $1 WHERE release_id = $2", release.Processes, release.ID); err != nil {
			return err
		}
	}
	return nil
}

// migrateProcessData populates ProcessType.Volumes if ProcessType.Data is set
func migrateProcessData(tx *postgres.DBTx) error {
	type Release struct {
		ID string

		// use map[string]interface{} for process types so we can just
		// update Volumes and Data and leave other fields untouched
		Processes map[string]map[string]interface{}
	}

	var releases []Release
	rows, err := tx.Query("SELECT release_id, processes FROM releases")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var release Release
		if err := rows.Scan(&release.ID, &release.Processes); err != nil {
			return err
		}
		releases = append(releases, release)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, release := range releases {
		for typ, proc := range release.Processes {
			v, ok := proc["data"]
			if !ok {
				continue
			}
			data, ok := v.(bool)
			if !ok || !data {
				continue
			}
			proc["volumes"] = []struct {
				Path string `json:"path"`
			}{
				{Path: "/data"},
			}
			delete(proc, "data")
			release.Processes[typ] = proc
		}

		// save the processes back to the db
		if err := tx.Exec("UPDATE releases SET processes = $1 WHERE release_id = $2", release.Processes, release.ID); err != nil {
			return err
		}
	}
	return nil
}
