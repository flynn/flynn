package data

import (
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
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
	if err := CreateEvent(tx.Exec, &ct.Event{
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
