package registry

import (
	"fmt"
	"io"
	"net/url"
	"strings"
)

type Session interface {
	Repo() string
	ImageID() string
	GetImage() (*Image, error)
	GetLayer(string) (io.ReadCloser, error)
	GetAncestors(string) ([]*Image, error)
}

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
	if !strings.Contains(ref.repo, "/") {
		// silly docker hack: https://github.com/dotcloud/docker/blob/1310243d488cfede2f5765e79b01ab20efd46cc0/registry/registry.go#L278-L282
		ref.repo = "library/" + ref.repo
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
