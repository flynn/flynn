package schema

import (
	"github.com/jackc/pgx"
)

var preparedStatements = map[string]string{
	// misc
	"ping": ping,

	// tcp
	"insert_tcp_route": insertTcpRoute,
	"list_tcp_routes":  listTcpRoutes,
	"select_tcp_route": selectTcpRoute,
	"update_tcp_route": updateTcpRoute,
	"delete_tcp_route": deleteTcpRoute,

	// http
	"insert_http_route": insertHttpRoute,
	"list_http_routes":  listHttpRoutes,
	"select_http_route": selectHttpRoute,
	"update_http_route": updateHttpRoute,
	"delete_http_route": deleteHttpRoute,

	// certificates
	"select_certificate_by_sha":                  selectCertificateBySha,
	"select_certificate":                         selectCertificate,
	"list_certificates":                          listCertificates,
	"list_certificate_routes":                    listCertificateRoutes,
	"insert_certificate":                         insertCertificate,
	"delete_certificate":                         deleteCertificate,
	"insert_route_certificate":                   insertRouteCertificate,
	"delete_route_certificate_by_route_id":       deleteRouteCertificateByRouteId,
	"delete_route_certificate_by_certificate_id": deleteRouteCertificateByCertificateId,
}

func PrepareStatements(conn *pgx.Conn) error {
	for name, sql := range preparedStatements {
		if _, err := conn.Prepare(name, sql); err != nil {
			return err
		}
	}
	return nil
}

const (
	// misc
	ping = `SELECT 1`

	// tcp
	insertTcpRoute = `
	INSERT INTO tcp_routes (parent_ref, service, leader, drain_backends, port)
	VALUES ($1, $2, $3, $4, $5)
	RETURNING id, created_at, updated_at`

	selectTcpRoute = `
	SELECT id, parent_ref, service, leader, drain_backends, port, created_at, updated_at FROM tcp_routes
	WHERE id = $1 AND deleted_at IS NULL`

	updateTcpRoute = `
	UPDATE tcp_routes SET parent_ref = $1, service = $2, leader = $3
	WHERE id = $4 AND port = $5 AND deleted_at IS NULL
	RETURNING id, parent_ref, service, leader, drain_backends, port, created_at, updated_at`

	deleteTcpRoute = `
	UPDATE tcp_routes SET deleted_at = now() 
	WHERE id = $1`

	listTcpRoutes = `
	SELECT id, parent_ref, service, leader, drain_backends, port, created_at, updated_at FROM tcp_routes
	WHERE deleted_at IS NULL`

	// http
	insertHttpRoute = `
	INSERT INTO http_routes (parent_ref, service, leader, drain_backends, domain, sticky, path)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	RETURNING id, created_at, updated_at`

	selectHttpRoute = `
	SELECT r.id, r.parent_ref, r.service, r.leader, r.drain_backends, r.domain, r.sticky, r.path, r.created_at, r.updated_at, c.id, c.cert, c.key, c.created_at, c.updated_at FROM http_routes as r
	LEFT OUTER JOIN route_certificates AS rc on r.id = rc.http_route_id
	LEFT OUTER JOIN certificates AS c ON c.id = rc.certificate_id
	WHERE r.id = $1 AND r.deleted_at IS NULL`

	updateHttpRoute = `
	UPDATE http_routes as r
	SET parent_ref = $1, service = $2, leader = $3, sticky = $4, path = $5
	WHERE id = $6 AND domain = $7 AND deleted_at IS NULL
	RETURNING r.id, r.parent_ref, r.service, r.leader, r.drain_backends, r.domain, r.sticky, r.path, r.created_at, r.updated_at`

	deleteHttpRoute = `UPDATE http_routes SET deleted_at = now() WHERE id = $1`

	listHttpRoutes = `
	SELECT r.id, r.parent_ref, r.service, r.leader, r.drain_backends, r.domain, r.sticky, r.path, r.created_at, r.updated_at, c.id, c.cert, c.key, c.created_at, c.updated_at FROM http_routes as r
	LEFT OUTER JOIN route_certificates AS rc on r.id = rc.http_route_id
	LEFT OUTER JOIN certificates AS c ON c.id = rc.certificate_id
	WHERE r.deleted_at IS NULL
	ORDER BY r.domain, r.path`

	// certificates
	selectCertificate = `
	SELECT c.id, c.cert, c.key, c.created_at, c.updated_at, ARRAY(
		SELECT http_route_id::varchar FROM route_certificates
		WHERE certificate_id = $1
	) FROM certificates AS c WHERE c.id = $1`

	selectCertificateBySha = `
	SELECT id, created_at, updated_at FROM certificates
	WHERE cert_sha256 = $1 AND deleted_at IS NULL`

	listCertificates = `
	SELECT c.id, c.cert, c.key, c.created_at, c.updated_at, ARRAY(
		SELECT http_route_id::varchar FROM route_certificates AS rc
		WHERE rc.certificate_id = c.id
	) FROM certificates AS c`

	listCertificateRoutes = `
	SELECT r.id, r.parent_ref, r.service, r.leader, r.drain_backends, r.domain, r.sticky, r.path, r.created_at, r.updated_at FROM http_routes AS r
	INNER JOIN route_certificates AS rc ON rc.http_route_id = r.id AND rc.certificate_id = $1`

	insertCertificate = `
	INSERT INTO certificates (cert, key, cert_sha256)
	VALUES ($1, $2, $3)
	ON CONFLICT (cert_sha256) WHERE deleted_at IS NULL DO UPDATE SET cert_sha256 = $3
	RETURNING id, created_at, updated_at`

	deleteCertificate = `UPDATE certificates SET deleted_at = now() WHERE id = $1`

	insertRouteCertificate = `
	INSERT INTO route_certificates (http_route_id, certificate_id)
	VALUES ($1, $2)`

	deleteRouteCertificateByCertificateId = `
	DELETE FROM route_certificates
	WHERE certificate_id = $1`

	deleteRouteCertificateByRouteId = `
	DELETE FROM route_certificates
	WHERE http_route_id = $1`
)
