package data

import (
	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	routerc "github.com/flynn/flynn/router/client"
	"github.com/flynn/flynn/router/types"
)

func routeParentRef(appID string) string {
	return ct.RouteParentRefPrefix + appID
}

func CreateRoute(db *postgres.DB, rc routerc.Client, appID string, route *router.Route) error {
	route.ParentRef = routeParentRef(appID)
	if err := schema.Validate(route); err != nil {
		return err
	}
	return rc.CreateRoute(route)
}
