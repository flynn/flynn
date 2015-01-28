package registry

import (
	"encoding/json"
	"errors"
	"io"
	"time"
)

type Image struct {
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

	session Session
	layer   io.ReadCloser
}

func (i *Image) Read(p []byte) (int, error) {
	if i.session == nil {
		return 0, errors.New("registry: improperly initialized Image")
	}
	if i.layer == nil {
		var err error
		i.layer, err = i.session.GetLayer(i.ID)
		if err != nil {
			return 0, err
		}
	}
	return i.layer.Read(p)
}

func (i *Image) Close() error {
	if i.layer == nil {
		return nil
	}
	return i.layer.Close()
}

var ErrNoParent = errors.New("registry: image has no parent")

func (i *Image) Ancestors() ([]*Image, error) {
	if i.ParentID == "" {
		return nil, ErrNoParent
	}
	return i.session.GetAncestors(i.ID)
}
