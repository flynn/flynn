package main

import (
	"sync"

	ct "github.com/flynn/flynn-controller/types"
)

type ArtifactRepo struct {
	artifactIDs map[string]*ct.Artifact
	artifacts   []*ct.Artifact
	mtx         sync.RWMutex
}

func NewArtifactRepo() *ArtifactRepo {
	return &ArtifactRepo{artifactIDs: make(map[string]*ct.Artifact)}
}

// - validate
// - set id
// - persist
func (r *ArtifactRepo) Add(data interface{}) error {
	artifact := data.(*ct.Artifact)
	// TODO: actually validate
	artifact.ID = uuid()
	r.mtx.Lock()
	defer r.mtx.Unlock()

	r.artifactIDs[artifact.ID] = artifact
	r.artifacts = append(r.artifacts, artifact)

	return nil
}

func (r *ArtifactRepo) Get(id string) (interface{}, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	artifact, ok := r.artifactIDs[id]
	if !ok {
		return nil, ErrNotFound
	}
	return artifact, nil
}
