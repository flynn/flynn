package main

import (
	"database/sql"
	"strconv"
	"sync"

	ct "github.com/flynn/flynn-controller/types"
	"github.com/lib/pq"
	"github.com/lib/pq/hstore"
)

type formationKey struct {
	AppID, ReleaseID string
}

type FormationRepo struct {
	db        *DB
	apps      *AppRepo
	releases  *ReleaseRepo
	artifacts *ArtifactRepo

	subscriptions map[chan<- *ct.ExpandedFormation]struct{}
	subMtx        sync.RWMutex
}

func NewFormationRepo(db *DB, appRepo *AppRepo, releaseRepo *ReleaseRepo, artifactRepo *ArtifactRepo) *FormationRepo {
	return &FormationRepo{
		db:            db,
		subscriptions: make(map[chan<- *ct.ExpandedFormation]struct{}),
		apps:          appRepo,
		releases:      releaseRepo,
		artifacts:     artifactRepo,
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
	if e, ok := err.(*pq.Error); ok && e.Code == "23505" /* unique_violation */ {
		err = r.db.QueryRow("UPDATE formations SET processes = $3, updated_at = current_timestamp, deleted_at = nil WHERE app_id = $1 AND release_id = $2 RETURNING created_at, updated_at",
			f.AppID, f.ReleaseID, procs).Scan(&f.CreatedAt, &f.UpdatedAt)
	}
	if err != nil {
		return err
	}
	go r.publish(f)
	return nil
}

func scanFormation(s Scanner) (*ct.Formation, error) {
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
	f.AppID = cleanUUID(f.AppID)
	f.ReleaseID = cleanUUID(f.ReleaseID)
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
	var formations []*ct.Formation
	for rows.Next() {
		formation, err := scanFormation(rows)
		if err != nil {
			return nil, err
		}
		formations = append(formations, formation)
	}
	return formations, nil
}

func (r *FormationRepo) Remove(appID, releaseID string) error {
	err := r.db.Exec("UPDATE formations SET deleted_at = current_timestamp WHERE app_id = $1 AND release_id = $2", appID, releaseID)
	if err != nil {
		return err
	}
	go r.publish(&ct.Formation{AppID: appID, ReleaseID: releaseID})
	return nil
}

func (r *FormationRepo) publish(formation *ct.Formation) {
	app, err := r.apps.Get(formation.AppID)
	if err != nil {
		// TODO: log error
		return
	}
	release, err := r.releases.Get(formation.ReleaseID)
	if err != nil {
		// TODO: log error
		return
	}
	artifact, err := r.artifacts.Get(release.(*ct.Release).ArtifactID)
	if err != nil {
		// TODO: log error
		return
	}

	f := &ct.ExpandedFormation{
		App:       app.(*ct.App),
		Release:   release.(*ct.Release),
		Artifact:  artifact.(*ct.Artifact),
		Processes: formation.Processes,
	}

	r.subMtx.RLock()
	defer r.subMtx.RUnlock()

	for ch := range r.subscriptions {
		ch <- f
	}
}

func (r *FormationRepo) Subscribe(ch chan<- *ct.ExpandedFormation) {
	r.subMtx.Lock()
	r.subscriptions[ch] = struct{}{}
	r.subMtx.Unlock()
}

func (r *FormationRepo) Unsubscribe(ch chan<- *ct.ExpandedFormation) {
	r.subMtx.Lock()
	delete(r.subscriptions, ch)
	r.subMtx.Unlock()
}
