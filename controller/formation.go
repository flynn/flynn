package main

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/controller/schema"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/jackc/pgx"
	"golang.org/x/net/context"
)

// we are wrapping the client specified channel to send formation updates in order to safely interrupt
// `sendUpdateSince` goroutine if an unsubscribe happens before it is completed. otherwise we can get
// a panic due to sending to a closed channel. See https://github.com/flynn/flynn/issues/2175 for more
// details on this subject.
type FormationSubscription struct {
	mtx sync.RWMutex
	ch  chan *ct.ExpandedFormation
	err error
}

func (f *FormationSubscription) Notify(ef *ct.ExpandedFormation) bool {
	f.mtx.RLock()
	defer f.mtx.RUnlock()
	if f.ch == nil {
		return false
	}
	f.ch <- ef
	return true
}

func (f *FormationSubscription) NotifyCurrent() {
	f.Notify(&ct.ExpandedFormation{})
}

func (f *FormationSubscription) Close() {
	f.CloseWithError(nil)
}

func (f *FormationSubscription) CloseWithError(err error) {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	f.err = err
	if f.ch != nil {
		close(f.ch)
		f.ch = nil
	}
}

func (f *FormationSubscription) Err() error {
	f.mtx.RLock()
	defer f.mtx.RUnlock()
	return f.err
}

type FormationRepo struct {
	db        *postgres.DB
	apps      *AppRepo
	releases  *ReleaseRepo
	artifacts *ArtifactRepo

	subscriptions map[*FormationSubscription]struct{}
	stopListener  chan struct{}
	subMtx        sync.RWMutex
}

func NewFormationRepo(db *postgres.DB, appRepo *AppRepo, releaseRepo *ReleaseRepo, artifactRepo *ArtifactRepo) *FormationRepo {
	return &FormationRepo{
		db:            db,
		apps:          appRepo,
		releases:      releaseRepo,
		artifacts:     artifactRepo,
		subscriptions: make(map[*FormationSubscription]struct{}),
		stopListener:  make(chan struct{}),
	}
}

func (r *FormationRepo) validateFormProcs(f *ct.Formation) error {
	release, err := r.releases.Get(f.ReleaseID)
	if err != nil {
		return err
	}
	rel := release.(*ct.Release)
	invalid := make([]string, 0, len(f.Processes))
	for k := range f.Processes {
		if _, ok := rel.Processes[k]; !ok {
			invalid = append(invalid, k)
		}
	}
	if len(invalid) > 0 {
		return ct.ValidationError{Message: fmt.Sprintf("Requested formation includes process types that do not exist in release. Invalid process types: [%s]", strings.Join(invalid, ", "))}
	}
	return nil
}

