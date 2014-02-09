package main

import (
	"errors"
	"sync"

	ct "github.com/flynn/flynn-controller/types"
)

type ReleaseRepo struct {
	artifacts  *ArtifactRepo
	releaseIDs map[string]*ct.Release
	releases   []*ct.Release
	mtx        sync.RWMutex
}

func NewReleaseRepo(artifactRepo *ArtifactRepo) *ReleaseRepo {
	return &ReleaseRepo{
		artifacts:  artifactRepo,
		releaseIDs: make(map[string]*ct.Release),
	}
}

// - validate
// - set id
// - persist
func (r *ReleaseRepo) Add(data interface{}) error {
	release := data.(*ct.Release)
	_, err := r.artifacts.Get(release.ArtifactID)
	if err != nil {
		if err == ErrNotFound {
			return errors.New("controller: unknown artifact")
		}
		return err
	}
	release.ID = uuid()
	r.mtx.Lock()
	defer r.mtx.Unlock()

	r.releaseIDs[release.ID] = release
	r.releases = append(r.releases, release)

	return nil
}

func (r *ReleaseRepo) Get(id string) (interface{}, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	release, ok := r.releaseIDs[id]
	if !ok {
		return nil, ErrNotFound
	}
	return release, nil
}
