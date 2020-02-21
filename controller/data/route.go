package data

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/flynn/flynn/controller/api"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/httphelper"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	router "github.com/flynn/flynn/router/types"
	"github.com/jackc/pgx"
	cjson "github.com/tent/canonical-json-go"
)

var ErrRouteNotFound = errors.New("controller: route not found")

type RouteRepo struct {
	db *postgres.DB
}

func NewRouteRepo(db *postgres.DB) *RouteRepo {
	return &RouteRepo{db: db}
}

// Set takes the desired list of routes for a set of apps, calculates the
// changes that are needed to the existing routes to realise that list, and
// then either atomically applies those changes or returns them for user
// confirmation (otherwise known as a dry run).
//
// The given list of app routes are expected to contain the desired
// configuration for all of the app's routes, and so if any existing routes are
// not contained in the list, or they match ones in the list but have different
// configuration, then they will be either deleted or updated.
//
// If dryRun is true, then the state of all existing routes is calculated and
// returned along with the changes without applying them so that a user can
// inspect the changes and then set the routes again but specifying the state
// that they expect the changes to be applied to, with the request being
// rejected if the state differs.
//
// If dryRun is false, then changes are atomically both calculated and applied,
// first checking that expectedState matches the state of existing routes if
// set.
func (r *RouteRepo) Set(routes []*api.AppRoutes, dryRun bool, expectedState []byte) ([]*api.RouteChange, []byte, error) {
	// check the routes have required fields set
	if err := validateAPIRoutes(routes); err != nil {
		return nil, nil, err
	}

	// if we're doing a dry run, just load the existing routes and return
	// their state along with what changes would be applied
	if dryRun {
		existingRoutes, err := r.List("")
		if err != nil {
			return nil, nil, err
		}
		state := RouteState(existingRoutes)
		changes, err := r.set(nil, routes, existingRoutes)
		return changes, state, err
	}

	// we're not doing a dry run, so load the existing routes and apply the
	// requested changes using a db transaction
	tx, err := r.db.Begin()
	if err != nil {
		return nil, nil, err
	}

	existingRoutes, err := r.listForUpdate(tx, "")
	if err != nil {
		tx.Rollback()
		return nil, nil, err
	}

	// if the request includes an expected state, check it matches the
	// current state of the existing routes
	currentState := RouteState(existingRoutes)
	if len(expectedState) > 0 {
		if !bytes.Equal(expectedState, currentState) {
			tx.Rollback()
			msg := "the expected route state in the request does not match the current state"
			return nil, nil, httphelper.PreconditionFailedErr(msg)
		}
	}

	// set the routes and return the changes
	changes, err := r.set(tx, routes, existingRoutes)
	if err != nil {
		tx.Rollback()
		return nil, nil, err
	}
	return changes, currentState, tx.Commit()
}

