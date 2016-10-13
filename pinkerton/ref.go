package pinkerton

import (
	"fmt"
	"net/url"
	"strings"
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
	if q.Get("name") == "" {
		return nil, fmt.Errorf("registry: name must be provided")
	}
	if q.Get("tag") != "" && q.Get("id") != "" {
		return nil, fmt.Errorf("registry: only one of id or tag may be provided")
	}

	ref := &Ref{
		scheme:  u.Scheme,
		host:    u.Host,
		repo:    q.Get("name"),
		tag:     q.Get("tag"),
		imageID: q.Get("id"),
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
	scheme   string
	host     string
	repo     string
	tag      string
	imageID  string
	username string
	password string
}

func (r *Ref) ID() string {
	return r.imageID
}

func (r *Ref) Name() string {
	return r.repo
}

func (r *Ref) DockerRepo() string {
	return r.host + "/" + r.repo
}

func (r *Ref) Tag() string {
	if r.imageID != "" {
		return r.imageID
	}
	return r.tag
}

func (r *Ref) DockerRef() string {
	delim := ":"
	if strings.Contains(r.Tag(), ":") {
		delim = "@"
	}
	return r.DockerRepo() + delim + r.Tag()
}
