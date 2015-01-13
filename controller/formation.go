package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq/hstore"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/go-martini/martini"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/sse"
)

type formationKey struct {
	AppID, ReleaseID string
}

type FormationRepo struct {
	db        *postgres.DB
	apps      *AppRepo
	releases  *ReleaseRepo
	artifacts *ArtifactRepo

	subscriptions map[chan<- *ct.ExpandedFormation]struct{}
	stopListener  chan struct{}
	subMtx        sync.RWMutex
}

func NewFormationRepo(db *postgres.DB, appRepo *AppRepo, releaseRepo *ReleaseRepo, artifactRepo *ArtifactRepo) *FormationRepo {
	return &FormationRepo{
		db:            db,
		apps:          appRepo,
		releases:      releaseRepo,
		artifacts:     artifactRepo,
		subscriptions: make(map[chan<- *ct.ExpandedFormation]struct{}),
		stopListener:  make(chan struct{}),
	}
}

func procsHstore(m map[string]int) hstore.Hstore {
	res := hstore.Hstore{Map: make(map[string]sql.NullString, len(m))}
	for k, v := range m {
		res.Map[k] = sql.NullString{String: strconv.Itoa(v), Valid: true}
	}
	return res

}

func (r *FormationRepo) Add(f *ct.Formation) error {
	// TODO: actually validate
	procs := procsHstore(f.Processes)
	err := r.db.QueryRow("INSERT INTO formations (app_id, release_id, processes) VALUES ($1, $2, $3) RETURNING created_at, updated_at",
		f.AppID, f.ReleaseID, procs).Scan(&f.CreatedAt, &f.UpdatedAt)
	if e, ok := err.(*pq.Error); ok && e.Code.Name() == "unique_violation" {
		err = r.db.QueryRow("UPDATE formations SET processes = $3, updated_at = now(), deleted_at = NULL WHERE app_id = $1 AND release_id = $2 RETURNING created_at, updated_at",
			f.AppID, f.ReleaseID, procs).Scan(&f.CreatedAt, &f.UpdatedAt)
	}
	if err != nil {
		return err
	}
	return nil
}

