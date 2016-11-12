package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/jackc/pgx"
	"golang.org/x/net/context"
)

type FormationRepo struct {
	db        *postgres.DB
	apps      *AppRepo
	releases  *ReleaseRepo
	artifacts *ArtifactRepo
}

func NewFormationRepo(db *postgres.DB, appRepo *AppRepo, releaseRepo *ReleaseRepo, artifactRepo *ArtifactRepo) *FormationRepo {
	return &FormationRepo{
		db:        db,
		apps:      appRepo,
		releases:  releaseRepo,
		artifacts: artifactRepo,
	}
}

func (r *FormationRepo) validateProcesses(req *ct.ScaleRequest) error {
	if req.NewProcesses == nil {
		return nil
	}
	data, err := r.releases.Get(req.ReleaseID)
	if err != nil {
		return err
	}
	release := data.(*ct.Release)
	invalid := make([]string, 0, len(*req.NewProcesses))
	for typ := range *req.NewProcesses {
		if _, ok := release.Processes[typ]; !ok {
			invalid = append(invalid, typ)
		}
	}
	if len(invalid) > 0 {
		return ct.ValidationError{Message: fmt.Sprintf("requested scale includes process types that do not exist in the release: %s", strings.Join(invalid, ", "))}
	}
	return nil
}

