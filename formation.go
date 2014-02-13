package main

import (
	"sync"

	ct "github.com/flynn/flynn-controller/types"
)

type formationKey struct {
	AppID, ReleaseID string
}

type FormationRepo struct {
	appFormations map[formationKey]*ct.Formation
	formations    []*ct.Formation
	apps          *AppRepo
	releases      *ReleaseRepo
	artifacts     *ArtifactRepo
	mtx           sync.RWMutex

	subscriptions map[chan<- *ct.ExpandedFormation]struct{}
	subMtx        sync.RWMutex
}

func NewFormationRepo(appRepo *AppRepo, releaseRepo *ReleaseRepo, artifactRepo *ArtifactRepo) *FormationRepo {
	return &FormationRepo{
		appFormations: make(map[formationKey]*ct.Formation),
		subscriptions: make(map[chan<- *ct.ExpandedFormation]struct{}),
		apps:          appRepo,
		releases:      releaseRepo,
		artifacts:     artifactRepo,
	}
}

// - validate
// - persist
func (r *FormationRepo) Add(formation *ct.Formation) error {
	// TODO: validate process types

	r.mtx.Lock()
	defer r.mtx.Unlock()
	r.appFormations[formationKey{formation.AppID, formation.ReleaseID}] = formation
	r.formations = append(r.formations, formation)
	go r.publish(formation)
	return nil
}

func (r *FormationRepo) Get(appID, releaseID string) (*ct.Formation, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	formation := r.appFormations[formationKey{appID, releaseID}]
	if formation == nil {
		return nil, ErrNotFound
	}
	return formation, nil
}

func (r *FormationRepo) Remove(appID, releaseID string) error {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	delete(r.appFormations, formationKey{appID, releaseID})
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
