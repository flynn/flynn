package main

import (
	"errors"
	"sync"
)

type Release struct {
	ID          string                 `json:"id,omitempty"`
	ArtifactID  string                 `json:"artifact,omitempty"`
	Environment map[string]string      `json:"environment,omitempty"`
	Processes   map[string]ProcessType `json:"processes,omitempty"`
}

type ProcessType struct {
	Cmd   []string     `json:"cmd,omitempty"`
	Ports ProcessPorts `json:"ports,omitempty"`
}

type ProcessPorts struct {
	TCP int `json:"tcp,omitempty"`
	UDP int `json:"udp,omitempty"`
}

type ReleaseRepo struct {
	artifacts  *ArtifactRepo
	releaseIDs map[string]*Release
	releases   []*Release
	mtx        sync.RWMutex
}

func NewReleaseRepo(artifactRepo *ArtifactRepo) *ReleaseRepo {
	return &ReleaseRepo{
		artifacts:  artifactRepo,
		releaseIDs: make(map[string]*Release),
	}
}

// - validate
// - set id
// - persist
func (r *ReleaseRepo) Add(data interface{}) error {
	release := data.(*Release)
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
