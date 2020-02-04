package data

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/httphelper"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	router "github.com/flynn/flynn/router/types"
	"github.com/jackc/pgx"
)

var ErrRouteNotFound = errors.New("controller: route not found")

type RouteRepo struct {
	db *postgres.DB
}

func NewRouteRepo(db *postgres.DB) *RouteRepo {
	return &RouteRepo{db: db}
}

func (r *RouteRepo) Add(route *router.Route) error {
	existingRoutes, err := r.List("")
	if err != nil {
		return err
	}
	if err := r.validate(route, existingRoutes, routeOpCreate); err != nil {
		return err
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	if err := r.addTx(tx, route); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *RouteRepo) addTx(tx *postgres.DBTx, route *router.Route) error {
	var err error
	switch route.Type {
	case "http":
		err = r.addHTTP(tx, route)
	case "tcp":
		err = r.addTCP(tx, route)
	default:
		return hh.ValidationErr("type", "is invalid (must be 'http' or 'tcp')")
	}
	if err != nil {
		return err
	}
	return r.createEvent(tx, route, ct.EventTypeRoute)
}

func (r *RouteRepo) addHTTP(tx *postgres.DBTx, route *router.Route) error {
	if err := tx.QueryRow(
		"http_route_insert",
		route.ParentRef,
		route.Service,
		route.Port,
		route.Leader,
		route.DrainBackends,
		route.Domain,
		route.Sticky,
		route.Path,
		route.DisableKeepAlives,
	).Scan(&route.ID, &route.Path, &route.CreatedAt, &route.UpdatedAt); err != nil {
		return err
	}
	return r.addRouteCertWithTx(tx, route)
}

func (r *RouteRepo) addTCP(tx *postgres.DBTx, route *router.Route) error {
	return tx.QueryRow(
		"tcp_route_insert",
		route.ParentRef,
		route.Service,
		route.Port,
		route.Leader,
		route.DrainBackends,
	).Scan(&route.ID, &route.Port, &route.CreatedAt, &route.UpdatedAt)
}

func (r *RouteRepo) addCertWithTx(tx *postgres.DBTx, cert *router.Certificate) error {
	tlsCertSHA256 := sha256.Sum256([]byte(cert.Cert))
	if err := tx.QueryRow(
		"certificate_insert",
		cert.Cert,
		cert.Key,
		tlsCertSHA256[:],
	).Scan(&cert.ID, &cert.CreatedAt, &cert.UpdatedAt); err != nil {
		return err
	}
	for _, rid := range cert.Routes {
		if err := tx.Exec("route_certificate_delete_by_route_id", rid); err != nil {
			return err
		}
		if err := tx.Exec("route_certificate_insert", rid, cert.ID); err != nil {
			return err
		}
	}
	return nil
}

func (r *RouteRepo) addRouteCertWithTx(tx *postgres.DBTx, route *router.Route) error {
	cert := route.Certificate
	if cert == nil || (len(cert.Cert) == 0 && len(cert.Key) == 0) {
		return nil
	}
	cert.Routes = []string{route.ID}
	if err := r.addCertWithTx(tx, cert); err != nil {
		return err
	}
	return nil
}

func (r *RouteRepo) Get(typ, id string) (*router.Route, error) {
	if id == "" {
		return nil, ErrRouteNotFound
	}
	var (
		route *router.Route
		err   error
	)
	switch typ {
	case "http":
		route, err = r.getHTTP(id)
	case "tcp":
		route, err = r.getTCP(id)
	default:
		err = ErrRouteNotFound
	}
	if err == pgx.ErrNoRows {
		err = ErrRouteNotFound
	}
	return route, err
}

func (r *RouteRepo) getHTTP(id string) (*router.Route, error) {
	return scanHTTPRoute(r.db.QueryRow("http_route_select", id))
}

func scanHTTPRoute(s postgres.Scanner) (*router.Route, error) {
	var (
		route         router.Route
		certID        *string
		certRoutes    *string
		certCert      *string
		certKey       *string
		certCreatedAt *time.Time
		certUpdatedAt *time.Time
	)
	if err := s.Scan(
		&route.ID,
		&route.ParentRef,
		&route.Service,
		&route.Port,
		&route.Leader,
		&route.DrainBackends,
		&route.Domain,
		&route.Sticky,
		&route.Path,
		&route.DisableKeepAlives,
		&route.CreatedAt,
		&route.UpdatedAt,
		&certID,
		&certRoutes,
		&certCert,
		&certKey,
		&certCreatedAt,
		&certUpdatedAt,
	); err != nil {
		return nil, err
	}
	route.Type = "http"
	if certID != nil {
		route.Certificate = &router.Certificate{
			ID:        *certID,
			Cert:      *certCert,
			Key:       *certKey,
			Routes:    splitPGStringArray(*certRoutes),
			CreatedAt: *certCreatedAt,
			UpdatedAt: *certUpdatedAt,
		}
	}
	return &route, nil
}

func (r *RouteRepo) getTCP(id string) (*router.Route, error) {
	return scanTCPRoute(r.db.QueryRow("tcp_route_select", id))
}

func scanTCPRoute(s postgres.Scanner) (*router.Route, error) {
	var route router.Route
	if err := s.Scan(
		&route.ID,
		&route.ParentRef,
		&route.Service,
		&route.Port,
		&route.Leader,
		&route.DrainBackends,
		&route.CreatedAt,
		&route.UpdatedAt,
	); err != nil {
		return nil, err
	}
	route.Type = "tcp"
	return &route, nil
}

func (r *RouteRepo) List(parentRef string) ([]*router.Route, error) {
	return r.list(r.db, parentRef)
}

func (r *RouteRepo) list(db rowQueryer, parentRef string) ([]*router.Route, error) {
	httpRoutes, err := r.listHTTP(db, parentRef)
	if err != nil {
		return nil, err
	}
	tcpRoutes, err := r.listTCP(db, parentRef)
	if err != nil {
		return nil, err
	}
	return append(httpRoutes, tcpRoutes...), nil
}

func (r *RouteRepo) listHTTP(db rowQueryer, parentRef string) ([]*router.Route, error) {
	var (
		rows *pgx.Rows
		err  error
	)
	if parentRef != "" {
		rows, err = db.Query("http_route_list_by_parent_ref", parentRef)
	} else {
		rows, err = db.Query("http_route_list")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var routes []*router.Route
	for rows.Next() {
		route, err := scanHTTPRoute(rows)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	return routes, rows.Err()
}

func (r *RouteRepo) listTCP(db rowQueryer, parentRef string) ([]*router.Route, error) {
	var (
		rows *pgx.Rows
		err  error
	)
	if parentRef != "" {
		rows, err = db.Query("tcp_route_list_by_parent_ref", parentRef)
	} else {
		rows, err = db.Query("tcp_route_list")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var routes []*router.Route
	for rows.Next() {
		route, err := scanTCPRoute(rows)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	return routes, rows.Err()
}

func (r *RouteRepo) Update(route *router.Route) error {
	existingRoutes, err := r.List("")
	if err != nil {
		return err
	}
	if err := r.validate(route, existingRoutes, routeOpUpdate); err != nil {
		return err
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	if err := r.updateTx(tx, route); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *RouteRepo) updateTx(tx *postgres.DBTx, route *router.Route) error {
	var err error
	switch route.Type {
	case "http":
		err = r.updateHTTP(tx, route)
	case "tcp":
		err = r.updateTCP(tx, route)
	default:
		err = ErrRouteNotFound
	}
	if err == pgx.ErrNoRows {
		err = ErrRouteNotFound
	}
	if err != nil {
		return err
	}
	return r.createEvent(tx, route, ct.EventTypeRoute)
}

func (r *RouteRepo) updateHTTP(tx *postgres.DBTx, route *router.Route) error {
	if err := tx.QueryRow(
		"http_route_update",
		route.ParentRef,
		route.Service,
		route.Port,
		route.Leader,
		route.Sticky,
		route.Path,
		route.DisableKeepAlives,
		route.ID,
		route.Domain,
	).Scan(
		&route.ID,
		&route.ParentRef,
		&route.Service,
		&route.Port,
		&route.Leader,
		&route.DrainBackends,
		&route.Domain,
		&route.Sticky,
		&route.Path,
		&route.DisableKeepAlives,
		&route.CreatedAt,
		&route.UpdatedAt,
	); err != nil {
		return err
	}
	return r.addRouteCertWithTx(tx, route)
}

func (r *RouteRepo) updateTCP(tx *postgres.DBTx, route *router.Route) error {
	return tx.QueryRow(
		"tcp_route_update",
		route.ParentRef,
		route.Service,
		route.Port,
		route.Leader,
		route.ID,
	).Scan(
		&route.ID,
		&route.ParentRef,
		&route.Service,
		&route.Port,
		&route.Leader,
		&route.DrainBackends,
		&route.CreatedAt,
		&route.UpdatedAt,
	)
}

func (r *RouteRepo) Delete(route *router.Route) error {
	existingRoutes, err := r.List("")
	if err != nil {
		return err
	}
	if err := r.validate(route, existingRoutes, routeOpDelete); err != nil {
		return err
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	if err := r.deleteTx(tx, route); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *RouteRepo) deleteTx(tx *postgres.DBTx, route *router.Route) error {
	var err error
	switch route.Type {
	case "http":
		err = tx.Exec("http_route_delete", route.ID)
	case "tcp":
		err = tx.Exec("tcp_route_delete", route.ID)
	default:
		err = ErrRouteNotFound
	}
	if err != nil {
		return err
	}
	return r.createEvent(tx, route, ct.EventTypeRouteDeletion)
}

func (r *RouteRepo) createEvent(tx *postgres.DBTx, route *router.Route, typ ct.EventType) error {
	var appID string
	if strings.HasPrefix(route.ParentRef, ct.RouteParentRefPrefix) {
		appID = strings.TrimPrefix(route.ParentRef, ct.RouteParentRefPrefix)
	}
	hash := md5.New()
	io.WriteString(hash, appID)
	io.WriteString(hash, string(typ))
	io.WriteString(hash, route.ID)
	io.WriteString(hash, route.CreatedAt.String())
	io.WriteString(hash, route.UpdatedAt.String())
	uniqueID := fmt.Sprintf("%x", hash.Sum(nil))
	return CreateEvent(tx.Exec, &ct.Event{
		AppID:      appID,
		ObjectID:   route.ID,
		ObjectType: typ,
		UniqueID:   uniqueID,
	}, route)
}

// routeOp represents an operation that is performed on a route and is used to
// decide what type of validation to perform
type routeOp string

const (
	routeOpCreate routeOp = "create"
	routeOpUpdate routeOp = "update"
	routeOpDelete routeOp = "delete"
)

// validate validates the given route against the list of existing routes for
// the given operation
func (r *RouteRepo) validate(route *router.Route, existingRoutes []*router.Route, op routeOp) error {
	switch route.Type {
	case "http":
		return r.validateHTTP(route, existingRoutes, op)
	case "tcp":
		return r.validateTCP(route, existingRoutes, op)
	default:
		return hh.ValidationErr("type", "is invalid (must be 'http' or 'tcp')")
	}
}

// validateHTTP validates an HTTP route
func (r *RouteRepo) validateHTTP(route *router.Route, existingRoutes []*router.Route, op routeOp) error {
	if op == routeOpDelete {
		// If we are removing a default route ensure no dependent routes left
		if route.Path == "/" {
			for _, existing := range existingRoutes {
				if existing.Domain == route.Domain && existing.Path != "/" {
					return hh.ValidationErr("", fmt.Sprintf(
						"cannot delete default route as a dependent route with path=%s exists",
						existing.Path,
					))
				}
			}
		}
		// don't do any further validation on a route we're deleting
		return nil
	}

	// check the domain and service are set
	if route.Domain == "" {
		return hh.ValidationErr("domain", "must be set")
	}
	if route.Service == "" {
		return hh.ValidationErr("service", "must be set")
	}

	// check the default port is used
	if route.Port > 0 {
		return hh.ValidationErr("port", "must have the default value of zero")
	}

	// normalise the path
	route.Path = normaliseRoutePath(route.Path)

	// path must start with a slash
	if route.Path[0] != '/' {
		return hh.ValidationErr("path", "must start with a forward slash")
	}

	// check routes are unique on domain, port and path
	for _, existing := range existingRoutes {
		if existing.Type != "http" || (op == routeOpUpdate && existing.ID == route.ID) {
			continue
		}
		if existing.Domain == route.Domain && existing.Port == route.Port && existing.Path == route.Path {
			return hh.ConflictErr(fmt.Sprintf("a http route with domain=%s and path=%s already exists", route.Domain, route.Path))
		}
	}

	// If path not the default then validate that a default route exists
	if route.Path != "/" {
		defaultExists := false
		for _, existing := range existingRoutes {
			if existing.Type == "http" && existing.Domain == route.Domain && existing.Path == "/" {
				defaultExists = true
				break
			}
		}
		if !defaultExists {
			return hh.ValidationErr("path", "is not allowed as there is no route at the default path")
		}
	}

	// check that all routes with the same service have the same drain_backends
	for _, existing := range existingRoutes {
		if existing.Type == "http" && existing.Service == route.Service && existing.DrainBackends != route.DrainBackends {
			msg := fmt.Sprintf(
				"cannot create route with mismatch drain_backends=%v, other routes for service %s exist with drain_backends=%v",
				route.DrainBackends, route.Service, existing.DrainBackends,
			)
			return hh.ValidationErr("drain_backends", msg)
		}
	}

	// handle legacy certificate fields
	if route.LegacyTLSCert != "" || route.LegacyTLSKey != "" {
		// setting both legacy and route.Certificate is an error
		if route.Certificate != nil {
			return hh.ValidationErr("certificate", "cannot be set along with the deprecated tls_cert and tls_key")
		}
		route.Certificate = &router.Certificate{
			Cert: route.LegacyTLSCert,
			Key:  route.LegacyTLSKey,
		}
	}

	// validate the certificate if set
	cert := route.Certificate
	if cert != nil && len(cert.Cert) > 0 && len(cert.Key) > 0 {
		cert.Cert = strings.Trim(cert.Cert, " \n")
		cert.Key = strings.Trim(cert.Key, " \n")

		if _, err := tls.X509KeyPair([]byte(cert.Cert), []byte(cert.Key)); err != nil {
			return hh.ValidationErr("certificate", fmt.Sprintf("is invalid: %s", err))
		}
	}

	return nil
}

// validateTCP validates a TCP route
func (r *RouteRepo) validateTCP(route *router.Route, existingRoutes []*router.Route, op routeOp) error {
	// don't validate routes that are being deleted
	if op == routeOpDelete {
		return nil
	}

	// don't allow default HTTP ports
	if route.Port == 80 || route.Port == 443 {
		return hh.ConflictErr("Port reserved for HTTP/HTTPS traffic")
	}

	// assign an available port if the port is unset
	if route.Port == 0 {
	outer:
		for port := int32(3000); port <= 3500; port++ {
			for _, existing := range existingRoutes {
				if existing.Type == "tcp" && existing.Port == port {
					continue outer
				}
			}
			route.Port = port
			break
		}
	}

	// check that the port is in range
	if route.Port <= 0 || route.Port >= 65535 {
		return hh.ValidationErr("port", "must be between 0 and 65535")
	}

	// check that the port is unused
	for _, existing := range existingRoutes {
		if existing.Type != "tcp" || (op == routeOpUpdate && existing.ID == route.ID) {
			continue
		}
		if existing.Port == route.Port {
			return hh.ConflictErr(fmt.Sprintf("a tcp route with port=%d already exists", route.Port))
		}
	}

	// check the service is set
	if route.Service == "" {
		return hh.ValidationErr("service", "must be set")
	}

	// check that all routes with the same service have the same drain_backends
	for _, existing := range existingRoutes {
		if existing.Type == "tcp" && existing.Service == route.Service && existing.DrainBackends != route.DrainBackends {
			msg := fmt.Sprintf(
				"cannot create route with mismatch drain_backends=%v, other routes for service %s exist with drain_backends=%v",
				route.DrainBackends, route.Service, existing.DrainBackends,
			)
			return hh.ValidationErr("drain_backends", msg)
		}
	}

	return nil
}

// normaliseRoutePath normalises a route path by ensuring it ends with a
// forward slash
func normaliseRoutePath(path string) string {
	if !strings.HasSuffix(path, "/") {
		return path + "/"
	}
	return path
}
