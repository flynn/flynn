package common

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	routerc "github.com/flynn/flynn/router/client"
	router "github.com/flynn/flynn/router/types"
	"github.com/jackc/pgx"
)

var ErrNotFound = errors.New("controller: resource not found")

var IDPattern = regexp.MustCompile(`^[a-f0-9]{8}-?([a-f0-9]{4}-?){3}[a-f0-9]{12}$`)

func CreateEvent(dbExec func(string, ...interface{}) error, e *ct.Event, data interface{}) error {
	args := []interface{}{e.ObjectID, string(e.ObjectType), data}
	fields := []string{"object_id", "object_type", "data"}
	if e.AppID != "" {
		fields = append(fields, "app_id")
		args = append(args, e.AppID)
	}
	if e.UniqueID != "" {
		fields = append(fields, "unique_id")
		args = append(args, e.UniqueID)
	}
	query := "INSERT INTO events ("
	for i, n := range fields {
		if i > 0 {
			query += ","
		}
		query += n
	}
	query += ") VALUES ("
	for i := range fields {
		if i > 0 {
			query += ","
		}
		query += fmt.Sprintf("$%d", i+1)
	}
	query += ")"
	return dbExec(query, args...)
}

func RouteParentRef(appID string) string {
	return ct.RouteParentRefPrefix + appID
}

func CreateRoute(db *postgres.DB, rc routerc.Client, appID string, route *router.Route) error {
	route.ParentRef = RouteParentRef(appID)
	if err := schema.Validate(route); err != nil {
		return err
	}
	return rc.CreateRoute(route)
}

func ScanRelease(s postgres.Scanner) (*ct.Release, error) {
	var artifactIDs string
	release := &ct.Release{}
	err := s.Scan(&release.ID, &release.AppID, &artifactIDs, &release.Env, &release.Processes, &release.Meta, &release.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	if artifactIDs != "" {
		release.ArtifactIDs = Split(artifactIDs[1:len(artifactIDs)-1], ",")
	}
	if len(release.ArtifactIDs) > 0 {
		release.LegacyArtifactID = release.ArtifactIDs[0]
	}
	return release, err
}

func Split(s string, sep string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
