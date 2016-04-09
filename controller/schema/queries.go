package schema

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/que-go"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
)

var preparedStatements = map[string]string{
	"ping":                                  pingQuery,
	"app_list":                              appListQuery,
	"app_select_by_name":                    appSelectByNameQuery,
	"app_select_by_name_for_update":         appSelectByNameForUpdateQuery,
	"app_select_by_name_or_id":              appSelectByNameOrIDQuery,
	"app_select_by_name_or_id_for_update":   appSelectByNameOrIDForUpdateQuery,
	"app_insert":                            appInsertQuery,
	"app_update_strategy":                   appUpdateStrategyQuery,
	"app_update_meta":                       appUpdateMetaQuery,
	"app_update_release":                    appUpdateReleaseQuery,
	"app_delete":                            appDeleteQuery,
	"app_next_name_id":                      appNextNameIDQuery,
	"app_get_release":                       appGetReleaseQuery,
	"release_list":                          releaseListQuery,
	"release_select":                        releaseSelectQuery,
	"release_insert":                        releaseInsertQuery,
	"release_app_list":                      releaseAppListQuery,
	"release_artifacts_insert":              releaseArtifactsInsertQuery,
	"artifact_list":                         artifactListQuery,
	"artifact_list_ids":                     artifactListIDsQuery,
	"artifact_select":                       artifactSelectQuery,
	"artifact_select_by_type_and_uri":       artifactSelectByTypeAndURIQuery,
	"artifact_insert":                       artifactInsertQuery,
	"deployment_list":                       deploymentListQuery,
	"deployment_select":                     deploymentSelectQuery,
	"deployment_insert":                     deploymentInsertQuery,
	"deployment_update_finished_at":         deploymentUpdateFinishedAtQuery,
	"deployment_update_finished_at_now":     deploymentUpdateFinishedAtNowQuery,
	"deployment_delete":                     deploymentDeleteQuery,
	"event_select":                          eventSelectQuery,
	"event_insert":                          eventInsertQuery,
	"event_insert_unique":                   eventInsertUniqueQuery,
	"formation_list_by_app":                 formationListByAppQuery,
	"formation_list_active":                 formationListActiveQuery,
	"formation_list_since":                  formationListSinceQuery,
	"formation_select":                      formationSelectQuery,
	"formation_select_expanded":             formationSelectExpandedQuery,
	"formation_insert":                      formationInsertQuery,
	"formation_update":                      formationUpdateQuery,
	"formation_delete":                      formationDeleteQuery,
	"formation_delete_by_app":               formationDeleteByAppQuery,
	"job_list":                              jobListQuery,
	"job_list_active":                       jobListActiveQuery,
	"job_select":                            jobSelectQuery,
	"job_insert":                            jobInsertQuery,
	"job_update":                            jobUpdateQuery,
	"provider_list":                         providerListQuery,
	"provider_select_by_name":               providerSelectByNameQuery,
	"provider_select_by_name_or_id":         providerSelectByNameOrIDQuery,
	"provider_insert":                       providerInsertQuery,
	"resource_list":                         resourceListQuery,
	"resource_list_by_provider":             resourceListByProviderQuery,
	"resource_list_by_app":                  resourceListByAppQuery,
	"resource_select":                       resourceSelectQuery,
	"resource_insert":                       resourceInsertQuery,
	"resource_delete":                       resourceDeleteQuery,
	"app_resource_insert_app_by_name":       appResourceInsertAppByNameQuery,
	"app_resource_insert_app_by_name_or_id": appResourceInsertAppByNameOrIDQuery,
	"app_resource_delete_by_app":            appResourceDeleteByAppQuery,
	"app_resource_delete_by_resource":       appResourceDeleteByResourceQuery,
	"domain_migration_insert":               domainMigrationInsert,
}

func PrepareStatements(conn *pgx.Conn) error {
	for name, sql := range preparedStatements {
		if _, err := conn.Prepare(name, sql); err != nil {
			return err
		}
	}
	if err := que.PrepareStatements(conn); err != nil {
		return err
	}
	return nil
}

