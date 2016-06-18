package pinkerton

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/flynn/flynn/pkg/tufutil"
	tuf "github.com/flynn/go-tuf/client"
)

func NewTUFSession(client *tuf.Client, ref *Ref) *tufSession {
	return &tufSession{client, ref}
}

type tufSession struct {
	client *tuf.Client
	ref    *Ref
}

func (s *tufSession) GetImage() (*Image, error) {
	img := &Image{config: &ImageConfig{}, session: s}
	_, err := s.get(fmt.Sprintf("/images/%s/json", s.ref.imageID), img.config)
	return img, err
}

func (s *tufSession) GetLayer(id string) (io.ReadCloser, error) {
	return s.get(fmt.Sprintf("/images/%s/layer", id), nil)
}

func (s *tufSession) GetAncestors(id string) ([]*Image, error) {
	var ids []string
	if _, err := s.get(fmt.Sprintf("/images/%s/ancestry", id), &ids); err != nil {
		return nil, err
	}
	images := make([]*Image, len(ids))
	for i, id := range ids {
		img := &Image{config: &ImageConfig{}, session: s}
		if _, err := s.get(fmt.Sprintf("/images/%s/json", id), img.config); err != nil {
			return nil, err
		}
		images[i] = img
	}
	return images, nil
}

func (s *tufSession) get(name string, out interface{}) (io.ReadCloser, error) {
	tmp, err := tufutil.NewTempFile()
	if err != nil {
		return nil, err
	}
	name = path.Join("v1", name)
	if err := s.client.Download(name, tmp); err != nil {
		return nil, err
	}
	if _, err := tmp.Seek(0, os.SEEK_SET); err != nil {
		return nil, err
	}
	if out != nil {
		if err = json.NewDecoder(tmp).Decode(out); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return tmp, nil
}