func (r *RouteRepo) set(tx *postgres.DBTx, desiredAppRoutes []*api.AppRoutes, existingRoutes []*router.Route) ([]*api.RouteChange, error) {
	// determine which routes we are going to create, update or delete for each
	// app first so that we can then apply them in the order we want to (e.g.
	// we want to process all deletes before updates and creates to support
	// moving routes between apps)
	var creates []*router.Route
	var updates []*routeUpdate
	var deletes []*router.Route
	for _, appRoutes := range desiredAppRoutes {
		// ensure the app exists
		appID := strings.TrimPrefix(appRoutes.App, "apps/")
		app, err := selectApp(r.db, appID, false)
		if err != nil {
			if err == ErrNotFound {
				err = hh.ValidationErr("", fmt.Sprintf("app not found: %s", appID))
			}
			return nil, err
		}

		// track desired routes that already exist so we know not to create them
		exists := make(map[*api.Route]struct{}, len(appRoutes.Routes))

		// iterate over the app's existing routes to determine what changes to make
		for _, existingRoute := range existingRoutes {
			if existingRoute.ParentRef != ct.RouteParentRefPrefix+app.ID {
				continue
			}

			// we should delete the route unless we find a matching desired route
			shouldDelete := true

			for _, desiredRoute := range appRoutes.Routes {
				// check if the desired route matches the existing route
				if routesMatchForUpdate(existingRoute, desiredRoute) {
					// track that the desired route exists so we don't create it
					exists[desiredRoute] = struct{}{}

					// we shouldn't delete the existing route now that it matches
					shouldDelete = false

					// track this as an update if the configuration differs
					if !routesEqualForUpdate(existingRoute, desiredRoute) {
						update := ToRouterRoute(app.ID, desiredRoute)
						update.ID = existingRoute.ID
						updates = append(updates, &routeUpdate{
							existingRoute: existingRoute,
							updatedRoute:  update,
						})
					}

					break
				}
			}

			// track as a delete if we didn't match with a desired route
			if shouldDelete {
				deletes = append(deletes, existingRoute)
			}
		}

		// track routes to create that don't exist
		for _, route := range appRoutes.Routes {
			if _, ok := exists[route]; ok {
				continue
			}
			creates = append(creates, ToRouterRoute(app.ID, route))
		}
	}

	// process the operations and track the changes made
	var changes []*api.RouteChange

	// process deletions first so they don't affect further validations
	// (e.g. so a domain can be deleted from one app and added to another
	// in the same request)
	for _, routeToDelete := range deletes {
		if err := r.validate(routeToDelete, existingRoutes, routeOpDelete); err != nil {
			return nil, err
		}
		// actually perform the delete if we have a db transaction
		if tx != nil {
			if err := r.deleteTx(tx, routeToDelete); err != nil {
				return nil, err
			}
		}
		changes = append(changes, &api.RouteChange{
			Action: api.RouteChange_ACTION_DELETE,
			Before: ToAPIRoute(routeToDelete),
		})
		// remove the deleted route from the existing routes so it no
		// longer affects validations
		newExistingRoutes := make([]*router.Route, 0, len(existingRoutes))
		for _, route := range existingRoutes {
			if route.ID == routeToDelete.ID {
				continue
			}
			newExistingRoutes = append(newExistingRoutes, route)
		}
		existingRoutes = newExistingRoutes
	}

	// process updates
	for _, u := range updates {
		if err := r.validate(u.updatedRoute, existingRoutes, routeOpUpdate); err != nil {
			return nil, err
		}
		// actually perform the update if we have a db transaction
		if tx != nil {
			if err := r.updateTx(tx, u.updatedRoute); err != nil {
				return nil, err
			}
		}
		changes = append(changes, &api.RouteChange{
			Action: api.RouteChange_ACTION_UPDATE,
			Before: ToAPIRoute(u.existingRoute),
			After:  ToAPIRoute(u.updatedRoute),
		})
		// replace the existing route with the updated one in
		// the existing routes so that it affects future
		// validations
		newExistingRoutes := make([]*router.Route, 0, len(existingRoutes))
		for _, route := range existingRoutes {
			if u.updatedRoute.ID == route.ID {
				newExistingRoutes = append(newExistingRoutes, u.updatedRoute)
			} else {
				newExistingRoutes = append(newExistingRoutes, route)
			}
		}
		existingRoutes = newExistingRoutes
	}

	// process creates
	for _, newRoute := range creates {
		if err := r.validate(newRoute, existingRoutes, routeOpCreate); err != nil {
			return nil, err
		}
		// actually perform the create if we have a db transaction
		if tx != nil {
			if err := r.addTx(tx, newRoute); err != nil {
				return nil, err
			}
		}
		changes = append(changes, &api.RouteChange{
			Action: api.RouteChange_ACTION_CREATE,
			After:  ToAPIRoute(newRoute),
		})
		// add the new route to the existing routes so that
		// it affects future validations
		existingRoutes = append(existingRoutes, newRoute)
	}

	return changes, nil
}

