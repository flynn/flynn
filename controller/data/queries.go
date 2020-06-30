package data

import (
	"github.com/flynn/que-go"
	"github.com/jackc/pgx"
)

var preparedStatements = map[string]string{
	"ping":                                  pingQuery,
	"app_list":                              appListQuery,
	"app_list_page":                         appListPageQuery,
	"app_select_by_name":                    appSelectByNameQuery,
	"app_select_by_name_for_update":         appSelectByNameForUpdateQuery,
	"app_select_by_name_or_id":              appSelectByNameOrIDQuery,
	"app_select_by_name_or_id_for_update":   appSelectByNameOrIDForUpdateQuery,
	"app_insert":                            appInsertQuery,
	"app_update_strategy":                   appUpdateStrategyQuery,
	"app_update_meta":                       appUpdateMetaQuery,
	"app_update_release":                    appUpdateReleaseQuery,
	"app_update_deploy_timeout":             appUpdateDeployTimeoutQuery,
	"app_delete":                            appDeleteQuery,
	"app_next_name_id":                      appNextNameIDQuery,
	"app_get_release":                       appGetReleaseQuery,
	"release_list":                          releaseListQuery,
	"release_list_page":                     releaseListPageQuery,
	"release_select":                        releaseSelectQuery,
	"release_insert":                        releaseInsertQuery,
	"release_app_list":                      releaseAppListQuery,
	"release_artifacts_insert":              releaseArtifactsInsertQuery,
	"release_artifacts_delete":              releaseArtifactsDeleteQuery,
	"release_delete":                        releaseDeleteQuery,
	"artifact_list":                         artifactListQuery,
	"artifact_list_ids":                     artifactListIDsQuery,
	"artifact_select":                       artifactSelectQuery,
	"artifact_select_by_type_and_uri":       artifactSelectByTypeAndURIQuery,
	"artifact_insert":                       artifactInsertQuery,
	"artifact_delete":                       artifactDeleteQuery,
	"artifact_release_count":                artifactReleaseCountQuery,
	"artifact_layer_count":                  artifactLayerCountQuery,
	"deployment_list":                       deploymentListQuery,
	"deployment_list_page":                  deploymentListPageQuery,
	"deployment_select":                     deploymentSelectQuery,
	"deployment_select_expanded":            deploymentSelectExpandedQuery,
	"deployment_insert":                     deploymentInsertQuery,
	"deployment_update_finished_at":         deploymentUpdateFinishedAtQuery,
	"deployment_update_finished_at_now":     deploymentUpdateFinishedAtNowQuery,
	"deployment_delete":                     deploymentDeleteQuery,
	"event_select":                          eventSelectQuery,
	"event_select_expanded":                 eventSelectExpandedQuery,
	"event_insert":                          eventInsertQuery,
	"event_insert_op":                       eventInsertOpQuery,
	"event_insert_unique":                   eventInsertUniqueQuery,
	"event_list_page":                       eventListPageQuery,
	"formation_list_by_app":                 formationListByAppQuery,
	"formation_list_by_release":             formationListByReleaseQuery,
	"formation_list_active":                 formationListActiveQuery,
	"formation_list_since":                  formationListSinceQuery,
	"formation_select":                      formationSelectQuery,
	"formation_select_expanded":             formationSelectExpandedQuery,
	"formation_insert":                      formationInsertQuery,
	"formation_delete":                      formationDeleteQuery,
	"formation_delete_by_app":               formationDeleteByAppQuery,
	"scale_request_insert":                  scaleRequestInsertQuery,
	"scale_request_cancel":                  scaleRequestCancelQuery,
	"scale_request_update":                  scaleRequestUpdateQuery,
	"scale_request_list":                    scaleRequestListQuery,
	"job_find_deployment":                   jobFindDeploymentQuery,
	"job_list":                              jobListQuery,
	"job_list_active":                       jobListActiveQuery,
	"job_select":                            jobSelectQuery,
	"job_insert":                            jobInsertQuery,
	"job_volume_insert":                     jobVolumeInsertQuery,
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
	"backup_insert":                         backupInsert,
	"backup_update":                         backupUpdate,
	"backup_select_latest":                  backupSelectLatest,
	"sink_list":                             sinkListQuery,
	"sink_list_since":                       sinkListSinceQuery,
	"sink_select":                           sinkSelectQuery,
	"sink_insert":                           sinkInsertQuery,
	"sink_delete":                           sinkDeleteQuery,
	"volume_list":                           volumeListQuery,
	"volume_app_list":                       volumeAppListQuery,
	"volume_list_since":                     volumeListSinceQuery,
	"volume_select":                         volumeSelectQuery,
	"volume_insert":                         volumeInsertQuery,
	"volume_decommission":                   volumeDecommissionQuery,
	"http_route_list":                       httpRouteListQuery,
	"http_route_list_by_parent_ref":         httpRouteListByParentRefQuery,
	"http_route_insert":                     httpRouteInsertQuery,
	"http_route_select":                     httpRouteSelectQuery,
	"http_route_update":                     httpRouteUpdateQuery,
	"http_route_delete":                     httpRouteDeleteQuery,
	"tcp_route_list":                        tcpRouteListQuery,
	"tcp_route_list_by_parent_ref":          tcpRouteListByParentRefQuery,
	"tcp_route_insert":                      tcpRouteInsertQuery,
	"tcp_route_select":                      tcpRouteSelectQuery,
	"tcp_route_update":                      tcpRouteUpdateQuery,
	"tcp_route_delete":                      tcpRouteDeleteQuery,
	"certificate_insert":                    certificateInsertQuery,
	"route_certificate_delete_by_route_id":  routeCertificateDeleteByRouteIDQuery,
	"route_certificate_insert":              routeCertificateInsertQuery,
}

