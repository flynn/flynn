package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	ct "github.com/flynn/flynn-controller/types"
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

func (s *S) send(method, path string, data interface{}) (*http.Response, error) {
	buf, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, s.srv.URL+path, bytes.NewBuffer(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

func (s *S) Post(path string, data interface{}) (*http.Response, error) {
	return s.send("POST", path, data)
}

func (s *S) Put(path string, data interface{}) (*http.Response, error) {
	return s.send("PUT", path, data)
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

func (s *S) Delete(path string) (*http.Response, error) {
	req, err := http.NewRequest("DELETE", s.srv.URL+path, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

func (s *S) createTestApp(c *C, in *ct.App) *ct.App {
	res, err := s.Post("/apps", in)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	out := &ct.App{}
	err = json.NewDecoder(res.Body).Decode(out)
	res.Body.Close()
	c.Assert(err, IsNil)

	return out
}

func (s *S) TestCreateApp(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "foo"})
	c.Assert(app.Name, Equals, "foo")
	c.Assert(app.ID, Not(Equals), "")

	gotApp := &ct.App{}
	res, err := s.Get("/apps/"+app.ID, gotApp)
	c.Assert(err, IsNil)
	c.Assert(gotApp, DeepEquals, app)

	res, err = s.Get("/apps/fail"+app.ID, gotApp)
	c.Assert(res.StatusCode, Equals, 404)
}

func (s *S) createTestArtifact(c *C, in *ct.Artifact) *ct.Artifact {
	res, err := s.Post("/artifacts", in)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	out := &ct.Artifact{}
	err = json.NewDecoder(res.Body).Decode(out)
	res.Body.Close()
	c.Assert(err, IsNil)

	return out
}

func (s *S) TestCreateArtifact(c *C) {
	in := &ct.Artifact{Type: "docker-image", URI: "docker://flynn/host?id=adsf"}
	out := s.createTestArtifact(c, in)

	c.Assert(out.Type, Equals, in.Type)
	c.Assert(out.URI, Equals, in.URI)
	c.Assert(out.ID, Not(Equals), "")

	gotArtifact := &ct.Artifact{}
	res, err := s.Get("/artifacts/"+out.ID, gotArtifact)
	c.Assert(err, IsNil)
	c.Assert(gotArtifact, DeepEquals, out)

	res, err = s.Get("/artifacts/fail"+out.ID, gotArtifact)
	c.Assert(res.StatusCode, Equals, 404)
}

func (s *S) createTestRelease(c *C, in *ct.Release) *ct.Release {
	artifactID := s.createTestArtifact(c, &ct.Artifact{}).ID
	in.ArtifactID = artifactID
	res, err := s.Post("/releases", in)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	out := &ct.Release{}
	err = json.NewDecoder(res.Body).Decode(out)
	res.Body.Close()
	c.Assert(err, IsNil)

	return out
}

func (s *S) TestCreateRelease(c *C) {
	in := &ct.Release{}
	out := s.createTestRelease(c, in)
	c.Assert(out.ArtifactID, Equals, in.ArtifactID)

	gotRelease := &ct.Release{}
	res, err := s.Get("/releases/"+out.ID, gotRelease)
	c.Assert(err, IsNil)
	c.Assert(gotRelease, DeepEquals, out)

	res, err = s.Get("/releases/fail"+out.ID, gotRelease)
	c.Assert(res.StatusCode, Equals, 404)
}

func (s *S) TestCreateFormation(c *C) {
	release := s.createTestRelease(c, &ct.Release{})
	app := s.createTestApp(c, &ct.App{Name: "asdf1"})

	in := &ct.Formation{ReleaseID: release.ID, AppID: app.ID}
	out := s.createTestFormation(c, in)
	c.Assert(out.AppID, Equals, app.ID)
	c.Assert(out.ReleaseID, Equals, release.ID)

	gotFormation := &ct.Formation{}
	path := formationPath(app.ID, release.ID)
	res, err := s.Get(path, gotFormation)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(gotFormation, DeepEquals, out)

	res, err = s.Get(path+"fail", gotFormation)
	c.Assert(res.StatusCode, Equals, 404)
}

func (s *S) createTestFormation(c *C, formation *ct.Formation) *ct.Formation {
	path := formationPath(formation.AppID, formation.ReleaseID)
	formation.AppID = ""
	formation.ReleaseID = ""
	res, err := s.Put(path, formation)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	out := &ct.Formation{}
	err = json.NewDecoder(res.Body).Decode(out)
	res.Body.Close()
	c.Assert(err, IsNil)

	return out
}

func formationPath(appID, releaseID string) string {
	return "/apps/" + appID + "/formations/" + releaseID
}

func (s *S) TestDeleteFormation(c *C) {
	release := s.createTestRelease(c, &ct.Release{})
	app := s.createTestApp(c, &ct.App{Name: "asdf2"})

	out := s.createTestFormation(c, &ct.Formation{ReleaseID: release.ID, AppID: app.ID})
	path := formationPath(app.ID, release.ID)
	res, err := s.Delete(path)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	res, err = s.Get(path, out)
	c.Assert(res.StatusCode, Equals, 404)
}
