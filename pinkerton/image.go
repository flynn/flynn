package pinkerton

import (
	"encoding/json"
	"io"
	"time"

	"github.com/docker/docker/layer"
	"github.com/docker/docker/pkg/progress"
	"golang.org/x/net/context"
)

type Image struct {
	config  *ImageConfig
	session *tufSession
}

type ImageConfig struct {
	ID              string           `json:"id"`
	ParentID        string           `json:"parent,omitempty"`
	Comment         string           `json:"comment,omitempty"`
	Created         time.Time        `json:"created"`
	Container       string           `json:"container,omitempty"`
	ContainerConfig *json.RawMessage `json:"container_config,omitempty"`
	Config          *json.RawMessage `json:"config,omitempty"`
	DockerVersion   string           `json:"docker_version,omitempty"`
	Author          string           `json:"author,omitempty"`
	Architecture    string           `json:"architecture,omitempty"`
	OS              string           `json:"os,omitempty"`
	Size            int64            `json:"size,omitempty"`
}

func (i *Image) Key() string {
	return i.ID()
}

func (i *Image) ID() string {
	return i.config.ID
}

func (i *Image) DiffID() (layer.DiffID, error) {
	return layer.DiffID(""), nil
}

func (i *Image) Parent() string {
	return i.config.ParentID
}

func (i *Image) MarshalConfig() ([]byte, error) {
	return json.Marshal(i.config)
}

func (i *Image) Download(ctx context.Context, progressOutput progress.Output) (io.ReadCloser, int64, error) {
	layer, err := i.session.GetLayer(i.ID())
	return layer, i.config.Size, err
}

func (i *Image) Close() {}