func (r *FormationRepo) AddScaleRequest(req *ct.ScaleRequest, deleteFormation bool) (*ct.Formation, error) {
	if req.NewProcesses == nil && req.NewTags == nil {
		return nil, ct.ValidationError{Message: "scale request must have either processes or tags set"}
	}

	if err := r.validateProcesses(req); err != nil {
		return nil, err
	}

	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}

	// get the current formation so we can add the current processes and
	// tags to the scale request
	formation, err := scanFormation(tx.QueryRow("formation_select", req.AppID, req.ReleaseID))
	if err == ErrNotFound {
		formation = &ct.Formation{
			AppID:     req.AppID,
			ReleaseID: req.ReleaseID,
		}
	} else if err != nil {
		tx.Rollback()
		return nil, err
	}

	// cancel any current scale requests for the same formation
	if err := tx.Exec("scale_request_cancel", req.AppID, req.ReleaseID); err != nil {
		tx.Rollback()
		return nil, err
	}

	req.ID = random.UUID()
	req.State = ct.ScaleRequestStatePending

	// copy the formation's current processes and tags as we may modify
	// them later on
	req.OldProcesses = make(map[string]int, len(formation.Processes))
	for typ, count := range formation.Processes {
		req.OldProcesses[typ] = count
	}
	req.OldTags = make(map[string]map[string]string, len(formation.Tags))
	for typ, tags := range formation.Tags {
		req.OldTags[typ] = tags
	}

	// if the request has no new processes / tags, keep them the same,
	// otherwise modify the formation's processes / tags accordingly
	if req.NewProcesses == nil {
		req.NewProcesses = &formation.Processes
	} else {
		if formation.Processes == nil {
			formation.Processes = make(map[string]int, len(*req.NewProcesses))
		}
		for typ, count := range *req.NewProcesses {
			formation.Processes[typ] = count
		}
	}
	if req.NewTags == nil {
		req.NewTags = &formation.Tags
	} else {
		for typ, tags := range *req.NewTags {
			if formation.Tags == nil {
				formation.Tags = make(map[string]map[string]string, len(*req.NewTags))
			}
			formation.Tags[typ] = tags
		}
	}

	// create the scale request and either insert or delete the formation
	err = tx.QueryRow(
		"scale_request_insert",
		req.ID,
		req.AppID,
		req.ReleaseID,
		string(req.State),
		req.OldProcesses,
		req.NewProcesses,
		req.OldTags,
		req.NewTags,
	).Scan(&req.CreatedAt, &req.UpdatedAt)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if deleteFormation {
		err = tx.Exec("formation_delete", formation.AppID, formation.ReleaseID)
	} else {
		err = tx.QueryRow(
			"formation_insert",
			formation.AppID,
			formation.ReleaseID,
			formation.Processes,
			formation.Tags,
		).Scan(&formation.CreatedAt, &formation.UpdatedAt)
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// emit a scale request event so clients know scaling has begun
	if err := createEvent(tx.Exec, &ct.Event{
		AppID:      req.AppID,
		ObjectID:   req.ID,
		ObjectType: ct.EventTypeScaleRequest,
	}, req); err != nil {
		tx.Rollback()
		return nil, err
	}

	// emit a deprecated scale event for old clients
	if req.NewProcesses != nil {
		deprecatedScale := &ct.DeprecatedScale{
			Processes:     *req.NewProcesses,
			PrevProcesses: req.OldProcesses,
			ReleaseID:     req.ReleaseID,
		}
		if err := createEvent(tx.Exec, &ct.Event{
			AppID:      req.AppID,
			ObjectID:   req.AppID + ":" + req.ReleaseID,
			ObjectType: ct.EventTypeDeprecatedScale,
		}, deprecatedScale); err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	return formation, tx.Commit()
}

func scanFormations(rows *pgx.Rows) ([]*ct.Formation, error) {
	var formations []*ct.Formation
	for rows.Next() {
		formation, err := scanFormation(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		formations = append(formations, formation)
	}
	return formations, rows.Err()
}

func scanFormation(s postgres.Scanner) (*ct.Formation, error) {
	f := &ct.Formation{}
	err := s.Scan(&f.AppID, &f.ReleaseID, &f.Processes, &f.Tags, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	return f, nil
}

func scanExpandedFormation(s postgres.Scanner) (*ct.ExpandedFormation, error) {
	f := &ct.ExpandedFormation{
		App:     &ct.App{},
		Release: &ct.Release{},
	}
	var artifactIDs string
	var appReleaseID *string
	var req ct.ScaleRequest
	var reqID *string
	err := s.Scan(
		&f.App.ID,
		&f.App.Name,
		&f.App.Meta,
		&f.App.Strategy,
		&appReleaseID,
		&f.App.DeployTimeout,
		&f.App.CreatedAt,
		&f.App.UpdatedAt,
		&f.Release.ID,
		&artifactIDs,
		&f.Release.Meta,
		&f.Release.Env,
		&f.Release.Processes,
		&f.Release.CreatedAt,
		&reqID,
		&req.OldProcesses,
		&req.NewProcesses,
		&req.OldTags,
		&req.NewTags,
		&req.CreatedAt,
		&f.Processes,
		&f.Tags,
		&f.UpdatedAt,
		&f.Deleted,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	if reqID != nil {
		req.ID = *reqID
		req.AppID = f.App.ID
		req.ReleaseID = f.Release.ID
		req.State = ct.ScaleRequestStatePending
		f.PendingScaleRequest = &req
	}
	if appReleaseID != nil {
		f.App.ReleaseID = *appReleaseID
	}
	if f.App.Meta == nil {
		// ensure we don't return `{"meta": null}`
		f.App.Meta = make(map[string]string)
	}
	if artifactIDs != "" {
		f.Release.ArtifactIDs = split(artifactIDs[1:len(artifactIDs)-1], ",")
		if len(f.Release.ArtifactIDs) > 0 {
			f.Release.LegacyArtifactID = f.Release.ArtifactIDs[0]
		}
	}
	f.Release.AppID = f.App.ID
	return f, nil
}

func populateFormationArtifacts(ef *ct.ExpandedFormation, artifacts map[string]*ct.Artifact) {
	ef.Artifacts = make([]*ct.Artifact, len(ef.Release.ArtifactIDs))
	for i, id := range ef.Release.ArtifactIDs {
		ef.Artifacts[i] = artifacts[id]
	}
}

func (r *FormationRepo) Get(appID, releaseID string) (*ct.Formation, error) {
	row := r.db.QueryRow("formation_select", appID, releaseID)
	return scanFormation(row)
}

func (r *FormationRepo) GetExpanded(appID, releaseID string, includeDeleted bool) (*ct.ExpandedFormation, error) {
	row := r.db.QueryRow("formation_select_expanded", appID, releaseID)
	ef, err := scanExpandedFormation(row)
	if err != nil {
		return nil, err
	}
	if !includeDeleted && ef.Deleted {
		return nil, ErrNotFound
	}
	artifacts, err := r.artifacts.ListIDs(ef.Release.ArtifactIDs...)
	if err != nil {
		return nil, err
	}
	populateFormationArtifacts(ef, artifacts)
	return ef, nil
}

func (r *FormationRepo) List(appID string) ([]*ct.Formation, error) {
	rows, err := r.db.Query("formation_list_by_app", appID)
	if err != nil {
		return nil, err
	}
	return scanFormations(rows)
}

func (r *FormationRepo) ListActive() ([]*ct.ExpandedFormation, error) {
	rows, err := r.db.Query("formation_list_active")
	if err != nil {
		return nil, err
	}
	return r.listExpanded(rows)
}

func (r *FormationRepo) ListSince(since time.Time) ([]*ct.ExpandedFormation, error) {
	rows, err := r.db.Query("formation_list_since", since)
	if err != nil {
		return nil, err
	}
	return r.listExpanded(rows)
}

func (r *FormationRepo) listExpanded(rows *pgx.Rows) ([]*ct.ExpandedFormation, error) {
	defer rows.Close()

	var formations []*ct.ExpandedFormation

	// artifactIDs is a list of artifact IDs related to the formation list
	// and is used to populate the formation's artifact fields using a
	// subsequent artifact list query
	artifactIDs := make(map[string]struct{})

	for rows.Next() {
		formation, err := scanExpandedFormation(rows)
		if err != nil {
			return nil, err
		}
		formations = append(formations, formation)

		for _, id := range formation.Release.ArtifactIDs {
			artifactIDs[id] = struct{}{}
		}
	}

	if len(artifactIDs) > 0 {
		ids := make([]string, 0, len(artifactIDs))
		for id := range artifactIDs {
			ids = append(ids, id)
		}
		artifacts, err := r.artifacts.ListIDs(ids...)
		if err != nil {
			return nil, err
		}
		for _, formation := range formations {
			populateFormationArtifacts(formation, artifacts)
		}
	}

	return formations, rows.Err()
}

func (r *FormationRepo) UpdateScaleRequest(req *ct.ScaleRequest) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	if err := tx.Exec("scale_request_update", req.ID, string(req.State)); err != nil {
		tx.Rollback()
		return err
	}
	if err := createEvent(tx.Exec, &ct.Event{
		AppID:      req.AppID,
		ObjectID:   req.ID,
		ObjectType: ct.EventTypeScaleRequest,
	}, req); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func newScaleRequest(f *ct.Formation, release *ct.Release) *ct.ScaleRequest {
	// treat nil processes as a request to scale everything down
	if f.Processes == nil {
		f.Processes = make(map[string]int, len(release.Processes))
		for typ := range release.Processes {
			f.Processes[typ] = 0
		}
	}
	return &ct.ScaleRequest{
		AppID:        f.AppID,
		ReleaseID:    f.ReleaseID,
		NewProcesses: &f.Processes,
		NewTags:      &f.Tags,
	}
}

func (c *controllerAPI) PutFormation(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	app := c.getApp(ctx)
	release, err := c.getRelease(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}

	formation := &ct.Formation{}
	if err = httphelper.DecodeJSON(r, formation); err != nil {
		respondWithError(w, err)
		return
	}

	if len(release.ArtifactIDs) == 0 {
		respondWithError(w, ct.ValidationError{Message: "release is not deployable"})
		return
	}

	formation.AppID = app.ID
	formation.ReleaseID = release.ID

	if err = schema.Validate(formation); err != nil {
		respondWithError(w, err)
		return
	}

	req := newScaleRequest(formation, release)
	formation, err = c.formationRepo.AddScaleRequest(req, false)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, formation)
}

func (c *controllerAPI) PutScaleRequest(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	app := c.getApp(ctx)
	release, err := c.getRelease(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}

	var req ct.ScaleRequest
	if err := httphelper.DecodeJSON(r, &req); err != nil {
		respondWithError(w, err)
		return
	}
	req.AppID = app.ID
	req.ReleaseID = release.ID

	if err := schema.Validate(req); err != nil {
		respondWithError(w, err)
		return
	}

	if req.State == ct.ScaleRequestStatePending {
		_, err = c.formationRepo.AddScaleRequest(&req, false)
	} else {
		err = c.formationRepo.UpdateScaleRequest(&req)
	}
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, &req)
}

func (c *controllerAPI) GetFormation(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)

	app := c.getApp(ctx)
	releaseID := params.ByName("releases_id")
	if req.URL.Query().Get("expand") == "true" {
		formation, err := c.formationRepo.GetExpanded(app.ID, releaseID, false)
		if err != nil {
			respondWithError(w, err)
			return
		}
		httphelper.JSON(w, 200, formation)
		return
	}

	formation, err := c.formationRepo.Get(app.ID, releaseID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, formation)
}

func (c *controllerAPI) DeleteFormation(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	app := c.getApp(ctx)
	release, err := c.getRelease(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}
	formation, err := c.formationRepo.Get(app.ID, release.ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	formation.Processes = nil
	req := newScaleRequest(formation, release)
	if _, err := c.formationRepo.AddScaleRequest(req, true); err != nil {
		respondWithError(w, err)
		return
	}
	w.WriteHeader(200)
}

func (c *controllerAPI) ListFormations(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	app := c.getApp(ctx)
	list, err := c.formationRepo.List(app.ID)
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, list)
}

func (c *controllerAPI) GetFormations(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	if strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		c.streamFormations(ctx, w, req)
		return
	}

	if req.URL.Query().Get("active") == "true" {
		list, err := c.formationRepo.ListActive()
		if err != nil {
			respondWithError(w, err)
			return
		}
		httphelper.JSON(w, 200, list)
		return
	}

	// don't return a list of all formations, there will be lots of them
	// and no components currently need such a list
	httphelper.ValidationError(w, "", "must either request a stream or only active formations")
}

