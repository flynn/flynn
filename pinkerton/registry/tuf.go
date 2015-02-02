package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"

	tuf "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-tuf/client"
	"github.com/flynn/flynn/pkg/tufutil"
)

func NewTUFSession(client *tuf.Client, ref *Ref) Session {
	return &tufSession{client, ref}
}

type tufSession struct {
	client *tuf.Client
	ref    *Ref
}

func (s *tufSession) ImageID() string {
	return s.ref.imageID
}

func (s *tufSession) Repo() string {
	return s.ref.repo
}

func (s *tufSession) GetImage() (*Image, error) {
	id := s.ref.imageID
	if s.ref.tag != "" {
		tags, err := s.tags()
		if err != nil {
			return nil, err
		}
		if id = tags[s.ref.tag]; id == "" {
			return nil, fmt.Errorf("registry: tag %q not found", s.ref.tag)
		}
	}
	img := &Image{session: s}
	_, err := s.get(fmt.Sprintf("/images/%s/json", id), img)
	return img, err
}

func (s *tufSession) GetLayer(id string) (io.ReadCloser, error) {
	layer, err := s.get(fmt.Sprintf("/images/%s/layer", id), nil)
	if err != nil {
		return nil, err
	}
	return layer, nil
}

func (s *tufSession) GetAncestors(id string) ([]*Image, error) {
	var ids []string
	if _, err := s.get(fmt.Sprintf("/images/%s/ancestry", id), &ids); err != nil {
		return nil, err
	}
	images := make([]*Image, len(ids))
	for i, id := range ids {
		img := &Image{session: s}
		if _, err := s.get(fmt.Sprintf("/images/%s/json", id), img); err != nil {
			return nil, err
		}
		images[i] = img
	}
	return images, nil
}

func (s *tufSession) tags() (map[string]string, error) {
	var tags map[string]string
	_, err := s.get(fmt.Sprintf("/repositories/%s/tags", s.ref.repo), &tags)
	return tags, err
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