func scanFormation(s postgres.Scanner) (*ct.Formation, error) {
	f := &ct.Formation{}
	var procs hstore.Hstore
	err := s.Scan(&f.AppID, &f.ReleaseID, &procs, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	f.Processes = make(map[string]int, len(procs.Map))
	for k, v := range procs.Map {
		n, _ := strconv.Atoi(v.String)
		if n > 0 {
			f.Processes[k] = n
		}
	}
	f.AppID = postgres.CleanUUID(f.AppID)
	f.ReleaseID = postgres.CleanUUID(f.ReleaseID)
	return f, nil
}

func (r *FormationRepo) Get(appID, releaseID string) (*ct.Formation, error) {
	row := r.db.QueryRow("SELECT app_id, release_id, processes, created_at, updated_at FROM formations WHERE app_id = $1 AND release_id = $2 AND deleted_at IS NULL", appID, releaseID)
	return scanFormation(row)
}

func (r *FormationRepo) List(appID string) ([]*ct.Formation, error) {
	rows, err := r.db.Query("SELECT app_id, release_id, processes, created_at, updated_at FROM formations WHERE app_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC", appID)
	if err != nil {
		return nil, err
	}
	formations := []*ct.Formation{}
	for rows.Next() {
		formation, err := scanFormation(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		formations = append(formations, formation)
	}
	return formations, nil
}

func (r *FormationRepo) Remove(appID, releaseID string) error {
	err := r.db.Exec("UPDATE formations SET deleted_at = now(), processes = NULL, updated_at = now() WHERE app_id = $1 AND release_id = $2", appID, releaseID)
	if err != nil {
		return err
	}
	return nil
}

func (r *FormationRepo) publish(appID, releaseID string) {
	formation, err := r.Get(appID, releaseID)
	if err == ErrNotFound {
		// formation delete event
		updated_at := time.Now()
		formation = &ct.Formation{AppID: appID, ReleaseID: releaseID, UpdatedAt: &updated_at}
	} else if err != nil {
		// TODO: log error
		return
	}

	f, err := r.expandFormation(formation)
	if err != nil {
		// TODO: log error
		return
	}
	r.subMtx.RLock()
	defer r.subMtx.RUnlock()

	for ch := range r.subscriptions {
		ch <- f
	}
}

func (r *FormationRepo) expandFormation(formation *ct.Formation) (*ct.ExpandedFormation, error) {
	app, err := r.apps.Get(formation.AppID)
	if err == ErrNotFound {
		app = &ct.App{ID: postgres.CleanUUID(formation.AppID)}
	} else if err != nil {
		return nil, err
	}
	release, err := r.releases.Get(formation.ReleaseID)
	if err != nil {
		return nil, err
	}
	artifact, err := r.artifacts.Get(release.(*ct.Release).ArtifactID)
	if err != nil {
		return nil, err
	}
	f := &ct.ExpandedFormation{
		App:       app.(*ct.App),
		Release:   release.(*ct.Release),
		Artifact:  artifact.(*ct.Artifact),
		Processes: formation.Processes,
		UpdatedAt: *formation.UpdatedAt,
	}
	return f, nil
}

func (r *FormationRepo) startListener() error {
	// TODO: get connection string from somewhere
	listenerEvent := func(ev pq.ListenerEventType, err error) {
		if err != nil {
			fmt.Println("LISTENER error:", err)
		}
		// TODO: handle errors
	}
	listener := pq.NewListener(r.db.DSN(), 10*time.Second, time.Minute, listenerEvent)
	if err := listener.Listen("formations"); err != nil {
		return err
	}
	go func() {
		for {
			select {
			case n := <-listener.Notify:
				ids := strings.SplitN(n.Extra, ":", 2)
				go r.publish(ids[0], ids[1])
			case <-r.stopListener:
				listener.Close()
				return
			}
		}
	}()
	return nil
}

func (r *FormationRepo) Subscribe(ch chan<- *ct.ExpandedFormation, stopCh <-chan struct{}, since time.Time) error {
	var startListener bool
	r.subMtx.Lock()
	if len(r.subscriptions) == 0 {
		startListener = true
	}
	r.subscriptions[ch] = struct{}{}
	r.subMtx.Unlock()
	if startListener {
		if err := r.startListener(); err != nil {
			return err
		}
	}
	go r.sendUpdatedSince(ch, stopCh, since)
	return nil
}

func (r *FormationRepo) sendUpdatedSince(ch chan<- *ct.ExpandedFormation, stopCh <-chan struct{}, since time.Time) error {
	rows, err := r.db.Query("SELECT app_id, release_id, processes, created_at, updated_at FROM formations WHERE updated_at >= $1 ORDER BY updated_at DESC", since)
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
			return err
		}
		select {
		case ch <- ef:
		case <-stopCh:
			return nil
		}
	}
	ch <- &ct.ExpandedFormation{} // sentinel
	return rows.Err()
}

func (r *FormationRepo) Unsubscribe(ch chan *ct.ExpandedFormation) {
	r.subMtx.Lock()
	defer r.subMtx.Unlock()
	go func() {
		// drain to prevent deadlock while removing the listener
		for range ch {
		}
	}()
	delete(r.subscriptions, ch)
	if len(r.subscriptions) == 0 {
		r.stopListener <- struct{}{}
	}
}

func getFormations(repo *FormationRepo, req *http.Request, params martini.Params, w http.ResponseWriter, r ResponseHelper) {
	ch := make(chan *ct.ExpandedFormation)
	stopCh := make(chan struct{})
	wr := sse.NewSSEWriter(w)
	enc := json.NewEncoder(wr)
	since, err := time.Parse(time.RFC3339, req.FormValue("since"))
	if err != nil {
		r.Error(err)
		return
	}
	if err := repo.Subscribe(ch, stopCh, since); err != nil {
		r.Error(err)
		return
	}
	go func() {
		<-w.(http.CloseNotifier).CloseNotify()
		repo.Unsubscribe(ch)
		close(stopCh)
	}()
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.WriteHeader(200)
	wr.Flush()
	for data := range ch {
		enc.Encode(data)
		wr.Flush()
	}
}
