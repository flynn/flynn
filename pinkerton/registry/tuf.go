package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"

	tuf "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-tuf/client"
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

func (s *tufSession) GetAncestors(id string) ([]string, error) {
	var ids []string
	_, err := s.get(fmt.Sprintf("/images/%s/ancestry", id), &ids)
	return ids, err
}

func (s *tufSession) tags() (map[string]string, error) {
	var tags map[string]string
	_, err := s.get(fmt.Sprintf("/repositories/%s/tags", s.ref.repo), &tags)
	return tags, err
}

type tmpFile struct {
	*os.File
}

func (t *tmpFile) Delete() error {
	t.File.Close()
	return os.Remove(t.Name())
}

func (t *tmpFile) Close() error {
	return t.Delete()
}

func (s *tufSession) get(name string, out interface{}) (io.ReadCloser, error) {
	file, err := ioutil.TempFile("", "pinkerton")
	if err != nil {
		return nil, err
	}
	name = path.Join("v1", name)
	tmp := &tmpFile{file}
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