func (r *RouteRepo) Add(route *router.Route) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	existingRoutes, err := r.listForUpdate(tx, "")
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := r.validate(route, existingRoutes, routeOpCreate); err != nil {
		tx.Rollback()
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
	cert.ID = router.CertificateID([]byte(cert.Cert))

	var keyID string
	if cert.Key != "" {
		key, err := r.addKey(tx, []byte(cert.Key))
		if err != nil {
			return err
		}
		keyID = key.ID
	}
	if keyID == "" {
		keyID = router.CertificateKeyID([]byte(cert.Cert))
		key, err := scanKey(tx.QueryRow("tls_key_select", keyID))
		if err != nil {
			return hh.ValidationErr("certificate", fmt.Sprintf("key not found: %s", keyID))
		}
		var keyPEM bytes.Buffer
		if err := pem.Encode(&keyPEM, &pem.Block{Type: "PRIVATE KEY", Bytes: key.Key}); err != nil {
			return err
		}
		cert.Key = keyPEM.String()
	}
	if err := tx.QueryRow(
		"certificate_insert",
		cert.ID,
		cert.Cert,
		keyID,
	).Scan(&cert.CreatedAt, &cert.UpdatedAt); err != nil {
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

func (r *RouteRepo) addKey(tx dbOrTx, keyPEM []byte) (*router.Key, error) {
	key, err := router.NewKey(bytes.Trim(keyPEM, " \n"))
	if err != nil {
		return nil, err
	}
	if err := tx.QueryRow(
		"tls_key_insert",
		key.ID,
		string(key.Algorithm),
		key.Key,
	).Scan(&key.CreatedAt); err != nil {
		return nil, err
	}
	return key, nil
}

func (r *RouteRepo) AddKey(keyDER []byte) (*router.Key, error) {
	var keyPEM bytes.Buffer
	if err := pem.Encode(&keyPEM, &pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}); err != nil {
		return nil, err
	}
	return r.addKey(r.db, keyPEM.Bytes())
}

func (r *RouteRepo) ListKeys() ([]*router.Key, error) {
	rows, err := r.db.Query("tls_key_list")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []*router.Key
	for rows.Next() {
		key, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}

func scanKey(s postgres.Scanner) (*router.Key, error) {
	var (
		key   router.Key
		algo  string
		certs string
	)
	if err := s.Scan(
		&key.ID,
		&algo,
		&key.Key,
		&certs,
		&key.CreatedAt,
	); err != nil {
		return nil, err
	}
	key.Algorithm = router.KeyAlgorithm(algo)
	key.Certificates = splitPGStringArray(certs)
	return &key, nil
}

func (r *RouteRepo) DeleteKey(name string) (*router.Key, error) {
	// start a transaction
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}

	// get the key
	key, err := scanKey(tx.QueryRow("tls_key_select", strings.TrimPrefix(name, "tls-keys/")))
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// ensure the key is not referenced by any certificates
	if len(key.Certificates) > 0 {
		tx.Rollback()
		return nil, fmt.Errorf("cannot delete key as it is referenced by the following certificates: %s", strings.Join(key.Certificates, ", "))
	}

	// delete the key
	if err := tx.Exec("tls_key_delete", key.ID); err != nil {
		tx.Rollback()
		return nil, err
	}

	// commit the transaction
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// return the key
	return key, nil
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
		certKey       []byte
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
		var keyPEM bytes.Buffer
		if err := pem.Encode(&keyPEM, &pem.Block{Type: "PRIVATE KEY", Bytes: certKey}); err != nil {
			return nil, err
		}
		route.Certificate = &router.Certificate{
			ID:        *certID,
			Cert:      *certCert,
			Key:       keyPEM.String(),
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
	return r.list(r.db, parentRef, false)
}

func (r *RouteRepo) listForUpdate(db rowQueryer, parentRef string) ([]*router.Route, error) {
	return r.list(db, parentRef, true)
}

func (r *RouteRepo) list(db rowQueryer, parentRef string, forUpdate bool) ([]*router.Route, error) {
	httpRoutes, err := r.listHTTP(db, parentRef, forUpdate)
	if err != nil {
		return nil, err
	}
	tcpRoutes, err := r.listTCP(db, parentRef, forUpdate)
	if err != nil {
		return nil, err
	}
	return append(httpRoutes, tcpRoutes...), nil
}

func (r *RouteRepo) listHTTP(db rowQueryer, parentRef string, forUpdate bool) ([]*router.Route, error) {
	query := "http_route_list"
	var args []interface{}
	if forUpdate {
		query += "_for_update"
	}
	if parentRef != "" {
		query += "_by_parent_ref"
		args = append(args, parentRef)
	}
	rows, err := db.Query(query, args...)
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

func (r *RouteRepo) listTCP(db rowQueryer, parentRef string, forUpdate bool) ([]*router.Route, error) {
	query := "tcp_route_list"
	var args []interface{}
	if forUpdate {
		query += "_for_update"
	}
	if parentRef != "" {
		query += "_by_parent_ref"
		args = append(args, parentRef)
	}
	rows, err := db.Query(query, args...)
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
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	existingRoutes, err := r.listForUpdate(tx, "")
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := r.validate(route, existingRoutes, routeOpUpdate); err != nil {
		tx.Rollback()
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
		route.DrainBackends,
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
		route.DrainBackends,
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
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	existingRoutes, err := r.listForUpdate(tx, "")
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := r.validate(route, existingRoutes, routeOpDelete); err != nil {
		tx.Rollback()
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

// validateAPIRoutes checks that the given API routes are semantically valid
func validateAPIRoutes(appRoutes []*api.AppRoutes) error {
	for _, a := range appRoutes {
		if a.App == "" {
			return hh.ValidationErr("app", "must be set")
		}
		for _, route := range a.Routes {
			if route.ServiceTarget == nil {
				return hh.ValidationErr("service_target", "must be set")
			}
			switch config := route.Config.(type) {
			case *api.Route_Http:
				if config.Http == nil {
					return hh.ValidationErr("config.http", "must be set for HTTP routes")
				}
				if config.Http.Domain == "" {
					return hh.ValidationErr("config.http.domain", "must be set for HTTP routes")
				}
				// ensure HTTP routes have a normalised path
				config.Http.Path = normaliseRoutePath(config.Http.Path)
			case *api.Route_Tcp:
				if config.Tcp == nil {
					return hh.ValidationErr("config.tcp", "must be set for TCP routes")
				}
				if config.Tcp.Port == nil {
					return hh.ValidationErr("config.tcp.port", "must be set for TCP routes")
				}
			default:
				return hh.ValidationErr("config", "must be either HTTP or TCP")
			}
		}
	}
	return nil
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
	if cert != nil && len(cert.Cert) > 0 {
		cert.Cert = strings.Trim(cert.Cert, " \n")

		// if the certificate has an explicit key, then check that it
		// matches the certificate, otherwise check that the expected
		// key ID exists in the database
		if cert.Key != "" {
			cert.Key = strings.Trim(cert.Key, " \n")

			if _, err := tls.X509KeyPair([]byte(cert.Cert), []byte(cert.Key)); err != nil {
				return hh.ValidationErr("certificate", fmt.Sprintf("is invalid: %s", err))
			}
		} else {
			keyID := router.CertificateKeyID([]byte(cert.Cert))

			if _, err := scanKey(r.db.QueryRow("tls_key_select", keyID)); err != nil {
				return hh.ValidationErr("certificate", fmt.Sprintf("key not found: %s", keyID))
			}
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

// RouteState calculates the state of the given set of routes as the SHA256
// digest of the canonical JSON representation of a map of route IDs to routes
func RouteState(routes []*router.Route) []byte {
	v := make(map[string]*router.Route, len(routes))
	for _, r := range routes {
		v[r.ID] = r
	}
	data, _ := cjson.Marshal(v)
	state := sha256.Sum256(data)
	return state[:]
}

// routeUpdate is used to track existing routes that need to be updated along
// with their updated route
type routeUpdate struct {
	existingRoute *router.Route
	updatedRoute  *router.Route
}

// routesMatchForUpdate checks whether an existing route matches the given
// desired route and should thus be updated with it
func routesMatchForUpdate(existing *router.Route, desired *api.Route) bool {
	switch config := desired.Config.(type) {
	case *api.Route_Http:
		// HTTP routes should be updated with the desired route if they
		// have the same domain and path
		return config.Http.Domain == existing.Domain && config.Http.Path == existing.Path
	case *api.Route_Tcp:
		// TCP routes should be updated with the desired route if they
		// have the same port
		return int32(config.Tcp.Port.Port) == existing.Port
	default:
		return false
	}
}

// routesEqualForUpdate checks whether an existing route has the same
// configuration as a desired route that has been identified as being an update
// of the existing route
func routesEqualForUpdate(existing *router.Route, desired *api.Route) bool {
	// check HTTP routes for a change in certificate or stickiness
	if config, ok := desired.Config.(*api.Route_Http); ok {
		if config.Http.Tls == nil {
			if existing.Certificate != nil {
				return false
			}
		} else {
			if !certificatesEqual(existing.Certificate, config.Http.Tls.Certificate) {
				return false
			}
		}
		if existing.Sticky == (config.Http.StickySessions == nil) {
			return false
		}
	}

	// check general config is the same
	return existing.Service == desired.ServiceTarget.ServiceName &&
		existing.Leader == desired.ServiceTarget.Leader &&
		existing.DrainBackends == desired.ServiceTarget.DrainBackends &&
		existing.DisableKeepAlives == desired.DisableKeepAlives
}

func certificatesEqual(existing *router.Certificate, desired *api.Certificate) bool {
	if existing == nil {
		return desired == nil
	}
	if desired == nil {
		return existing == nil
	}

	pemData := []byte(existing.Cert)
	var existingChain [][]byte
	for {
		var block *pem.Block
		block, pemData = pem.Decode(pemData)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			existingChain = append(existingChain, block.Bytes)
		}
	}

	if len(desired.Chain) != len(existingChain) {
		return false
	}
	for i, desiredCert := range desired.Chain {
		if !bytes.Equal(desiredCert, existingChain[i]) {
			return false
		}
	}
	return true
}

// ToAPIRoute converts a router.Route to an api.Route
func ToAPIRoute(route *router.Route) *api.Route {
	r := &api.Route{
		ServiceTarget: &api.Route_ServiceTarget{
			ServiceName:   route.Service,
			Leader:        route.Leader,
			DrainBackends: route.DrainBackends,
		},
		DisableKeepAlives: route.DisableKeepAlives,
	}
	if route.ID != "" {
		r.Name = path.Join(
			strings.TrimPrefix(route.ParentRef, "controller/"),
			"routes", route.ID,
		)
	}
	switch route.Type {
	case "http":
		r.Config = &api.Route_Http{Http: &api.Route_HTTP{
			Domain: route.Domain,
			Path:   route.Path,
		}}
		if route.Certificate != nil {
			pemData := []byte(route.Certificate.Cert)
			var chain [][]byte
			for {
				var block *pem.Block
				block, pemData = pem.Decode(pemData)
				if block == nil {
					break
				}
				if block.Type == "CERTIFICATE" {
					chain = append(chain, block.Bytes)
				}
			}
			tls := &api.Route_TLS{
				Certificate: &api.Certificate{
					Chain: chain,
				},
			}
			r.Config.(*api.Route_Http).Http.Tls = tls
		}
		if route.Sticky {
			r.Config.(*api.Route_Http).Http.StickySessions = &api.Route_HTTP_StickySessions{}
		}
	case "tcp":
		r.Config = &api.Route_Tcp{Tcp: &api.Route_TCP{
			Port: &api.Route_TCPPort{Port: uint32(route.Port)},
		}}
	}
	return r
}

// ToRouterRoute converts an api.Route into a router.Route
func ToRouterRoute(appID string, route *api.Route) *router.Route {
	r := &router.Route{
		ParentRef:         ct.RouteParentRefPrefix + appID,
		DisableKeepAlives: route.DisableKeepAlives,
	}
	if t := route.ServiceTarget; t != nil {
		r.Service = t.ServiceName
		r.Leader = t.Leader
		r.DrainBackends = t.DrainBackends
	}
	switch config := route.Config.(type) {
	case *api.Route_Http:
		r.Type = "http"
		r.Domain = config.Http.Domain
		r.Path = config.Http.Path
		if tls := config.Http.Tls; tls != nil && tls.Certificate != nil {
			chain := tls.Certificate.Chain
			certsPEM := make([][]byte, len(chain))
			for i, certDER := range chain {
				certsPEM[i] = pem.EncodeToMemory(&pem.Block{
					Type:  "CERTIFICATE",
					Bytes: certDER,
				})
			}
			chainPEM := bytes.Join(certsPEM, []byte("\n"))
			r.Certificate = &router.Certificate{
				ID:   router.CertificateID(chainPEM),
				Cert: string(chainPEM),
			}
		}
		r.Sticky = config.Http.StickySessions != nil
	case *api.Route_Tcp:
		r.Type = "tcp"
		r.Port = int32(config.Tcp.Port.Port)
	}
	return r
}
