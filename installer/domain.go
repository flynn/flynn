package installer

import (
	"errors"
	"net/http"

	"github.com/flynn/flynn/pkg/httpclient"
)

func AllocateDomain() (*Domain, error) {
	domain := &Domain{}
	return domain, domain.client().Post("/domains", nil, domain)
}

type Domain struct {
	Name  string `json:"domain"`
	Token string `json:"token"`
	c     *httpclient.Client
}

func (d *Domain) client() *httpclient.Client {
	if d.c == nil {
		d.c = &httpclient.Client{
			ErrNotFound: errors.New("domain not found"),
			URL:         "https://domains.flynn.io",
			HTTP:        http.DefaultClient,
		}
	}
	return d.c
}

func (d *Domain) authHeader() http.Header {
	return http.Header{
		"Authorization": {"Token " + d.Token},
	}
}

func (d *Domain) path() string {
	return "/domains/" + d.Name
}

func (d *Domain) Configure(nameservers []string) error {
	data := struct {
		Nameservers []string `json:"nameservers"`
	}{nameservers}
	res, err := d.client().RawReq("PUT", d.path(), d.authHeader(), data, nil)
	if err == nil {
		res.Body.Close()
	}
	return err
}

func (d *Domain) Status() (string, error) {
	var res struct {
		Status string
	}
	_, err := d.client().RawReq("GET", d.path()+"/status", d.authHeader(), nil, &res)
	return res.Status, err
}
