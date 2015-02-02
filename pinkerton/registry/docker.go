package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func NewDockerSession(ref *Ref) *dockerSession {
	return &dockerSession{
		ref:   ref,
		index: fmt.Sprintf("%s://%s/v1", ref.scheme, ref.host),
	}
}

type dockerSession struct {
	ref       *Ref
	index     string
	token     string
	endpoints []string
}

func (s *dockerSession) ImageID() string {
	return s.ref.imageID
}

func (s *dockerSession) Repo() string {
	return s.ref.repo
}

func (s *dockerSession) GetImage() (*Image, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/repositories/%s/images", s.index, s.ref.repo), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Docker-Token", "true")
	if s.ref.username != "" {
		req.SetBasicAuth(s.ref.username, s.ref.password)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	res.Body.Close()
	if res.StatusCode == 404 {
		return nil, fmt.Errorf("registry: repo not found")
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("registry: unexpected status %d", res.StatusCode)
	}

	if res.Header.Get("X-Docker-Token") != "" {
		s.token = strings.Join(res.Header["X-Docker-Token"], ",")
	}
	if res.Header.Get("X-Docker-Endpoints") != "" {
		s.endpoints = s.parseEndpoints(res.Header["X-Docker-Endpoints"])
	} else {
		s.endpoints = []string{s.index}
	}

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
	_, err = s.get(fmt.Sprintf("/images/%s/json", id), img)
	return img, err
}

func (s *dockerSession) GetLayer(id string) (io.ReadCloser, error) {
	res, err := s.get(fmt.Sprintf("/images/%s/layer", id), nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

func (s *dockerSession) GetAncestors(id string) ([]*Image, error) {
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

func (s *dockerSession) tags() (map[string]string, error) {
	var tags map[string]string
	_, err := s.get(fmt.Sprintf("/repositories/%s/tags", s.ref.repo), &tags)
	return tags, err
}

func (s *dockerSession) setAuth(req *http.Request) {
	if s.token != "" {
		req.Header.Set("Authorization", "Token "+s.token)
	} else if s.ref.username != "" || s.ref.password != "" {
		req.SetBasicAuth(s.ref.username, s.ref.password)
	}
}

func (s *dockerSession) get(path string, out interface{}) (*http.Response, error) {
	var err error
	for _, endpoint := range s.endpoints {
		var req *http.Request
		req, err = http.NewRequest("GET", endpoint+path, nil)
		if err != nil {
			continue
		}
		s.setAuth(req)
		if out != nil {
			req.Header.Set("Accept", "application/json")
		}

		var res *http.Response
		res, err = http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		if out != nil {
			defer res.Body.Close()
		}
		if res.StatusCode != 200 {
			err = fmt.Errorf("registry: unexpected status %d", res.StatusCode)
			continue
		}

		if out != nil {
			if err = json.NewDecoder(res.Body).Decode(out); err != nil {
				continue
			}
		}
		return res, nil
	}
	return nil, err
}

func (s *dockerSession) parseEndpoints(headers []string) []string {
	var res []string
	for _, h := range headers {
		endpoints := strings.Split(h, ",")
		for _, e := range endpoints {
			u := &url.URL{Scheme: s.ref.scheme, Host: e, Path: "/v1"}
			if s.token == "" && s.ref.username != "" {
				u.User = url.UserPassword(s.ref.username, s.ref.password)
			}
			res = append(res, u.String())
		}
	}
	return res
}
