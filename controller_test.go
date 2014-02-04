package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	. "launchpad.net/gocheck"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct {
	srv *httptest.Server
}

var _ = Suite(&S{})

func (s *S) SetUpSuite(c *C) {
	s.srv = httptest.NewServer(appHandler())
}

func (s *S) Post(path string, data interface{}) (*http.Response, error) {
	buf, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return http.Post(s.srv.URL+path, "application/json", bytes.NewBuffer(buf))
}

func (s *S) Get(path string, data interface{}) (*http.Response, error) {
	res, err := http.Get(s.srv.URL + path)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		// TODO: error here
		return res, nil
	}
	return res, json.NewDecoder(res.Body).Decode(data)
}

func (s *S) TestCreateApp(c *C) {
	res, err := s.Post("/apps", &App{Name: "foo"})
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	app := &App{}
	err = json.NewDecoder(res.Body).Decode(app)
	res.Body.Close()
	c.Assert(err, IsNil)
	c.Assert(app.Name, Equals, "foo")
	c.Assert(app.ID, Not(Equals), "")

	gotApp := &App{}
	res, err = s.Get("/apps/"+app.ID, gotApp)
	c.Assert(err, IsNil)
	c.Assert(gotApp, DeepEquals, app)

	res, err = s.Get("/apps/fail"+app.ID, gotApp)
	c.Assert(res.StatusCode, Equals, 404)
}

func (s *S) createTestArtifact(c *C, in *Artifact) *Artifact {
	res, err := s.Post("/artifacts", in)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	out := &Artifact{}
	err = json.NewDecoder(res.Body).Decode(out)
	res.Body.Close()
	c.Assert(err, IsNil)

	return out
}

func (s *S) TestCreateArtifact(c *C) {
	in := &Artifact{Type: "docker-image", URL: "docker://flynn/host?id=adsf"}
	out := s.createTestArtifact(c, in)

	c.Assert(out.Type, Equals, in.Type)
	c.Assert(out.URL, Equals, in.URL)
	c.Assert(out.ID, Not(Equals), "")

	gotArtifact := &Artifact{}
	res, err := s.Get("/artifacts/"+out.ID, gotArtifact)
	c.Assert(err, IsNil)
	c.Assert(gotArtifact, DeepEquals, out)

	res, err = s.Get("/artifacts/fail"+out.ID, gotArtifact)
	c.Assert(res.StatusCode, Equals, 404)
}

func (s *S) TestCreateRelease(c *C) {
	artifactID := s.createTestArtifact(c, &Artifact{}).ID
	in := &Release{ArtifactID: artifactID}
	res, err := s.Post("/releases", in)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	out := &Release{}
	err = json.NewDecoder(res.Body).Decode(out)
	res.Body.Close()
	c.Assert(err, IsNil)
	c.Assert(out.ArtifactID, Equals, in.ArtifactID)

	gotRelease := &Release{}
	res, err = s.Get("/releases/"+out.ID, gotRelease)
	c.Assert(err, IsNil)
	c.Assert(gotRelease, DeepEquals, out)

	res, err = s.Get("/releases/fail"+out.ID, gotRelease)
	c.Assert(res.StatusCode, Equals, 404)
}
