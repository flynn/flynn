package data

import (
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/jackc/pgx"
)

type VolumeRepo struct {
	db *postgres.DB
}

func NewVolumeRepo(db *postgres.DB) *VolumeRepo {
	return &VolumeRepo{db}
}

func (r *VolumeRepo) Get(appID, volID string) (*ct.Volume, error) {
	row := r.db.QueryRow("volume_select", appID, volID)
	return scanVolume(row)
}

func (r *VolumeRepo) Add(vol *ct.Volume) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}

	err = tx.QueryRow("volume_insert",
		vol.ID,
		vol.HostID,
		string(vol.Type),
		string(vol.State),
		vol.AppID,
		vol.ReleaseID,
		vol.JobID,
		vol.JobType,
		vol.Path,
		vol.DeleteOnStop,
		vol.Meta,
	).Scan(&vol.CreatedAt, &vol.UpdatedAt)
	if err != nil {
		tx.Rollback()
		return err
	}

	if err := CreateEvent(tx.Exec, &ct.Event{
		AppID:      vol.AppID,
		ObjectID:   vol.ID,
		ObjectType: ct.EventTypeVolume,
	}, vol); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func scanVolume(s postgres.Scanner) (*ct.Volume, error) {
	vol := &ct.Volume{}
	var typ, state string
	err := s.Scan(
		&vol.ID,
		&vol.HostID,
		&typ,
		&state,
		&vol.AppID,
		&vol.ReleaseID,
		&vol.JobID,
		&vol.JobType,
		&vol.Path,
		&vol.DeleteOnStop,
		&vol.Meta,
		&vol.CreatedAt,
		&vol.UpdatedAt,
		&vol.DecommissionedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	vol.Type = volume.VolumeType(typ)
	vol.State = ct.VolumeState(state)
	return vol, nil
}

func (r *VolumeRepo) List() ([]*ct.Volume, error) {
	rows, err := r.db.Query("volume_list")
	if err != nil {
		return nil, err
	}
	return scanVolumes(rows)
}

func (r *VolumeRepo) AppList(appID string) ([]*ct.Volume, error) {
	rows, err := r.db.Query("volume_app_list", appID)
	if err != nil {
		return nil, err
	}
	return scanVolumes(rows)
}

func (r *VolumeRepo) ListSince(since time.Time) ([]*ct.Volume, error) {
	rows, err := r.db.Query("volume_list_since", since)
	if err != nil {
		return nil, err
	}
	return scanVolumes(rows)
}

func scanVolumes(rows *pgx.Rows) ([]*ct.Volume, error) {
	var volumes []*ct.Volume
	for rows.Next() {
		volume, err := scanVolume(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		volumes = append(volumes, volume)
	}
	return volumes, rows.Err()
}

func (r *VolumeRepo) Decommission(appID string, vol *ct.Volume) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}

	err = tx.QueryRow("volume_decommission", appID, vol.ID).Scan(&vol.UpdatedAt, &vol.DecommissionedAt)
	if err != nil {
		tx.Rollback()
		return err
	}

	if err := CreateEvent(tx.Exec, &ct.Event{
		AppID:      appID,
		ObjectID:   vol.ID,
		ObjectType: ct.EventTypeVolume,
	}, vol); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}
