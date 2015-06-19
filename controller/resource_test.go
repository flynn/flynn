package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/random"
)

func (s *S) provisionTestResourceWithServer(c *C, name string, apps []string) (*ct.Resource, *ct.Provider, *httptest.Server) {
	data := []byte(`{"foo":"bar"}`)
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "DELETE" {
			w.WriteHeader(200)
			return
		}
		c.Assert(req.URL.Path, Equals, "/things")
		in, err := ioutil.ReadAll(req.Body)
		c.Assert(err, IsNil)
		c.Assert(string(in), Equals, string(data))
		w.Write([]byte(fmt.Sprintf(`{"id":"/things/%s","env":{"foo":"baz"}}`, name)))
	})
	srv := httptest.NewServer(handler)

	p := &ct.Provider{URL: fmt.Sprintf("http://%s/things", srv.Listener.Addr()), Name: name}
	c.Assert(s.c.CreateProvider(p), IsNil)
	conf := json.RawMessage(data)
	out, err := s.c.ProvisionResource(&ct.ResourceReq{ProviderID: p.ID, Config: &conf, Apps: apps})
	c.Assert(err, IsNil)
	return out, p, srv
}

func (s *S) provisionTestResource(c *C, name string, apps []string) (*ct.Resource, *ct.Provider) {
	out, p, srv := s.provisionTestResourceWithServer(c, name, apps)
	srv.Close()
	return out, p
}

func (s *S) TestProvisionResource(c *C) {
	app1 := s.createTestApp(c, &ct.App{Name: "provision-resource1"})
	app2 := s.createTestApp(c, &ct.App{Name: "provision-resource2"})

	resource, provider := s.provisionTestResource(c, "provision-resource", []string{app1.ID, app2.Name})
	c.Assert(resource.Env["foo"], Equals, "baz")
	c.Assert(resource.ProviderID, Equals, provider.ID)
	c.Assert(resource.ExternalID, Equals, "/things/provision-resource")
	c.Assert(resource.ID, Not(Equals), "")
	c.Assert(resource.Apps, DeepEquals, []string{app1.ID, app2.ID})

	gotResource, err := s.c.GetResource(provider.ID, resource.ID)
	c.Assert(err, IsNil)
	c.Assert(gotResource, DeepEquals, resource)

	gotResource, err = s.c.GetResource(provider.ID, resource.ID+"fail")
	c.Assert(err, Equals, controller.ErrNotFound)
}

func (s *S) TestPutResource(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "put-resource"})
	provider := s.createTestProvider(c, &ct.Provider{URL: "https://example.ca", Name: "put-resource"})

	resource := &ct.Resource{
		ID:         random.UUID(),
		ProviderID: provider.ID,
		ExternalID: "/foo/bar",
		Env:        map[string]string{"FOO": "BAR"},
		Apps:       []string{app.ID},
	}
	c.Assert(s.c.PutResource(resource), IsNil)

	c.Assert(resource.ProviderID, Equals, provider.ID)
	c.Assert(resource.CreatedAt, Not(IsNil))

	gotResource, err := s.c.GetResource(provider.ID, resource.ID)
	c.Assert(err, IsNil)
	c.Assert(gotResource, DeepEquals, resource)
}

func (s *S) TestResourceLists(c *C) {
	app1 := s.createTestApp(c, &ct.App{Name: "resource-list1"})
	app2 := s.createTestApp(c, &ct.App{Name: "resource-list2"})
	apps := []string{app1.ID, app2.ID}

	resource, provider := s.provisionTestResource(c, "resource-list", apps)

	check := func(list []*ct.Resource, err error) {
		c.Assert(err, IsNil)

		c.Assert(len(list) > 0, Equals, true)
		c.Assert(list[0].ID, Equals, resource.ID)
		c.Assert(list[0].Apps, DeepEquals, apps)
	}

	check(s.c.ResourceList(provider.ID))
	check(s.c.ResourceList(provider.Name))
	check(s.c.AppResourceList(app1.ID))
	check(s.c.AppResourceList(app1.ID))
}