func PrepareStatements(conn *pgx.Conn) error {
	for name, sql := range preparedStatements {
		if _, err := conn.Prepare(name, sql); err != nil {
			return err
		}
	}
	return que.PrepareStatements(conn)
}

const (
	// misc
	pingQuery = `SELECT 1`
	// apps
	appListQuery = `
SELECT app_id, name, meta, strategy, release_id, deploy_timeout, created_at, updated_at
FROM apps WHERE deleted_at IS NULL ORDER BY created_at DESC`
	appListPageQuery = `
SELECT app_id, name, meta, strategy, release_id, deploy_timeout, created_at, updated_at
FROM apps
WHERE
  deleted_at IS NULL
AND
  CASE WHEN array_length($2::text[], 1) > 0 THEN app_id::text = ANY($2::text[]) ELSE true END
AND
  match_label_filters($3, meta)
AND
CASE WHEN $1::timestamptz IS NOT NULL THEN created_at <= $1::timestamptz ELSE true END
ORDER BY created_at DESC
LIMIT $4;
`
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
UPDATE apps SET release_id = $2, updated_at = now() WHERE app_id = $1
RETURNING updated_at`
	appUpdateDeployTimeoutQuery = `
UPDATE apps SET deploy_timeout = $2, updated_at = now() WHERE app_id = $1`
	appDeleteQuery = `
UPDATE apps SET deleted_at = now() WHERE app_id = $1 AND deleted_at IS NULL`
	appNextNameIDQuery = `
SELECT nextval('name_ids')`
	appGetReleaseQuery = `
SELECT r.release_id, r.app_id,
  ARRAY(
	SELECT a.artifact_id
	FROM release_artifacts a
	WHERE a.release_id = r.release_id AND a.deleted_at IS NULL
	ORDER BY a.index
  ), r.env, r.processes, r.meta, r.created_at
FROM apps a JOIN releases r USING (release_id) WHERE a.app_id = $1 AND r.deleted_at IS NULL`

	releaseListQuery = `
SELECT r.release_id, r.app_id,
  ARRAY(
	SELECT a.artifact_id
	FROM release_artifacts a
	WHERE a.release_id = r.release_id AND a.deleted_at IS NULL
	ORDER BY a.index
  ), r.env, r.processes, r.meta, r.created_at
FROM releases r WHERE r.deleted_at IS NULL ORDER BY r.created_at DESC`
	releaseListPageQuery = `
SELECT r.release_id, r.app_id,
  ARRAY(
    SELECT a.artifact_id
    FROM release_artifacts a
    WHERE a.release_id = r.release_id AND a.deleted_at IS NULL
    ORDER BY a.index
  ), r.env, r.processes, r.meta, r.created_at
FROM releases r
WHERE
  CASE WHEN array_length($1::text[], 1) > 0 THEN r.app_id::text = ANY($1::text[]) ELSE true END
AND
  CASE WHEN array_length($2::text[], 1) > 0 THEN r.release_id::text = ANY($2::text[]) ELSE true END
AND
  match_label_filters($4, r.meta)
AND
  CASE WHEN $3::timestamptz IS NOT NULL THEN r.created_at <= $3::timestamptz ELSE true END
ORDER BY r.created_at DESC
LIMIT $5
`
	releaseSelectQuery = `
SELECT r.release_id, r.app_id,
  ARRAY(
	SELECT a.artifact_id
	FROM release_artifacts a
	WHERE a.release_id = r.release_id AND a.deleted_at IS NULL
	ORDER BY a.index
  ), r.env, r.processes, r.meta, r.created_at
FROM releases r WHERE r.release_id = $1 AND r.deleted_at IS NULL`
	releaseInsertQuery = `
INSERT INTO releases (release_id, app_id, env, processes, meta)
VALUES ($1, $2, $3, $4, $5) RETURNING created_at`
	releaseAppListQuery = `
SELECT r.release_id, r.app_id,
  ARRAY(
	SELECT a.artifact_id
	FROM release_artifacts a
	WHERE a.release_id = r.release_id AND a.deleted_at IS NULL
	ORDER BY a.index
  ), r.env, r.processes, r.meta, r.created_at
FROM releases r WHERE r.app_id = $1 AND r.deleted_at IS NULL ORDER BY r.created_at DESC`
	releaseArtifactsInsertQuery = `
INSERT INTO release_artifacts (release_id, artifact_id, index) VALUES ($1, $2, $3)`
	releaseArtifactsDeleteQuery = `
UPDATE release_artifacts SET deleted_at = now() WHERE release_id = $1 AND artifact_id = $2 AND deleted_at IS NULL`
	releaseDeleteQuery = `
UPDATE releases SET deleted_at = now() WHERE release_id = $1 AND deleted_at IS NULL`
	artifactListQuery = `
SELECT artifact_id, type, uri, meta, manifest, hashes, size, layer_url_template, created_at FROM artifacts
WHERE deleted_at IS NULL ORDER BY created_at DESC`
	artifactListIDsQuery = `
SELECT artifact_id, type, uri, meta, manifest, hashes, size, layer_url_template, created_at FROM artifacts
WHERE deleted_at IS NULL AND artifact_id = ANY($1)`
	artifactSelectQuery = `
SELECT artifact_id, type, uri, meta, manifest, hashes, size, layer_url_template, created_at FROM artifacts
WHERE artifact_id = $1 AND deleted_at IS NULL`
	artifactSelectByTypeAndURIQuery = `
SELECT artifact_id, meta, manifest, hashes, size, layer_url_template, created_at FROM artifacts WHERE type = $1 AND uri = $2 AND deleted_at IS NULL`
	artifactInsertQuery = `
INSERT INTO artifacts (artifact_id, type, uri, meta, manifest, hashes, size, layer_url_template) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING created_at`
	artifactDeleteQuery = `
UPDATE artifacts SET deleted_at = now() WHERE artifact_id = $1 AND deleted_at IS NULL`
	artifactReleaseCountQuery = `
SELECT COUNT(*) FROM release_artifacts WHERE artifact_id = $1 AND deleted_at IS NULL`
	artifactLayerCountQuery = `
SELECT COUNT(*) FROM (
  SELECT jsonb_array_elements(jsonb_array_elements(manifest->'rootfs')->'layers')->'id' AS layer_id
  FROM artifacts
  WHERE deleted_at IS NULL
) AS l WHERE l.layer_id = $1`
	deploymentInsertQuery = `
INSERT INTO deployments (deployment_id, app_id, old_release_id, new_release_id, type, strategy, processes, tags, deploy_timeout, deploy_batch_size)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING created_at`
	deploymentUpdateFinishedAtQuery = `
UPDATE deployments SET finished_at = $2 WHERE deployment_id = $1`
	deploymentUpdateFinishedAtNowQuery = `
UPDATE deployments SET finished_at = now() WHERE deployment_id = $1`
	deploymentDeleteQuery = `
DELETE FROM deployments WHERE deployment_id = $1`
	deploymentSelectQuery = `
SELECT deployment_id, app_id, old_release_id, new_release_id, strategy, deployment_status(deployment_id),
  processes, tags, deploy_timeout, deploy_batch_size, created_at, finished_at
FROM deployments
WHERE deployment_id = $1`
	deploymentSelectExpandedQuery = `
SELECT d.deployment_id, d.app_id, d.old_release_id, d.new_release_id, d.strategy, deployment_status(d.deployment_id),
  d.processes, d.tags, d.deploy_timeout, d.deploy_batch_size, d.created_at, d.finished_at,
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
  ) AS new_artifact_ids, new_r.env, new_r.processes, new_r.meta, new_r.created_at,
  type
