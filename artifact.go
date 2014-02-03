package main

import (
	"sync"
)

type Artifact struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	BaseID string `json:"base"`
	URL    string `json:"url"`
}

type ArtifactRepo struct {
	artifactIDs map[string]*Artifact
	artifacts   []*Artifact
	mtx         sync.RWMutex
}

func NewArtifactRepo() *ArtifactRepo {
	return &ArtifactRepo{artifactIDs: make(map[string]*Artifact)}
}

// - validate
// - set id
// - persist
func (r *ArtifactRepo) Add(artifact *Artifact) error {
	// TODO: actually validate
	artifact.ID = uuid()
	r.mtx.Lock()
	defer r.mtx.Unlock()

	r.artifactIDs[artifact.ID] = artifact
	r.artifacts = append(r.artifacts, artifact)

	return nil
}

func (r *ArtifactRepo) Get(id string) *Artifact {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	return r.artifactIDs[id]
}
