package data

import (
	"fmt"
	"strings"
	"time"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/jackc/pgx"
)

type FormationRepo struct {
	db        *postgres.DB
	apps      *AppRepo
	releases  *ReleaseRepo
	artifacts *ArtifactRepo
}

func NewFormationRepo(db *postgres.DB, appRepo *AppRepo, releaseRepo *ReleaseRepo, artifactRepo *ArtifactRepo) *FormationRepo {
	r := &FormationRepo{
		db:        db,
		apps:      appRepo,
		releases:  releaseRepo,
		artifacts: artifactRepo,
	}
	releaseRepo.formations = r
	return r
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

func (r *FormationRepo) AddScaleRequest(req *ct.ScaleRequest, deleteFormation bool) (*ct.ScaleRequest, error) {
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

	// cancel any current scale requests for the same formation (and return them
	// so we can emit events for them below)
	var canceledScaleRequests []*ct.ScaleRequest
	rows, err := tx.Query("scale_request_cancel", req.AppID, req.ReleaseID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		scale, err := scanScaleRequest(rows)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
		canceledScaleRequests = append(canceledScaleRequests, scale)
	}
	if err := rows.Err(); err != nil {
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
	var deploymentID *string
	if req.DeploymentID != "" {
		deploymentID = &req.DeploymentID
	}
	err = tx.QueryRow(
		"scale_request_insert",
		req.ID,
		req.AppID,
		req.ReleaseID,
		deploymentID,
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
	if err := CreateEvent(tx.Exec, &ct.Event{
		AppID:        req.AppID,
		DeploymentID: req.DeploymentID,
		ObjectID:     req.ID,
		ObjectType:   ct.EventTypeScaleRequest,
		Op:           ct.EventOpCreate,
	}, req); err != nil {
		tx.Rollback()
		return nil, err
	}
	// emit a scale request event for each one we canceled
	for _, s := range canceledScaleRequests {
		if err := CreateEvent(tx.Exec, &ct.Event{
			AppID:        s.AppID,
			DeploymentID: req.DeploymentID,
			ObjectID:     s.ID,
			ObjectType:   ct.EventTypeScaleRequestCancelation,
			Op:           ct.EventOpUpdate,
		}, s); err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	// emit a deprecated scale event for old clients
	if req.NewProcesses != nil {
		deprecatedScale := &ct.DeprecatedScale{
			Processes:     *req.NewProcesses,
			PrevProcesses: req.OldProcesses,
			ReleaseID:     req.ReleaseID,
		}
		if err := CreateEvent(tx.Exec, &ct.Event{
			AppID:      req.AppID,
			ObjectID:   req.AppID + ":" + req.ReleaseID,
			ObjectType: ct.EventTypeDeprecatedScale,
		}, deprecatedScale); err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	return req, tx.Commit()
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
	return r.TxGet(r.db, appID, releaseID)
}

func (r *FormationRepo) TxGet(tx rowQueryer, appID, releaseID string) (*ct.Formation, error) {
	row := tx.QueryRow("formation_select", appID, releaseID)
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
	defer rows.Close()
	return scanFormations(rows)
}

func (r *FormationRepo) ListActive() ([]*ct.ExpandedFormation, error) {
	rows, err := r.db.Query("formation_list_active")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.listExpanded(rows)
}

func (r *FormationRepo) ListSince(since time.Time) ([]*ct.ExpandedFormation, error) {
	rows, err := r.db.Query("formation_list_since", since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.listExpanded(rows)
}

func (r *FormationRepo) listExpanded(rows *pgx.Rows) ([]*ct.ExpandedFormation, error) {
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
	var deploymentID *string
	if err := tx.QueryRow("scale_request_update", req.ID, string(req.State)).Scan(&req.UpdatedAt, &deploymentID); err != nil {
		tx.Rollback()
		return err
	}
	if deploymentID != nil {
		req.DeploymentID = *deploymentID
	}
	if err := CreateEvent(tx.Exec, &ct.Event{
		AppID:        req.AppID,
		DeploymentID: req.DeploymentID,
		ObjectID:     req.ID,
		ObjectType:   ct.EventTypeScaleRequest,
		Op:           ct.EventOpUpdate,
	}, req); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

type ListScaleRequestOptions struct {
	PageToken    PageToken
	AppIDs       []string
	ReleaseIDs   []string
	ScaleIDs     []string
	StateFilters []ct.ScaleRequestState
}

func (r *FormationRepo) ListScaleRequests(opts ListScaleRequestOptions) ([]*ct.ScaleRequest, *PageToken, error) {
	pageSize := DEFAULT_PAGE_SIZE
	if opts.PageToken.Size > 0 {
		pageSize = opts.PageToken.Size
	}
	stateFilters := make([]string, 0, len(opts.StateFilters))
	for _, state := range opts.StateFilters {
		stateFilters = append(stateFilters, string(state))
	}
	cursor, err := opts.PageToken.Cursor()
	if err != nil {
		return nil, nil, err
	}
	rows, err := r.db.Query("scale_request_list", opts.AppIDs, opts.ReleaseIDs, opts.ScaleIDs, stateFilters, cursor, pageSize+1)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var scales []*ct.ScaleRequest
	for rows.Next() {
		scale, err := scanScaleRequest(rows)
		if err != nil {
			return nil, nil, err
		}
		scales = append(scales, scale)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var nextPageToken *PageToken
	if len(scales) == pageSize+1 {
		nextPageToken = &PageToken{
			CursorID: toCursorID(scales[pageSize].CreatedAt),
			Size:     pageSize,
		}
		scales = scales[0:pageSize]
	}
	return scales, nextPageToken, nil
}

func scanScaleRequest(s postgres.Scanner) (*ct.ScaleRequest, error) {
	sr := &ct.ScaleRequest{}
	err := s.Scan(&sr.ID, &sr.AppID, &sr.ReleaseID, &sr.State, &sr.OldProcesses, &sr.NewProcesses, &sr.OldTags, &sr.NewTags, &sr.CreatedAt, &sr.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	return sr, nil
}
