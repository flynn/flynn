package main

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/queue"
)

func migrateDB(db *sql.DB) error {
	m := postgres.NewMigrations()
	queueSQL, err := queue.SetupSQL("jobs")
	if err != nil {
		return err
	}
	m.Add(1, queueSQL...)
	m.Add(2,
		`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,

		`CREATE TYPE deployment_strategy AS ENUM ('all-at-once', 'one-by-one')`,
		`CREATE TABLE deployments (
    deployment_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
    app_id uuid NOT NULL,
    old_release_id uuid NOT NULL,
    new_release_id uuid NOT NULL,
    strategy deployment_strategy NOT NULL,
    steps text NOT NULL,
    status int8 NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now())`,

		`CREATE SEQUENCE deployment_event_ids`,
		`CREATE TABLE deployment_events (
    event_id bigint PRIMARY KEY DEFAULT nextval('deployment_event_ids'),
    deployment_id uuid NOT NULL REFERENCES deployments (deployment_id),
    release_id text NOT NULL,
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
	return m.Migrate(db)
}