FROM deployments d
LEFT OUTER JOIN releases old_r
  ON d.old_release_id = old_r.release_id
LEFT OUTER JOIN releases new_r
  ON d.new_release_id = new_r.release_id
WHERE d.deployment_id = $1
LIMIT 1
`
	deploymentListQuery = `
SELECT deployment_id, app_id, old_release_id, new_release_id, strategy, deployment_status(deployment_id),
  processes, tags, deploy_timeout, deploy_batch_size, created_at, finished_at
FROM deployments
WHERE app_id = $1 ORDER BY created_at DESC`
	deploymentListPageQuery = `
SELECT d.deployment_id, d.app_id, d.old_release_id, d.new_release_id, d.strategy, deployment_status(d.deployment_id),
  d.processes, d.tags, d.deploy_timeout, d.deploy_batch_size, d.created_at, d.finished_at,
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
  ) AS new_artifact_ids, new_r.env, new_r.processes, new_r.meta, new_r.created_at,
  type
FROM deployments d
LEFT OUTER JOIN releases old_r
  ON d.old_release_id = old_r.release_id
LEFT OUTER JOIN releases new_r
  ON d.new_release_id = new_r.release_id
WHERE
  CASE WHEN array_length($1::text[], 1) > 0 THEN d.app_id::text = ANY($1::text[]) ELSE true END
