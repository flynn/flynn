package store

import (
	"io"

	ct "github.com/flynn/flynn/controller/types"
)

// Store is an interface representing types which store image layers
type Store interface {
	GetLayer(string) (*ct.ImageLayer, bool)
	PutLayer(string, io.Reader, map[string]string) (*ct.ImageLayer, error)
	LayerURLTemplate() string
}

// LayerWriter saves a layer into the store
type LayerWriter interface {
	io.Writer
	Cancel() error
	Commit(id string, meta map[string]string) (*ct.ImageLayer, error)
}
