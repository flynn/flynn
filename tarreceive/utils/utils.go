package utils

import (
	"fmt"
)

func ConfigURL(id string) string {
	return fmt.Sprintf("http://blobstore.discoverd/tarreceive/layers/%s.json", id)
}

const LayerURLTemplate = "http://blobstore.discoverd/tarreceive/layers/{id}.squashfs"

func LayerURL(id string) string {
	return fmt.Sprintf("http://blobstore.discoverd/tarreceive/layers/%s.squashfs", id)
}