func (c *controllerAPI) streamFormations(ctx context.Context, w http.ResponseWriter, req *http.Request) (err error) {
	l, _ := ctxhelper.LoggerFromContext(ctx)
	ch := make(chan *ct.ExpandedFormation)
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
		l.Error("error starting event listener", "err", err)
		return err
	}

	sub, err := eventListener.Subscribe("", []string{string(ct.EventTypeScaleRequest)}, "")
	if err != nil {
		return err
	}
	defer sub.Close()

	formations, err := c.formationRepo.ListSince(since)
	if err != nil {
		return err
	}
	currentUpdatedAt := since
	for _, formation := range formations {
		select {
		case <-stream.Done:
			return nil
		case ch <- formation:
			if formation.UpdatedAt.After(currentUpdatedAt) {
				currentUpdatedAt = formation.UpdatedAt
			}
		}
	}

	select {
	case <-stream.Done:
		return nil
	case ch <- &ct.ExpandedFormation{}:
	}

	for {
		select {
		case <-stream.Done:
			return
		case event, ok := <-sub.Events:
			if !ok {
				return sub.Err
			}
			var req ct.ScaleRequest
			if err := json.Unmarshal(event.Data, &req); err != nil {
				l.Error("error deserializing scale event", "event.id", event.ID, "err", err)
				continue
			}
			formation, err := c.formationRepo.GetExpanded(req.AppID, req.ReleaseID, true)
			if err != nil {
				l.Error("error expanding formation", "app.id", req.AppID, "release.id", req.ReleaseID, "err", err)
				continue
			}
			if formation.UpdatedAt.Before(currentUpdatedAt) {
				continue
			}
			select {
			case <-stream.Done:
				return nil
			case ch <- formation:
			}
		}
	}
}
