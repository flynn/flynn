package main

import (
	"net/http"

	"github.com/jackc/pgx"
	"golang.org/x/net/context"
	//"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
)

type VolumeRepo struct {
	db *postgres.DB
}

func NewVolumeRepo(db *postgres.DB) *VolumeRepo {
	return &VolumeRepo{db}
}

func (r *VolumeRepo) Get(id string) (*ct.Volume, error) {
	if !idPattern.MatchString(id) {
		var err error
		id, err = cluster.ExtractUUID(id)
		if err != nil {
			return nil, ErrNotFound
		}
	}
	row := r.db.QueryRow("volume_select", id)
	return scanVolume(row)
}

func (r *VolumeRepo) Add(volume *ct.Volume) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	err = tx.QueryRow(
		"volume_insert",
		volume.ID,
		volume.HostID,
	).Scan(&volume.CreatedAt, &volume.UpdatedAt)
	if postgres.IsUniquenessError(err, "") {
		err = tx.QueryRow(
			"volume_update",
			volume.ID,
			volume.HostID,
		).Scan(&volume.CreatedAt, &volume.UpdatedAt)
		if postgres.IsPostgresCode(err, postgres.CheckViolation) {
			return ct.ValidationError{Field: "state", Message: err.Error()}
		}
	}
	if err != nil {
		tx.Rollback()
		return err
	}
	// Now create any missing attachments
	for _, attachment := range volume.Attachments {
		err = tx.Exec(
			"volume_attachment_insert",
			volume.ID,
			attachment.JobID,
			attachment.Target,
			attachment.Writeable,
		)
		if postgres.IsUniquenessError(err, "") {
			err = tx.Exec(
				"volume_attachment_update",
				volume.ID,
				attachment.JobID,
				attachment.Target,
				attachment.Writeable,
			)
			if err != nil {
				tx.Rollback()
				return err
			}
		}
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func scanVolume(s postgres.Scanner) (*ct.Volume, error) {
	volume := &ct.Volume{}
	err := s.Scan(
		&volume.ID,
		&volume.HostID,
		&volume.CreatedAt,
		&volume.UpdatedAt,
		&volume.Attachments,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	return volume, nil
}

func (r *VolumeRepo) List() ([]*ct.Volume, error) {
	rows, err := r.db.Query("volume_list")
	if err != nil {
		return nil, err
	}
	volumes := []*ct.Volume{}
	for rows.Next() {
		volume, err := scanVolume(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		volumes = append(volumes, volume)
	}
	return volumes, nil
}

func (c *controllerAPI) ListVolumes(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	list, err := c.volumeRepo.List()
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, list)
}

func (c *controllerAPI) GetVolume(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	volume, err := c.volumeRepo.Get(params.ByName("volume_id"))
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, volume)
}

func (c *controllerAPI) PutVolume(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	var volume ct.Volume
	if err := httphelper.DecodeJSON(req, &volume); err != nil {
		respondWithError(w, err)
		return
	}

	// TODO(jpg) Add volume etc to schema
	//if err := schema.Validate(volume); err != nil {
	//	respondWithError(w, err)
	//	return
	//}

	if err := c.volumeRepo.Add(&volume); err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, &volume)
}