AND
  CASE WHEN array_length($2::text[], 1) > 0 THEN d.deployment_id::text = ANY($2::text[]) ELSE true END
AND
  CASE WHEN array_length($3::text[], 1) > 0 THEN deployment_status(d.deployment_id) = ANY($3::text[]) ELSE true END
AND
  CASE WHEN array_length($4::text[], 1) > 0 THEN d.type::text = ANY($4::text[]) ELSE true END
AND
  CASE WHEN $5::timestamptz IS NOT NULL THEN d.created_at <= $5::timestamptz ELSE true END
ORDER BY d.created_at DESC
LIMIT $6
`
	eventSelectQuery = `
SELECT event_id, app_id, deployment_id, object_id, object_type, data, op, created_at
FROM events WHERE event_id = $1`
	eventSelectExpandedQuery = `
SELECT e.event_id, e.app_id, e.object_id, e.object_type, e.data, e.op, e.created_at,

	d.deployment_id, d.app_id, d.old_release_id, d.new_release_id, d.strategy, deployment_status(d.deployment_id),
  d.processes, d.tags, d.deploy_timeout, d.deploy_batch_size, d.created_at, d.finished_at,
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
  ) AS new_artifact_ids, new_r.env, new_r.processes, new_r.meta, new_r.created_at,
  d.type,

  j.cluster_id, j.job_id, j.host_id, j.app_id, j.release_id, j.process_type, j.state, j.meta,
  j.exit_status, j.host_error, j.run_at, j.restarts, j.created_at, j.updated_at, j.args,
  ARRAY(
    SELECT job_volumes.volume_id
    FROM job_volumes
    WHERE job_volumes.job_id = j.job_id
    ORDER BY job_volumes.index
  ),

	s.scale_request_id, s.app_id, s.release_id, s.state, s.old_processes, s.new_processes, s.old_tags, s.new_tags, s.created_at, s.updated_at
FROM events e
LEFT OUTER JOIN deployments d
	ON d.deployment_id = e.deployment_id
LEFT OUTER JOIN releases old_r
  ON d.old_release_id = old_r.release_id
LEFT OUTER JOIN releases new_r
  ON d.new_release_id = new_r.release_id
LEFT OUTER JOIN job_cache j
	ON e.object_type = 'job' AND j.job_id::text = e.object_id
LEFT OUTER JOIN scale_requests s
	ON e.object_type = 'scale_request' AND s.scale_request_id::text = e.object_id
WHERE e.event_id = $1
LIMIT 1
	`
	eventInsertQuery = `
INSERT INTO events (app_id, object_id, object_type, data)
VALUES ($1, $2, $3, $4)`
	eventInsertOpQuery = `
INSERT INTO events (app_id, deployment_id, object_id, object_type, data, op)
VALUES ($1, $2, $3, $4, $5, $6)`
	eventInsertUniqueQuery = `
INSERT INTO events (app_id, deployment_id, object_id, unique_id, object_type, data)
VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (unique_id) DO NOTHING`
	eventListPageQuery = `
