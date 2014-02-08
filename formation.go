package main

import (
	"sync"
)

type Formation struct {
	AppID     string         `json:"app,omitempty"`
	ReleaseID string         `json:"release,omitempty"`
	Processes map[string]int `json:"processes,omitempty"`
}

type ExpandedFormation struct {
	App       *App
	Release   *Release
	Processes map[string]int
}

type formationKey struct {
	AppID, ReleaseID string
}

type FormationRepo struct {
	appFormations map[formationKey]*Formation
	formations    []*Formation
	apps          *AppRepo
	releases      *ReleaseRepo
	mtx           sync.RWMutex

	subscriptions map[chan<- *ExpandedFormation]struct{}
	subMtx        sync.RWMutex
}

func NewFormationRepo(appRepo *AppRepo, releaseRepo *ReleaseRepo) *FormationRepo {
	return &FormationRepo{
		appFormations: make(map[formationKey]*Formation),
		subscriptions: make(map[chan<- *ExpandedFormation]struct{}),
		apps:          appRepo,
		releases:      releaseRepo,
	}
}

// - validate
// - persist
func (r *FormationRepo) Add(formation *Formation) error {
	// TODO: validate process types

	r.mtx.Lock()
	defer r.mtx.Unlock()
	r.appFormations[formationKey{formation.AppID, formation.ReleaseID}] = formation
	r.formations = append(r.formations, formation)
	go r.publish(formation)
	return nil
}

func (r *FormationRepo) Get(appID, releaseID string) (*Formation, error) {
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
	go r.publish(&Formation{AppID: appID, ReleaseID: releaseID})
	return nil
}

func (r *FormationRepo) publish(formation *Formation) {
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

	f := &ExpandedFormation{
		App:       app.(*App),
		Release:   release.(*Release),
		Processes: formation.Processes,
	}

	r.subMtx.RLock()
	defer r.subMtx.RUnlock()

	for ch := range r.subscriptions {
		ch <- f
	}
}

func (r *FormationRepo) Subscribe(ch chan<- *ExpandedFormation) {
	r.subMtx.Lock()
	r.subscriptions[ch] = struct{}{}
	r.subMtx.Unlock()
}

func (r *FormationRepo) Unsubscribe(ch chan<- *ExpandedFormation) {
	r.subMtx.Lock()
	delete(r.subscriptions, ch)
	r.subMtx.Unlock()
}
