package utils

import (
	"fmt"

	ct "github.com/flynn/flynn/controller/types"
)

func ConfigURL(id string) string {
	return fmt.Sprintf("http://blobstore.discoverd/docker-receive/layers/%s.json", id)
}

const LayerURLTemplate = "http://blobstore.discoverd/docker-receive/layers/{id}.squashfs"

func LayerURL(layer *ct.ImageLayer) string {
	return fmt.Sprintf("http://blobstore.discoverd/docker-receive/layers/%s.squashfs", layer.ID)
}