const (
	// misc
	pingQuery = `SELECT 1`
	// apps
	appListQuery = `
SELECT app_id, name, meta, strategy, release_id, deploy_timeout, created_at, updated_at
FROM apps WHERE deleted_at IS NULL ORDER BY created_at DESC`
	appSelectByNameQuery = `
SELECT app_id, name, meta, strategy, release_id, deploy_timeout, created_at, updated_at
FROM apps WHERE deleted_at IS NULL AND name = $1`
	appSelectByNameForUpdateQuery = `
SELECT app_id, name, meta, strategy, release_id, deploy_timeout, created_at, updated_at
FROM apps WHERE deleted_at IS NULL AND name = $1 FOR UPDATE`
	appSelectByNameOrIDQuery = `
SELECT app_id, name, meta, strategy, release_id, deploy_timeout, created_at, updated_at
FROM apps WHERE deleted_at IS NULL AND (app_id = $1 OR name = $2) LIMIT 1`
	appSelectByNameOrIDForUpdateQuery = `
SELECT app_id, name, meta, strategy, release_id, deploy_timeout, created_at, updated_at
FROM apps WHERE deleted_at IS NULL AND (app_id = $1 OR name = $2) LIMIT 1 FOR UPDATE`
	appInsertQuery = `
INSERT INTO apps (app_id, name, meta, strategy, deploy_timeout) VALUES ($1, $2, $3, $4, $5) RETURNING created_at, updated_at`
	appUpdateStrategyQuery = `
UPDATE apps SET strategy = $2, updated_at = now() WHERE app_id = $1`
	appUpdateMetaQuery = `
UPDATE apps SET meta = $2, updated_at = now() WHERE app_id = $1`
	appUpdateReleaseQuery = `
UPDATE apps SET release_id = $2, updated_at = now() WHERE app_id = $1`
	appDeleteQuery = `
UPDATE apps SET deleted_at = now() WHERE app_id = $1 AND deleted_at IS NULL`
	appNextNameIDQuery = `
SELECT nextval('name_ids')`
	appGetReleaseQuery = `
SELECT r.release_id,
  ARRAY(
	SELECT a.artifact_id
	FROM release_artifacts a
	WHERE a.release_id = r.release_id AND a.deleted_at IS NULL
	ORDER BY a.created_at
  ), r.env, r.processes, r.meta, r.created_at
FROM apps a JOIN releases r USING (release_id) WHERE a.app_id = $1`

	releaseListQuery = `
SELECT r.release_id,
  ARRAY(
	SELECT a.artifact_id
	FROM release_artifacts a
	WHERE a.release_id = r.release_id AND a.deleted_at IS NULL
	ORDER BY a.created_at
  ), r.env, r.processes, r.meta, r.created_at
FROM releases r WHERE r.deleted_at IS NULL ORDER BY r.created_at DESC`
	releaseSelectQuery = `
SELECT r.release_id,
  ARRAY(
	SELECT a.artifact_id
	FROM release_artifacts a
	WHERE a.release_id = r.release_id AND a.deleted_at IS NULL
	ORDER BY a.created_at
  ), r.env, r.processes, r.meta, r.created_at
FROM releases r WHERE r.release_id = $1 AND r.deleted_at IS NULL`
	releaseInsertQuery = `
INSERT INTO releases (release_id, env, processes, meta)
VALUES ($1, $2, $3, $4) RETURNING created_at`
	releaseAppListQuery = `
SELECT DISTINCT(r.release_id),
  ARRAY(
	SELECT a.artifact_id
	FROM release_artifacts a
	WHERE a.release_id = r.release_id AND a.deleted_at IS NULL
	ORDER BY a.created_at
  ), r.env, r.processes, r.meta, r.created_at
FROM releases r JOIN formations f USING (release_id)
WHERE f.app_id = $1 AND r.deleted_at IS NULL ORDER BY r.created_at DESC`
	releaseArtifactsInsertQuery = `
INSERT INTO release_artifacts (release_id, artifact_id) VALUES ($1, $2)`
	artifactListQuery = `
SELECT artifact_id, type, uri, meta, created_at FROM artifacts
WHERE deleted_at IS NULL ORDER BY created_at DESC`
	artifactListIDsQuery = `
SELECT artifact_id, type, uri, meta, created_at FROM artifacts
WHERE deleted_at IS NULL AND artifact_id = ANY($1)`
	artifactSelectQuery = `
SELECT artifact_id, type, uri, meta, created_at FROM artifacts
WHERE artifact_id = $1 AND deleted_at IS NULL`
	artifactSelectByTypeAndURIQuery = `
SELECT artifact_id, meta, created_at FROM artifacts WHERE type = $1 AND uri = $2`
	artifactInsertQuery = `
INSERT INTO artifacts (artifact_id, type, uri, meta) VALUES ($1, $2, $3, $4) RETURNING created_at`
	deploymentInsertQuery = `
INSERT INTO deployments (deployment_id, app_id, old_release_id, new_release_id, strategy, processes, deploy_timeout)
VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING created_at`
	deploymentUpdateFinishedAtQuery = `
UPDATE deployments SET finished_at = $2 WHERE deployment_id = $1`
	deploymentUpdateFinishedAtNowQuery = `
UPDATE deployments SET finished_at = now() WHERE deployment_id = $1`
	deploymentDeleteQuery = `
DELETE FROM deployments WHERE deployment_id = $1`
	deploymentSelectQuery = `
WITH deployment_events AS (SELECT * FROM events WHERE object_type = 'deployment')
SELECT d.deployment_id, d.app_id, d.old_release_id, d.new_release_id,
  strategy, e1.data->>'status' AS status,
  processes, deploy_timeout, d.created_at, d.finished_at
FROM deployments d
LEFT JOIN deployment_events e1
  ON d.deployment_id = e1.object_id::uuid
LEFT OUTER JOIN deployment_events e2
  ON (d.deployment_id = e2.object_id::uuid AND e1.created_at < e2.created_at)
WHERE e2.created_at IS NULL AND d.deployment_id = $1`
	deploymentListQuery = `
WITH deployment_events AS (SELECT * FROM events WHERE object_type = 'deployment')
SELECT d.deployment_id, d.app_id, d.old_release_id, d.new_release_id,
  strategy, e1.data->>'status' AS status,
  processes, deploy_timeout, d.created_at, d.finished_at
FROM deployments d
LEFT JOIN deployment_events e1
  ON d.deployment_id = e1.object_id::uuid
LEFT OUTER JOIN deployment_events e2
  ON (d.deployment_id = e2.object_id::uuid AND e1.created_at < e2.created_at)
WHERE e2.created_at IS NULL AND d.app_id = $1 ORDER BY d.created_at DESC`
	eventSelectQuery = `
SELECT event_id, app_id, object_id, object_type, data, created_at
FROM events WHERE event_id = $1`
	eventInsertQuery = `
INSERT INTO events (app_id, object_id, object_type, data)
VALUES ($1, $2, $3, $4)`
	eventInsertUniqueQuery = `
INSERT INTO events (app_id, object_id, unique_id, object_type, data)
VALUES ($1, $2, $3, $4, $5)`
	formationListByAppQuery = `
SELECT app_id, release_id, processes, tags, created_at, updated_at
FROM formations WHERE app_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC
	`
	formationListActiveQuery = `
SELECT
  apps.app_id, apps.name, apps.meta,
  releases.release_id,
  ARRAY(
	SELECT r.artifact_id
	FROM release_artifacts r
	WHERE r.release_id = releases.release_id AND r.deleted_at IS NULL
	ORDER BY r.created_at
  ),
  releases.meta, releases.env, releases.processes,
  formations.processes, formations.tags, formations.updated_at
FROM formations
JOIN apps USING (app_id)
JOIN releases ON releases.release_id = formations.release_id
WHERE (formations.app_id, formations.release_id) IN (
  SELECT app_id, release_id
  FROM formations, json_each_text(formations.processes::json)
  WHERE processes != 'null'
  GROUP BY app_id, release_id
  HAVING SUM(value::int) > 0
)
AND formations.deleted_at IS NULL
ORDER BY updated_at DESC`
	formationListSinceQuery = `
SELECT app_id, release_id, processes, tags, created_at, updated_at
FROM formations WHERE updated_at >= $1 ORDER BY updated_at DESC`
	formationSelectQuery = `
SELECT app_id, release_id, processes, tags, created_at, updated_at
FROM formations WHERE app_id = $1 AND release_id = $2 AND deleted_at IS NULL`
	formationSelectExpandedQuery = `
SELECT
  apps.app_id, apps.name, apps.meta,
  releases.release_id,
  ARRAY(
	SELECT a.artifact_id
	FROM release_artifacts a
	WHERE a.release_id = releases.release_id AND a.deleted_at IS NULL
	ORDER BY a.created_at
  ),
  releases.meta, releases.env, releases.processes,
  formations.processes, formations.tags, formations.updated_at
FROM formations
JOIN apps USING (app_id)
JOIN releases ON releases.release_id = formations.release_id
WHERE formations.app_id = $1 AND formations.release_id = $2 AND formations.deleted_at IS NULL`
	formationInsertQuery = `
INSERT INTO formations (app_id, release_id, processes, tags)
VALUES ($1, $2, $3, $4) RETURNING created_at, updated_at`
	formationUpdateQuery = `
UPDATE formations SET processes = $3, tags = $4, updated_at = now(), deleted_at = NULL
WHERE app_id = $1 AND release_id = $2 RETURNING created_at, updated_at`
	formationDeleteQuery = `
UPDATE formations SET deleted_at = now(), processes = NULL, updated_at = now()
WHERE app_id = $1 AND release_id = $2`
	formationDeleteByAppQuery = `
UPDATE formations SET deleted_at = now(), processes = NULL, updated_at = now()
WHERE app_id = $1 AND deleted_at IS NULL`
	jobListQuery = `
SELECT cluster_id, job_id, host_id, app_id, release_id, process_type, state, meta, exit_status, host_error, run_at, restarts, created_at, updated_at
FROM job_cache WHERE app_id = $1 ORDER BY created_at DESC`
	jobListActiveQuery = `
SELECT cluster_id, job_id, host_id, app_id, release_id, process_type, state, meta, exit_status, host_error, run_at, restarts, created_at, updated_at
FROM job_cache WHERE state = 'starting' OR state = 'up' ORDER BY updated_at DESC`
	jobSelectQuery = `
SELECT cluster_id, job_id, host_id, app_id, release_id, process_type, state, meta, exit_status, host_error, run_at, restarts, created_at, updated_at
FROM job_cache WHERE job_id = $1`
	jobInsertQuery = `
INSERT INTO job_cache (cluster_id, job_id, host_id, app_id, release_id, process_type, state, meta, exit_status, host_error, run_at, restarts)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING created_at, updated_at`
	jobUpdateQuery = `
UPDATE job_cache SET cluster_id = $2, host_id = $3, state = $4, exit_status = $5, host_error = $6, run_at = $7, restarts = $8, updated_at = now()
WHERE job_id = $1 RETURNING created_at, updated_at`
	providerListQuery = `
SELECT provider_id, name, url, created_at, updated_at
FROM providers WHERE deleted_at IS NULL ORDER BY created_at DESC`
	providerSelectByNameQuery = `
SELECT provider_id, name, url, created_at, updated_at
FROM providers WHERE deleted_at IS NULL AND name = $1`
	providerSelectByNameOrIDQuery = `
SELECT provider_id, name, url, created_at, updated_at
FROM providers WHERE deleted_at IS NULL AND (provider_id = $1 OR name = $2) LIMIT 1`
	providerInsertQuery = `
INSERT INTO providers (name, url) VALUES ($1, $2)
RETURNING provider_id, created_at, updated_at`
	resourceListQuery = `
SELECT resource_id, provider_id, external_id, env,
  ARRAY(
	SELECT a.app_id
    FROM app_resources a
	WHERE a.resource_id = r.resource_id AND a.deleted_at IS NULL
	ORDER BY a.created_at DESC
  ), created_at
FROM resources r
WHERE deleted_at IS NULL
ORDER BY created_at DESC`
	resourceListByProviderQuery = `
SELECT resource_id, provider_id, external_id, env,
  ARRAY(
	SELECT a.app_id
    FROM app_resources a
	WHERE a.resource_id = r.resource_id AND a.deleted_at IS NULL
	ORDER BY a.created_at DESC
  ), created_at
FROM resources r
WHERE provider_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC`
	resourceListByAppQuery = `
SELECT DISTINCT(r.resource_id), r.provider_id, r.external_id, r.env,
  ARRAY(
    SELECT a.app_id
	FROM app_resources a
	WHERE a.resource_id = r.resource_id AND a.deleted_at IS NULL
	ORDER BY a.created_at DESC
  ), r.created_at
FROM resources r
JOIN app_resources a USING (resource_id)
WHERE a.app_id = $1 AND r.deleted_at IS NULL AND a.deleted_at IS NULL
ORDER BY r.created_at DESC`
	resourceSelectQuery = `
SELECT resource_id, provider_id, external_id, env,
  ARRAY(
    SELECT app_id
	FROM app_resources a
	WHERE a.resource_id = r.resource_id AND a.deleted_at IS NULL
	ORDER BY a.created_at DESC
  ), created_at
FROM resources r
WHERE resource_id = $1 AND deleted_at IS NULL`
	resourceInsertQuery = `
INSERT INTO resources (resource_id, provider_id, external_id, env)
VALUES ($1, $2, $3, $4) RETURNING created_at`
	resourceDeleteQuery = `
UPDATE resources SET deleted_at = now() WHERE resource_id = $1 AND deleted_at IS NULL`
	appResourceInsertAppByNameQuery = `
INSERT INTO app_resources (app_id, resource_id)
VALUES ((SELECT app_id FROM apps WHERE name = $1 AND deleted_at IS NULL), $2)
RETURNING app_id`
	appResourceInsertAppByNameOrIDQuery = `
INSERT INTO app_resources (app_id, resource_id)
VALUES ((SELECT app_id FROM apps WHERE (app_id = $1 OR name = $2) AND deleted_at IS NULL), $3)
RETURNING app_id`
	appResourceDeleteByAppQuery = `
DELETE FROM app_resources WHERE app_id = $1`
	appResourceDeleteByResourceQuery = `
DELETE FROM app_resources WHERE resource_id = $1`
	domainMigrationInsert = `
INSERT INTO domain_migrations (old_domain, domain, old_tls_cert, tls_cert) VALUES ($1, $2, $3, $4) RETURNING migration_id, created_at`
)