SELECT e.event_id, e.app_id, e.object_id, e.object_type, e.data, e.op, e.created_at,

	d.deployment_id, d.app_id, d.old_release_id, d.new_release_id, d.strategy, deployment_status(d.deployment_id),
  d.processes, d.tags, d.deploy_timeout, d.deploy_batch_size, d.created_at, d.finished_at,
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
  ) AS new_artifact_ids, new_r.env, new_r.processes, new_r.meta, new_r.created_at,
  d.type,

  j.cluster_id, j.job_id, j.host_id, j.app_id, j.release_id, j.process_type, j.state, j.meta,
  j.exit_status, j.host_error, j.run_at, j.restarts, j.created_at, j.updated_at, j.args,
  ARRAY(
    SELECT job_volumes.volume_id
    FROM job_volumes
    WHERE job_volumes.job_id = j.job_id
    ORDER BY job_volumes.index
  ),

	s.scale_request_id, s.app_id, s.release_id, s.state, s.old_processes, s.new_processes, s.old_tags, s.new_tags, s.created_at, s.updated_at
FROM events e
LEFT OUTER JOIN deployments d
	ON d.deployment_id = e.deployment_id
LEFT OUTER JOIN releases old_r
  ON d.old_release_id = old_r.release_id
LEFT OUTER JOIN releases new_r
  ON d.new_release_id = new_r.release_id
LEFT OUTER JOIN job_cache j
	ON e.object_type = 'job' AND j.job_id::text = e.object_id
LEFT OUTER JOIN scale_requests s
	ON e.object_type = 'scale_request' AND s.scale_request_id::text = e.object_id
WHERE
  CASE WHEN array_length($2::text[], 1) > 0 THEN e.app_id::text = ANY($2::text[]) ELSE true END
AND
	CASE WHEN array_length($3::text[], 1) > 0 THEN e.deployment_id::text = ANY($3::text[]) ELSE true END
AND
	CASE WHEN array_length($4::text[], 1) > 0 THEN e.object_type = ANY($4::text[]) ELSE true END
AND
	CASE WHEN $1::timestamptz IS NOT NULL THEN e.created_at <= $1::timestamptz ELSE true END
ORDER BY e.created_at DESC
LIMIT $5;
	`
	formationListByAppQuery = `
SELECT app_id, release_id, processes, tags, created_at, updated_at
FROM formations WHERE app_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC`
	formationListByReleaseQuery = `
SELECT app_id, release_id, processes, tags, created_at, updated_at
FROM formations WHERE release_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC`
	formationListActiveQuery = `
SELECT
  apps.app_id, apps.name, apps.meta, apps.strategy, apps.release_id,
  apps.deploy_timeout, apps.created_at, apps.updated_at,
  releases.release_id,
  ARRAY(
	SELECT r.artifact_id
	FROM release_artifacts r
	WHERE r.release_id = releases.release_id AND r.deleted_at IS NULL
	ORDER BY r.index
  ),
  releases.meta, releases.env, releases.processes, releases.created_at,
  scale_requests.scale_request_id, scale_requests.old_processes, scale_requests.new_processes,
  scale_requests.old_tags, scale_requests.new_tags, scale_requests.created_at,
  formations.processes, formations.tags, formations.updated_at, formations.deleted_at IS NOT NULL
FROM formations
JOIN apps USING (app_id)
JOIN releases ON releases.release_id = formations.release_id
LEFT OUTER JOIN scale_requests
  ON scale_requests.app_id = formations.app_id
  AND scale_requests.release_id = formations.release_id
  AND scale_requests.state = 'pending'
WHERE (formations.app_id, formations.release_id) IN (
  SELECT app_id, release_id
  FROM formations, json_each_text(formations.processes::json)
  WHERE processes != 'null'
  GROUP BY app_id, release_id
  HAVING SUM(value::int) > 0
)
AND formations.deleted_at IS NULL
ORDER BY formations.updated_at DESC`
	formationListSinceQuery = `
SELECT
  apps.app_id, apps.name, apps.meta, apps.strategy, apps.release_id,
  apps.deploy_timeout, apps.created_at, apps.updated_at,
  releases.release_id,
  ARRAY(
	SELECT r.artifact_id
	FROM release_artifacts r
	WHERE r.release_id = releases.release_id AND r.deleted_at IS NULL
	ORDER BY r.index
  ),
  releases.meta, releases.env, releases.processes, releases.created_at,
  scale_requests.scale_request_id, scale_requests.old_processes, scale_requests.new_processes,
  scale_requests.old_tags, scale_requests.new_tags, scale_requests.created_at,
  formations.processes, formations.tags, formations.updated_at, formations.deleted_at IS NOT NULL
FROM formations
JOIN apps USING (app_id)
JOIN releases ON releases.release_id = formations.release_id
LEFT OUTER JOIN scale_requests
  ON scale_requests.app_id = formations.app_id
  AND scale_requests.release_id = formations.release_id
  AND scale_requests.state = 'pending'
