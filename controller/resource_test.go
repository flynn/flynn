package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/random"
)

func (s *S) provisionTestResource(c *C, name string, apps []string) (*ct.Resource, *ct.Provider) {
	data := []byte(`{"foo":"bar"}`)
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c.Assert(req.URL.Path, Equals, "/things")
		in, err := ioutil.ReadAll(req.Body)
		c.Assert(err, IsNil)
		c.Assert(string(in), Equals, string(data))
		w.Write([]byte(fmt.Sprintf(`{"id":"/things/%s","env":{"foo":"baz"}}`, name)))
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	newHandler := appHandler(handlerConfig{db: s.hc.db, cc: s.cc, sc: s.hc.sc, key: "test"})
	s.srv = httptest.NewServer(newHandler)

	p := s.createTestProvider(c, &ct.Provider{URL: fmt.Sprintf("http://%s/things", srv.Listener.Addr()), Name: name})
	conf := json.RawMessage(data)
	out := &ct.Resource{}
	res, err := s.Post("/providers/"+p.ID+"/resources", &ct.ResourceReq{Config: &conf, Apps: apps}, out)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
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

	gotResource := &ct.Resource{}
	path := fmt.Sprintf("/providers/%s/resources/%s", provider.ID, resource.ID)
	res, err := s.Get(path, gotResource)
	c.Assert(err, IsNil)
	c.Assert(gotResource, DeepEquals, resource)

	res, err = s.Get(path+"fail", gotResource)
	c.Assert(res.StatusCode, Equals, 404)
}

func (s *S) TestPutResource(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "put-resource"})
	provider := s.createTestProvider(c, &ct.Provider{URL: "https://example.ca", Name: "put-resource"})

	resource := &ct.Resource{
		ExternalID: "/foo/bar",
		Env:        map[string]string{"FOO": "BAR"},
		Apps:       []string{app.ID},
	}
	id := random.UUID()
	path := fmt.Sprintf("/providers/%s/resources/%s", provider.ID, id)
	created := &ct.Resource{}
	_, err := s.Put(path, resource, created)
	c.Assert(err, IsNil)

	c.Assert(created.ID, Equals, id)
	c.Assert(created.ProviderID, Equals, provider.ID)
	c.Assert(created.Env, DeepEquals, resource.Env)
	c.Assert(created.Apps, DeepEquals, resource.Apps)
	c.Assert(created.CreatedAt, Not(IsNil))

	gotResource := &ct.Resource{}
	_, err = s.Get(path, gotResource)
	c.Assert(err, IsNil)
	c.Assert(gotResource, DeepEquals, created)
}

func (s *S) TestResourceLists(c *C) {
	app1 := s.createTestApp(c, &ct.App{Name: "resource-list1"})
	app2 := s.createTestApp(c, &ct.App{Name: "resource-list2"})
	apps := []string{app1.ID, app2.ID}

	resource, provider := s.provisionTestResource(c, "resource-list", apps)

	paths := []string{
		fmt.Sprintf("/providers/%s/resources", provider.ID),
		fmt.Sprintf("/providers/%s/resources", provider.Name),
		fmt.Sprintf("/apps/%s/resources", app1.ID),
		fmt.Sprintf("/apps/%s/resources", app1.Name),
	}

	for _, path := range paths {
		var list []*ct.Resource
		res, err := s.Get(path, &list)
		c.Assert(err, IsNil)
		c.Assert(res.StatusCode, Equals, 200)

		c.Assert(len(list) > 0, Equals, true)
		c.Assert(list[0].ID, Equals, resource.ID)
		c.Assert(list[0].Apps, DeepEquals, apps)
	}
}
