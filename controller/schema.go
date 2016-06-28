package main

import (
	"fmt"
	"strings"

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
}

func migrateDB(db *postgres.DB) error {
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
	for rows.Next() {
		var release Release
		if err := rows.Scan(&release.ID, &release.Meta, &release.Processes, &release.AppName, &release.AppMeta); err != nil {
			rows.Close()
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
