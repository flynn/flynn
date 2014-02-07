package main

import (
	"sync"
)

type Formation struct {
	AppID     string         `json:"app,omitempty"`
	ReleaseID string         `json:"release,omitempty"`
	Processes map[string]int `json:"processes,omitempty"`
}

type formationKey struct {
	AppID, ReleaseID string
}

type FormationRepo struct {
	appFormations map[formationKey]*Formation
	formations    []*Formation
	mtx           sync.RWMutex
}

func NewFormationRepo() *FormationRepo {
	return &FormationRepo{appFormations: make(map[formationKey]*Formation)}
}

// - validate
// - persist
func (r *FormationRepo) Add(formation *Formation) error {
	// TODO: validate process types

	r.mtx.Lock()
	defer r.mtx.Unlock()
	r.appFormations[formationKey{formation.AppID, formation.ReleaseID}] = formation
	r.formations = append(r.formations, formation)
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
