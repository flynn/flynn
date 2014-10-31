package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func NewRef(s string) (*Ref, error) {
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}

	q := u.Query()
	if q.Get("tag") != "" && q.Get("id") != "" {
		return nil, fmt.Errorf("registry: only one of id or tag may be provided")
	}

	ref := &Ref{
		tag:     q.Get("tag"),
		repo:    strings.TrimPrefix(u.Path, "/"),
		imageID: q.Get("id"),
		index:   fmt.Sprintf("%s://%s/v1", u.Scheme, u.Host),
		scheme:  u.Scheme,
	}
	if u.User != nil {
		ref.username = u.User.Username()
		ref.password, _ = u.User.Password()
	}
	if ref.tag == "" && ref.imageID == "" {
		ref.tag = "latest"
	}

	return ref, nil
}

type Ref struct {
	username  string
	password  string
	token     string
	index     string
	endpoints []string
	repo      string
	tag       string
	imageID   string
	scheme    string
}

func (r *Ref) ImageID() string {
	return r.imageID
}

func (r *Ref) Get() (*Image, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/repositories/%s/images", r.index, r.repo), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Docker-Token", "true")
	if r.username != "" {
		req.SetBasicAuth(r.username, r.password)
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
		r.token = strings.Join(res.Header["X-Docker-Token"], ",")
	}
	if res.Header.Get("X-Docker-Endpoints") != "" {
		r.endpoints = parseEndpoints(res.Header["X-Docker-Endpoints"], r)
	} else {
		r.endpoints = []string{r.index}
	}

	if r.tag != "" {
		tags, err := r.tags()
		if err != nil {
			return nil, err
		}
		if r.imageID = tags[r.tag]; r.imageID == "" {
			return nil, fmt.Errorf("registry: tag %q not found", r.tag)
		}
	}

	return r.image(r.imageID)
}

func (r *Ref) tags() (map[string]string, error) {
	repo := r.repo
	if !strings.Contains(repo, "/") {
		// silly docker hack: https://github.com/dotcloud/docker/blob/1310243d488cfede2f5765e79b01ab20efd46cc0/registry/registry.go#L278-L282
		repo = "library/" + repo
	}
	var tags map[string]string
	_, err := r.registryGet(fmt.Sprintf("/repositories/%s/tags", repo), &tags)
	return tags, err
}

func (r *Ref) setToken(req *http.Request) {
	if r.token != "" {
		req.Header.Set("Authorization", "Token "+r.token)
	}
}

func (r *Ref) layer(id string) (io.ReadCloser, error) {
	res, err := r.registryGet(fmt.Sprintf("/images/%s/layer", id), nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

func (r *Ref) image(id string) (*Image, error) {
	img := &Image{ref: r}
	_, err := r.registryGet(fmt.Sprintf("/images/%s/json", id), img)
	return img, err
}

func (r *Ref) registryGet(path string, out interface{}) (*http.Response, error) {
	var err error
	for _, endpoint := range r.endpoints {
		var req *http.Request
		req, err = http.NewRequest("GET", endpoint+path, nil)
		if err != nil {
			continue
		}
		r.setToken(req)
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

func parseEndpoints(headers []string, ref *Ref) []string {
	var res []string
	for _, h := range headers {
		endpoints := strings.Split(h, ",")
		for _, e := range endpoints {
			u := &url.URL{Scheme: ref.scheme, Host: e, Path: "/v1"}
			if ref.username != "" {
				u.User = url.UserPassword(ref.username, ref.password)
			}
			res = append(res, u.String())
		}
	}
	return res
}

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

	ref   *Ref
	layer io.ReadCloser
}

func (i *Image) Read(p []byte) (int, error) {
	if i.ref == nil {
		return 0, errors.New("registry: improperly initialized Image")
	}
	if i.layer == nil {
		var err error
		i.layer, err = i.ref.layer(i.ID)
		if err != nil {
			return 0, err
		}
	}
	return i.layer.Read(p)
}

func (i *Image) Fetch() error {
	if i.ref == nil {
		return errors.New("registry: improperly initialized Image")
	}
	_, err := i.ref.registryGet(fmt.Sprintf("/images/%s/json", i.ID), i)
	return err
}

func (i *Image) Close() error {
	if i.layer == nil {
		return nil
	}
	return i.layer.Close()
}

var ErrNoParent = errors.New("registry: image has no parent")

func (img *Image) Ancestors() ([]*Image, error) {
	if img.ParentID == "" {
		return nil, ErrNoParent
	}
	var ids []string
	_, err := img.ref.registryGet(fmt.Sprintf("/images/%s/ancestry", img.ID), &ids)
	if err != nil {
		return nil, err
	}

	res := make([]*Image, len(ids))
	for i, id := range ids {
		res[i] = &Image{ID: id, ref: img.ref}
	}
	return res, nil
}
