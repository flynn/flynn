package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/que-go"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/tlscert"
)

type DomainMigrationRepo struct {
	db *postgres.DB
}

func NewDomainMigrationRepo(db *postgres.DB) *DomainMigrationRepo {
	return &DomainMigrationRepo{db: db}
}

func (repo *DomainMigrationRepo) Add(dm *ct.DomainMigration) error {
	tx, err := repo.db.Begin()
	if err != nil {
		return err
	}
	if err := tx.QueryRow("domain_migration_insert", dm.OldDomain, dm.Domain, dm.OldTLSCert, dm.TLSCert).Scan(&dm.ID, &dm.CreatedAt); err != nil {
		tx.Rollback()
		return err
	}
	if err := createEvent(tx.Exec, &ct.Event{
		ObjectID:   dm.ID,
		ObjectType: ct.EventTypeDomainMigration,
	}, ct.DomainMigrationEvent{
		DomainMigration: dm,
	}); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (c *controllerAPI) MigrateDomain(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	var dm *ct.DomainMigration
	if err := json.NewDecoder(req.Body).Decode(&dm); err != nil {
		respondWithError(w, err)
		return
	}
	defaultRouteDomain := os.Getenv("DEFAULT_ROUTE_DOMAIN")
	if dm.OldDomain != defaultRouteDomain {
		respondWithError(w, ct.ValidationError{
			Message: fmt.Sprintf(`Can't migrate from "%s" when currently using "%s"`, dm.OldDomain, defaultRouteDomain),
		})
		return
	}

	app, err := c.appRepo.Get("router")
	if err != nil {
		respondWithError(w, err)
		return
	}
	release, err := c.appRepo.GetRelease(app.(*ct.App).ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	dm.OldTLSCert = &tlscert.Cert{
		Cert:       release.Env["TLSCERT"],
		PrivateKey: release.Env["TLSKEY"],
	}

	if err := c.domainMigrationRepo.Add(dm); err != nil {
		respondWithError(w, err)
		return
	}

	args, err := json.Marshal(dm)
	if err != nil {
		respondWithError(w, err)
		return
	}

	if err := c.que.Enqueue(&que.Job{
		Type: "domain_migration",
		Args: args,
	}); err != nil {
		respondWithError(w, err)
		return
	}

	httphelper.JSON(w, 200, &dm)
}
