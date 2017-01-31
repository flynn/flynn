package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/jackc/pgx"
	"golang.org/x/net/context"
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

	if err := createEvent(tx.Exec, &ct.Event{
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

	if err := createEvent(tx.Exec, &ct.Event{
		AppID:      appID,
		ObjectID:   vol.ID,
		ObjectType: ct.EventTypeVolume,
	}, vol); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (c *controllerAPI) GetVolumes(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	if strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		c.streamVolumes(ctx, w, req)
		return
	}

	list, err := c.volumeRepo.List()
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, list)
}

func (c *controllerAPI) GetAppVolumes(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	list, err := c.volumeRepo.AppList(c.getApp(ctx).ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, list)
}

func (c *controllerAPI) GetVolume(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)
	volume, err := c.volumeRepo.Get(c.getApp(ctx).ID, params.ByName("volume_id"))
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

	if err := schema.Validate(volume); err != nil {
		respondWithError(w, err)
		return
	}

	if err := c.volumeRepo.Add(&volume); err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, &volume)
}

func (c *controllerAPI) DecommissionVolume(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	var volume ct.Volume
	if err := httphelper.DecodeJSON(req, &volume); err != nil {
		respondWithError(w, err)
		return
	}
	if err := c.volumeRepo.Decommission(c.getApp(ctx).ID, &volume); err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, &volume)
}

func (c *controllerAPI) streamVolumes(ctx context.Context, w http.ResponseWriter, req *http.Request) (err error) {
	l, _ := ctxhelper.LoggerFromContext(ctx)
	ch := make(chan *ct.Volume)
	stream := sse.NewStream(w, ch, l)
	stream.Serve()
	defer func() {
		if err == nil {
			stream.Close()
		} else {
			stream.CloseWithError(err)
		}
	}()

	since, err := time.Parse(time.RFC3339Nano, req.FormValue("since"))
	if err != nil {
		return err
	}

	eventListener, err := c.maybeStartEventListener()
	if err != nil {
		l.Error("error starting event listener")
		return err
	}

	sub, err := eventListener.Subscribe("", []string{string(ct.EventTypeVolume)}, "")
	if err != nil {
		return err
	}
	defer sub.Close()

	vols, err := c.volumeRepo.ListSince(since)
	if err != nil {
		return err
	}
	currentUpdatedAt := since
	for _, vol := range vols {
		select {
		case <-stream.Done:
			return nil
		case ch <- vol:
			if vol.UpdatedAt.After(currentUpdatedAt) {
				currentUpdatedAt = *vol.UpdatedAt
			}
		}
	}

	select {
	case <-stream.Done:
		return nil
	case ch <- &ct.Volume{}:
	}

	for {
		select {
		case <-stream.Done:
			return
		case event, ok := <-sub.Events:
			if !ok {
				return sub.Err
			}
			var vol ct.Volume
			if err := json.Unmarshal(event.Data, &vol); err != nil {
				l.Error("error deserializing volume event", "event.id", event.ID, "err", err)
				continue
			}
			if vol.UpdatedAt.Before(currentUpdatedAt) {
				continue
			}
			select {
			case <-stream.Done:
				return nil
			case ch <- &vol:
			}
		}
	}
}
