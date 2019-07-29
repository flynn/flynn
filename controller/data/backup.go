package data

import (
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/jackc/pgx"
)

type BackupRepo struct {
	db *postgres.DB
}

func NewBackupRepo(db *postgres.DB) *BackupRepo {
	return &BackupRepo{db: db}
}

func (r *BackupRepo) GetLatest() (*ct.ClusterBackup, error) {
	b := &ct.ClusterBackup{}
	if err := r.db.QueryRow("backup_select_latest").Scan(&b.ID, &b.Status, &b.SHA512, &b.Size, &b.Error, &b.CreatedAt, &b.UpdatedAt, &b.CompletedAt); err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	return b, nil
}

func (r *BackupRepo) Add(b *ct.ClusterBackup) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	if err := tx.QueryRow("backup_insert", b.Status, b.SHA512, b.Size, b.Error, b.CompletedAt).Scan(&b.ID, &b.CreatedAt, &b.UpdatedAt); err != nil {
		tx.Rollback()
		return err
	}
	if err := CreateEvent(tx.Exec, &ct.Event{
		ObjectID:   b.ID,
		ObjectType: ct.EventTypeClusterBackup,
	}, b); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *BackupRepo) Update(b *ct.ClusterBackup) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	if err := tx.QueryRow("backup_update", b.ID, b.Status, b.SHA512, b.Size, b.Error, b.CompletedAt).Scan(&b.UpdatedAt); err != nil {
		tx.Rollback()
		return err
	}
	if err := CreateEvent(tx.Exec, &ct.Event{
		ObjectID:   b.ID,
		ObjectType: ct.EventTypeClusterBackup,
	}, b); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