WHERE formations.updated_at >= $1 AND formations.deleted_at IS NULL
ORDER BY formations.updated_at DESC`
	formationSelectQuery = `
SELECT app_id, release_id, processes, tags, created_at, updated_at
FROM formations WHERE app_id = $1 AND release_id = $2 AND deleted_at IS NULL`
	formationSelectExpandedQuery = `
SELECT
  apps.app_id, apps.name, apps.meta, apps.strategy, apps.release_id,
  apps.deploy_timeout, apps.created_at, apps.updated_at,
  releases.release_id,
  ARRAY(
	SELECT a.artifact_id
	FROM release_artifacts a
	WHERE a.release_id = releases.release_id AND a.deleted_at IS NULL
	ORDER BY a.index
  ),
  releases.meta, releases.env, releases.processes, releases.created_at,
  scale_requests.scale_request_id, scale_requests.old_processes, scale_requests.new_processes,
  scale_requests.old_tags, scale_requests.new_tags, scale_requests.created_at,
  formations.processes, formations.tags, formations.updated_at, formations.deleted_at IS NOT NULL
FROM formations
JOIN apps USING (app_id)
JOIN releases ON releases.release_id = formations.release_id
LEFT OUTER JOIN scale_requests
  ON scale_requests.app_id = formations.app_id
  AND scale_requests.release_id = formations.release_id
  AND scale_requests.state = 'pending'
WHERE formations.app_id = $1 AND formations.release_id = $2`
	formationInsertQuery = `
INSERT INTO formations (app_id, release_id, processes, tags)
VALUES ($1, $2, $3, $4)
ON CONFLICT ON CONSTRAINT formations_pkey DO UPDATE
SET processes = $3, tags = $4, updated_at = now(), deleted_at = NULL
RETURNING created_at, updated_at`
	formationDeleteQuery = `
UPDATE formations SET deleted_at = now(), processes = NULL, updated_at = now()
WHERE app_id = $1 AND release_id = $2 AND deleted_at IS NULL`
	formationDeleteByAppQuery = `
UPDATE formations SET deleted_at = now(), processes = NULL, updated_at = now()
WHERE app_id = $1 AND deleted_at IS NULL`
	scaleRequestInsertQuery = `
INSERT INTO scale_requests (scale_request_id, app_id, release_id, deployment_id, state, old_processes, new_processes, old_tags, new_tags)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING created_at, updated_at`
	scaleRequestCancelQuery = `
WITH updated AS (
	UPDATE scale_requests SET state = 'cancelled', updated_at = now() WHERE app_id = $1 AND release_id = $2 AND state != 'cancelled'
	RETURNING *
)
SELECT scale_request_id, app_id, release_id, state, old_processes, new_processes, old_tags, new_tags, created_at, updated_at
FROM updated
ORDER BY created_at DESC`
	scaleRequestUpdateQuery = `
UPDATE scale_requests SET state = $2, updated_at = now() WHERE scale_request_id = $1
RETURNING updated_at, deployment_id`
	scaleRequestListQuery = `
SELECT s.scale_request_id, s.app_id, s.release_id, s.state, s.old_processes, s.new_processes, s.old_tags, s.new_tags, s.created_at, s.updated_at
FROM scale_requests s
WHERE
  CASE WHEN array_length($1::text[], 1) > 0 THEN s.app_id::text = ANY($1::text[]) ELSE true END
AND
  CASE WHEN array_length($2::text[], 1) > 0 THEN s.release_id::text = ANY($2::text[]) ELSE true END
AND
  CASE WHEN array_length($3::text[], 1) > 0 THEN s.scale_request_id::text = ANY($3::text[]) ELSE true END
AND
  CASE WHEN array_length($4::text[], 1) > 0 THEN s.state = ANY($4::text[]) ELSE true END
AND
  CASE WHEN $5::timestamptz IS NOT NULL THEN s.created_at <= $5::timestamptz ELSE true END
ORDER BY s.created_at DESC
LIMIT $6
`

	jobFindDeploymentQuery = `
SELECT deployment_id FROM deployments
WHERE app_id = $1
	AND (old_release_id = $2 OR new_release_id = $2)
	AND deployment_status(deployment_id) IN ('pending', 'running')
ORDER BY created_at DESC
LIMIT 1
	`

	jobListQuery = `
SELECT
  cluster_id, job_id, host_id, app_id, release_id, process_type, state, meta,
  exit_status, host_error, run_at, restarts, created_at, updated_at, args,
  ARRAY(
    SELECT job_volumes.volume_id
    FROM job_volumes
    WHERE job_volumes.job_id = job_cache.job_id
    ORDER BY job_volumes.index
  )