func (r *FormationRepo) Add(f *ct.Formation) error {
	if err := r.validateFormProcs(f); err != nil {
		return err
	}
	scale := &ct.Scale{
		Processes: f.Processes,
		ReleaseID: f.ReleaseID,
	}
	prevFormation, _ := r.Get(f.AppID, f.ReleaseID)
	if prevFormation != nil {
		scale.PrevProcesses = prevFormation.Processes
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	err = tx.QueryRow("formation_insert", f.AppID, f.ReleaseID, f.Processes, f.Tags).Scan(&f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := createEvent(tx.Exec, &ct.Event{
		AppID:      f.AppID,
		ObjectID:   f.AppID + ":" + f.ReleaseID,
		ObjectType: ct.EventTypeScale,
	}, scale); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
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
	return f, err
}

func scanExpandedFormation(s postgres.Scanner) (*ct.ExpandedFormation, error) {
	f := &ct.ExpandedFormation{
		App:     &ct.App{},
		Release: &ct.Release{},
	}
	var artifactIDs string
	err := s.Scan(
		&f.App.ID,
		&f.App.Name,
		&f.App.Meta,
		&f.Release.ID,
		&artifactIDs,
		&f.Release.Meta,
		&f.Release.Env,
		&f.Release.Processes,
		&f.Processes,
		&f.Tags,
		&f.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	if artifactIDs != "" {
		f.Release.ArtifactIDs = split(artifactIDs[1:len(artifactIDs)-1], ",")
	}
	return f, nil
}

func populateFormationArtifacts(ef *ct.ExpandedFormation, artifacts map[string]*ct.Artifact) {
	ef.ImageArtifact = artifacts[ef.Release.ImageArtifactID()]

	ef.FileArtifacts = make([]*ct.Artifact, len(ef.Release.FileArtifactIDs()))
	for i, id := range ef.Release.FileArtifactIDs() {
		ef.FileArtifacts[i] = artifacts[id]
	}
}

func (r *FormationRepo) Get(appID, releaseID string) (*ct.Formation, error) {
	row := r.db.QueryRow("formation_select", appID, releaseID)
	return scanFormation(row)
}

func (r *FormationRepo) GetExpanded(appID, releaseID string) (*ct.ExpandedFormation, error) {
	row := r.db.QueryRow("formation_select_expanded", appID, releaseID)
	ef, err := scanExpandedFormation(row)
	if err != nil {
		return nil, err
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
	var formations []*ct.ExpandedFormation

	// artifactIDs is a list of artifact IDs related to the formation list
	// and is used to populate the formation's artifact fields using a
	// subsequent artifact list query
	artifactIDs := make(map[string]struct{})

	for rows.Next() {
		formation, err := scanExpandedFormation(rows)
		if err != nil {
			rows.Close()
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

func (r *FormationRepo) Remove(appID, releaseID string) error {
	scale := &ct.Scale{
		ReleaseID: releaseID,
	}
	prevFormation, _ := r.Get(appID, releaseID)
	if prevFormation != nil {
		scale.PrevProcesses = prevFormation.Processes
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	err = tx.Exec("formation_delete", appID, releaseID)
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := createEvent(tx.Exec, &ct.Event{
		AppID:      appID,
		ObjectID:   appID + ":" + releaseID,
		ObjectType: ct.EventTypeScale,
	}, scale); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *FormationRepo) publish(appID, releaseID string) {
	formation, err := r.Get(appID, releaseID)
	if err == ErrNotFound {
		// formation delete event
		updated_at := time.Now()
		formation = &ct.Formation{AppID: appID, ReleaseID: releaseID, UpdatedAt: &updated_at}
	} else if err != nil {
		logger.Error("error getting formation", "fn", "FormationRepo.publish", "app", appID, "release", releaseID, "err", err)
		return
	}

	f, err := r.expandFormation(formation)
	if err != nil {
		logger.Error("error expanding formation", "fn", "FormationRepo.publish", "app", appID, "release", releaseID, "err", err)
		return
	}
	r.subMtx.RLock()
	defer r.subMtx.RUnlock()

	for sub := range r.subscriptions {
		sub.Notify(f)
	}
}

func (r *FormationRepo) expandFormation(formation *ct.Formation) (*ct.ExpandedFormation, error) {
	app, err := r.apps.Get(formation.AppID)
	if err == ErrNotFound {
		app = &ct.App{ID: formation.AppID}
	} else if err != nil {
		return nil, err
	}
	release, err := r.releases.Get(formation.ReleaseID)
	if err != nil {
		return nil, err
	}
	f := &ct.ExpandedFormation{
		App:       app.(*ct.App),
		Release:   release.(*ct.Release),
		Processes: formation.Processes,
		Tags:      formation.Tags,
		UpdatedAt: *formation.UpdatedAt,
	}
	if len(f.Release.ArtifactIDs) > 0 {
		artifacts, err := r.artifacts.ListIDs(f.Release.ArtifactIDs...)
		if err != nil {
			return nil, err
		}
		populateFormationArtifacts(f, artifacts)
	}
	return f, nil
}

func (r *FormationRepo) startListener() error {
	log := logger.New("fn", "FormationRepo.startListener")
	listener, err := r.db.Listen("formations", log)
	if err != nil {
		return err
	}
	go func() {
		defer r.unsubscribeAll()
		for {
			select {
			case n, ok := <-listener.Notify:
				if !ok {
					return
				}
				ids := strings.SplitN(n.Payload, ":", 2)
				go r.publish(ids[0], ids[1])
			case <-r.stopListener:
				listener.Close()
				return
			}
		}
	}()
	return nil
}

func (r *FormationRepo) unsubscribeAll() {
	r.subMtx.Lock()
	defer r.subMtx.Unlock()

	for sub := range r.subscriptions {
		r.unsubscribeLocked(sub)
	}
}

func (r *FormationRepo) Subscribe(ctx context.Context, ch chan *ct.ExpandedFormation, since time.Time, updated chan<- struct{}) (*FormationSubscription, error) {
	// we need to keep the mutex locked whilst calling startListener
	// to avoid a race where multiple subscribers can get added to
	// r.subscriptions before a potentially failed listener start,
	// meaning subsequent subscribers wont try to start the listener.
	r.subMtx.Lock()
	defer r.subMtx.Unlock()

	if len(r.subscriptions) == 0 {
		if err := r.startListener(); err != nil {
			return nil, err
		}
	}

	sub := &FormationSubscription{ch: ch}
	r.subscriptions[sub] = struct{}{}

	go func() {
		if err := r.sendUpdatedSince(ctx, sub, since, updated); err != nil {
			sub.CloseWithError(err)
		}
	}()

	return sub, nil
}

func (r *FormationRepo) sendUpdatedSince(ctx context.Context, sub *FormationSubscription, since time.Time, updated chan<- struct{}) error {
	if updated != nil {
		defer close(updated)
	}

	rows, err := r.db.Query("formation_list_since", since)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		formation, err := scanFormation(rows)
		if err != nil {
			return err
		}
		ef, err := r.expandFormation(formation)
		if err != nil {
			l, _ := ctxhelper.LoggerFromContext(ctx)
			l.Error("error expanding formation", "fn", "FormationRepo.sendUpdatedSince", "app", formation.AppID, "release", formation.ReleaseID, "err", err)
			continue
		}
		// return if Notify returns false (which indicates that the
		// subscriber channel is closed)
		if ok := sub.Notify(ef); !ok {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	sub.NotifyCurrent()
	return nil
}

func (r *FormationRepo) Unsubscribe(sub *FormationSubscription) {
	r.subMtx.Lock()
	defer r.subMtx.Unlock()
	r.unsubscribeLocked(sub)
}

func (r *FormationRepo) unsubscribeLocked(sub *FormationSubscription) {
	go func(ch chan *ct.ExpandedFormation) {
		// drain to prevent deadlock while removing the listener
		for range ch {
		}
	}(sub.ch)
	delete(r.subscriptions, sub)
	sub.Close()
	if len(r.subscriptions) == 0 {
		select {
		case r.stopListener <- struct{}{}:
		default:
		}
	}
}

func (c *controllerAPI) PutFormation(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	app := c.getApp(ctx)
	release, err := c.getRelease(ctx)
	if err != nil {
		respondWithError(w, err)
		return
	}

	var formation ct.Formation
	if err = httphelper.DecodeJSON(req, &formation); err != nil {
		respondWithError(w, err)
		return
	}

	if release.ImageArtifactID() == "" {
		respondWithError(w, ct.ValidationError{Message: "release is not deployable"})
		return
	}

	formation.AppID = app.ID
	formation.ReleaseID = release.ID

	if err = schema.Validate(formation); err != nil {
		respondWithError(w, err)
		return
	}

	if err = c.formationRepo.Add(&formation); err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, &formation)
}

func (c *controllerAPI) GetFormation(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)

	app := c.getApp(ctx)
	releaseID := params.ByName("releases_id")
	if req.URL.Query().Get("expand") == "true" {
		formation, err := c.formationRepo.GetExpanded(app.ID, releaseID)
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

func (c *controllerAPI) DeleteFormation(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	params, _ := ctxhelper.ParamsFromContext(ctx)

	app := c.getApp(ctx)
	formation, err := c.formationRepo.Get(app.ID, params.ByName("releases_id"))
	if err != nil {
		respondWithError(w, err)
		return
	}
	err = c.formationRepo.Remove(app.ID, formation.ReleaseID)
	if err != nil {
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

func (c *controllerAPI) streamFormations(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	ch := make(chan *ct.ExpandedFormation)
	since, err := time.Parse(time.RFC3339, req.FormValue("since"))
	if err != nil {
		respondWithError(w, err)
		return
	}
	sub, err := c.formationRepo.Subscribe(ctx, ch, since, nil)
	if err != nil {
		respondWithError(w, err)
		return
	}
	defer c.formationRepo.Unsubscribe(sub)
	l, _ := ctxhelper.LoggerFromContext(ctx)
	stream := sse.NewStream(w, ch, l)
	stream.Serve()
	stream.Wait()
	if err := sub.Err(); err != nil {
		stream.Error(err)
	}
}