FROM job_cache WHERE app_id = $1 ORDER BY created_at DESC`
	jobListActiveQuery = `
SELECT
  cluster_id, job_id, host_id, app_id, release_id, process_type, state, meta,
  exit_status, host_error, run_at, restarts, created_at, updated_at, args,
  ARRAY(
    SELECT job_volumes.volume_id
    FROM job_volumes
    WHERE job_volumes.job_id = job_cache.job_id
    ORDER BY job_volumes.index
  )
FROM job_cache WHERE state = 'pending' OR state = 'starting' OR state = 'up' OR state = 'stopping' ORDER BY updated_at DESC`
	jobSelectQuery = `
SELECT
  cluster_id, job_id, host_id, app_id, release_id, process_type, state, meta,
  exit_status, host_error, run_at, restarts, created_at, updated_at, args,
  ARRAY(
    SELECT job_volumes.volume_id
    FROM job_volumes
    WHERE job_volumes.job_id = job_cache.job_id
    ORDER BY job_volumes.index
  )
FROM job_cache WHERE job_id = $1`
	jobInsertQuery = `
INSERT INTO job_cache (cluster_id, job_id, host_id, app_id, release_id, deployment_id, process_type, state, meta, exit_status, host_error, run_at, restarts, args)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) ON CONFLICT (job_id) DO UPDATE
SET cluster_id = $1, host_id = $3, state = $8, exit_status = $10, host_error = $11, run_at = $12, restarts = $13, args = $14, updated_at = now()
RETURNING created_at, updated_at`
	jobVolumeInsertQuery = `
INSERT INTO job_volumes (job_id, volume_id, index) VALUES ($1, $2, $3)
ON CONFLICT ON CONSTRAINT job_volumes_pkey DO UPDATE SET index = $3
	`
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
	backupInsert = `
INSERT INTO backups (status, sha512, size, error, completed_at) VALUES ($1, $2, $3, $4, $5) RETURNING backup_id, created_at, updated_at`
	backupUpdate = `
UPDATE backups SET status = $2, sha512 = $3, size = $4, error = $5, completed_at = $6, updated_at = now() WHERE backup_id = $1 RETURNING updated_at`
	backupSelectLatest = `
SELECT backup_id, status, sha512, size, error, created_at, updated_at, completed_at FROM backups WHERE deleted_at IS NULL ORDER BY updated_at DESC LIMIT 1`
	sinkListQuery = `
SELECT sink_id, kind, config, created_at, updated_at FROM sinks WHERE deleted_at IS NULL ORDER BY updated_at DESC`
	sinkListSinceQuery = `
SELECT sink_id, kind, config, created_at, updated_at FROM sinks WHERE updated_at >= $1 AND deleted_at IS NULL ORDER BY updated_at DESC`
	sinkSelectQuery = `
SELECT sink_id, kind, config, created_at, updated_at FROM sinks WHERE sink_id = $1`
	sinkInsertQuery = `
INSERT INTO sinks (sink_id, kind, config) VALUES ($1, $2, $3) RETURNING created_at, updated_at`
	sinkDeleteQuery = `
UPDATE sinks SET deleted_at = now() WHERE sink_id = $1 AND deleted_at IS NULL`
	volumeListQuery = `
SELECT volume_id, host_id, type, state, app_id, release_id, job_id, job_type, path, delete_on_stop, meta, created_at, updated_at, decommissioned_at FROM volumes ORDER BY updated_at DESC`
	volumeAppListQuery = `
SELECT volume_id, host_id, type, state, app_id, release_id, job_id, job_type, path, delete_on_stop, meta, created_at, updated_at, decommissioned_at FROM volumes WHERE app_id = $1 ORDER BY updated_at DESC`
	volumeListSinceQuery = `
SELECT volume_id, host_id, type, state, app_id, release_id, job_id, job_type, path, delete_on_stop, meta, created_at, updated_at, decommissioned_at FROM volumes WHERE updated_at >= $1 ORDER BY updated_at DESC`
	volumeSelectQuery = `
SELECT volume_id, host_id, type, state, app_id, release_id, job_id, job_type, path, delete_on_stop, meta, created_at, updated_at, decommissioned_at FROM volumes WHERE app_id = $1 AND volume_id = $2`
	volumeInsertQuery = `
INSERT INTO volumes (volume_id, host_id, type, state, app_id, release_id, job_id, job_type, path, delete_on_stop, meta) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (volume_id) DO UPDATE SET job_id = $7, updated_at = now()
RETURNING created_at, updated_at`
	volumeDecommissionQuery = `
UPDATE volumes SET updated_at = now(), decommissioned_at = now() WHERE app_id = $1 AND volume_id = $2 RETURNING updated_at, decommissioned_at`
	httpRouteListQuery = `
SELECT r.id, r.parent_ref, r.service, r.port, r.leader, r.drain_backends, r.domain, r.sticky, r.path, r.disable_keep_alives, r.created_at, r.updated_at, c.id, c.cert, c.key, c.created_at, c.updated_at FROM http_routes as r
LEFT OUTER JOIN route_certificates AS rc on r.id = rc.http_route_id
LEFT OUTER JOIN certificates AS c ON c.id = rc.certificate_id
WHERE r.deleted_at IS NULL
ORDER BY r.domain, r.path`
	httpRouteListByParentRefQuery = `
SELECT r.id, r.parent_ref, r.service, r.port, r.leader, r.drain_backends, r.domain, r.sticky, r.path, r.disable_keep_alives, r.created_at, r.updated_at, c.id, c.cert, c.key, c.created_at, c.updated_at FROM http_routes as r
LEFT OUTER JOIN route_certificates AS rc on r.id = rc.http_route_id
LEFT OUTER JOIN certificates AS c ON c.id = rc.certificate_id
WHERE r.parent_ref = $1 AND r.deleted_at IS NULL
ORDER BY r.domain, r.path`
	httpRouteInsertQuery = `
INSERT INTO http_routes (parent_ref, service, port, leader, drain_backends, domain, sticky, path, disable_keep_alives)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, path, created_at, updated_at`
	httpRouteSelectQuery = `
SELECT r.id, r.parent_ref, r.service, r.port, r.leader, r.drain_backends, r.domain, r.sticky, r.path, r.disable_keep_alives, r.created_at, r.updated_at, c.id, c.cert, c.key, c.created_at, c.updated_at FROM http_routes as r
LEFT OUTER JOIN route_certificates AS rc on r.id = rc.http_route_id
LEFT OUTER JOIN certificates AS c ON c.id = rc.certificate_id
WHERE r.id = $1 AND r.deleted_at IS NULL`
	httpRouteUpdateQuery = `
UPDATE http_routes as r
SET parent_ref = $1, service = $2, port = $3, leader = $4, sticky = $5, path = $6, disable_keep_alives = $7
WHERE id = $8 AND domain = $9 AND deleted_at IS NULL
RETURNING r.id, r.parent_ref, r.service, r.port, r.leader, r.drain_backends, r.domain, r.sticky, r.path, r.disable_keep_alives, r.created_at, r.updated_at`
	httpRouteDeleteQuery = `
UPDATE http_routes SET deleted_at = now()
WHERE id = $1`
	tcpRouteListQuery = `
SELECT id, parent_ref, service, port, leader, drain_backends, created_at, updated_at FROM tcp_routes
WHERE deleted_at IS NULL`
	tcpRouteListByParentRefQuery = `
SELECT id, parent_ref, service, port, leader, drain_backends, created_at, updated_at FROM tcp_routes
WHERE parent_ref = $1 AND deleted_at IS NULL`
	tcpRouteInsertQuery = `
INSERT INTO tcp_routes (parent_ref, service, port, leader, drain_backends)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, port, created_at, updated_at`
	tcpRouteSelectQuery = `
SELECT id, parent_ref, service, port, leader, drain_backends, created_at, updated_at FROM tcp_routes
WHERE id = $1 AND deleted_at IS NULL`
	tcpRouteUpdateQuery = `
UPDATE tcp_routes SET parent_ref = $1, service = $2, port = $3, leader = $4
WHERE id = $5 AND deleted_at IS NULL
RETURNING id, parent_ref, service, port, leader, drain_backends, created_at, updated_at`
	tcpRouteDeleteQuery = `
UPDATE tcp_routes SET deleted_at = now()
WHERE id = $1`
	certificateInsertQuery = `
INSERT INTO certificates (cert, key, cert_sha256)
VALUES ($1, $2, $3)
ON CONFLICT (cert_sha256) WHERE deleted_at IS NULL DO UPDATE SET cert_sha256 = $3
RETURNING id, created_at, updated_at`
	routeCertificateDeleteByRouteIDQuery = `
DELETE FROM route_certificates
WHERE http_route_id = $1`
	routeCertificateInsertQuery = `
INSERT INTO route_certificates (http_route_id, certificate_id)
VALUES ($1, $2)`
)
