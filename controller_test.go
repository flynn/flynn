package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ct "github.com/flynn/flynn-controller/types"
	. "launchpad.net/gocheck"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct {
	cc  *fakeCluster
	srv *httptest.Server
}

var _ = Suite(&S{})

func (s *S) SetUpSuite(c *C) {
	s.cc = newFakeCluster()
	s.srv = httptest.NewServer(appHandler(s.cc))
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

	res, err = s.Get("/apps/"+app.Name, gotApp)
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

func (s *S) createTestKey(c *C, in *ct.Key) *ct.Key {
	res, err := s.Post("/keys", in)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	out := &ct.Key{}
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

func (s *S) TestCreateKey(c *C) {
	in := &ct.Key{Key: "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC5r1JfsAYIFi86KBa7C5nqKo+BLMJk29+5GsjelgBnCmn4J/QxOrVtovNcntoRLUCRwoHEMHzs3Tc6+PdswIxpX1l3YC78kgdJe6LVb962xUgP6xuxauBNRO7tnh9aPGyLbjl9j7qZAcn2/ansG1GBVoX1GSB58iBsVDH18DdVzlGwrR4OeNLmRQj8kuJEuKOoKEkW55CektcXjV08K3QSQID7aRNHgDpGGgp6XDi0GhIMsuDUGHAdPGZnqYZlxuUFaCW2hK6i1UkwnQCCEv/9IUFl2/aqVep2iX/ynrIaIsNKm16o0ooZ1gCHJEuUKRPUXhZUXqkRXqqHd3a4CUhH jonathan@titanous.com"}
	out := s.createTestKey(c, in)

	c.Assert(out.ID, Equals, "7ab054ff4a2009fadc67e1f8b380dbee")
	c.Assert(out.Key, Equals, in.Key[:strings.LastIndex(in.Key, " ")])
	c.Assert(out.Comment, Equals, "jonathan@titanous.com")

	gotKey := &ct.Key{}
	path := "/keys/" + out.ID
	res, err := s.Get(path, gotKey)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(gotKey, DeepEquals, out)

	res, err = s.Get(path+"fail", gotKey)
	c.Assert(res.StatusCode, Equals, 404)
}

func (s *S) TestDeleteKey(c *C) {
	key := s.createTestKey(c, &ct.Key{Key: "ssh-rsa AABB"})

	path := "/keys/" + key.ID
	res, err := s.Delete(path)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	res, err = s.Get(path, key)
	c.Assert(res.StatusCode, Equals, 404)
}

func (s *S) TestAppList(c *C) {
	s.createTestApp(c, &ct.App{Name: "listTest"})

	var list []ct.App
	res, err := s.Get("/apps", &list)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	c.Assert(len(list) > 0, Equals, true)
	c.Assert(list[0].ID, Not(Equals), "")
}

func (s *S) TestReleaseList(c *C) {
	s.createTestRelease(c, &ct.Release{})

	var list []ct.Release
	res, err := s.Get("/releases", &list)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	c.Assert(len(list) > 0, Equals, true)
	c.Assert(list[0].ID, Not(Equals), "")
}

func (s *S) TestKeyList(c *C) {
	s.createTestKey(c, &ct.Key{Key: "ssh-rsa AAAA"})

	var list []ct.Key
	res, err := s.Get("/keys", &list)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	c.Assert(len(list) > 0, Equals, true)
	c.Assert(list[0].ID, Not(Equals), "")

	for _, k := range list {
		s.Delete("/keys/" + k.ID)
	}

	res, err = s.Get("/keys", &list)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(list, HasLen, 0)
}

func (s *S) TestArtifactList(c *C) {
	s.createTestArtifact(c, &ct.Artifact{})

	var list []ct.Artifact
	res, err := s.Get("/artifacts", &list)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	c.Assert(len(list) > 0, Equals, true)
	c.Assert(list[0].ID, Not(Equals), "")
}

func (s *S) TestFormationList(c *C) {
	release := s.createTestRelease(c, &ct.Release{})
	app := s.createTestApp(c, &ct.App{Name: "formationList"})
	s.createTestFormation(c, &ct.Formation{ReleaseID: release.ID, AppID: app.ID})

	var list []ct.Formation
	path := "/apps/" + app.ID + "/formations"
	res, err := s.Get(path, &list)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	c.Assert(len(list) > 0, Equals, true)
	c.Assert(list[0].ReleaseID, Not(Equals), "")

	for _, f := range list {
		s.Delete(formationPath(f.AppID, f.ReleaseID))
	}

	res, err = s.Get(path, &list)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(list, HasLen, 0)
}

func (s *S) setAppRelease(c *C, appID, id string) *ct.Release {
	res, err := s.Put("/apps/"+appID+"/release", &ct.Release{ID: id})
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	out := &ct.Release{}
	err = json.NewDecoder(res.Body).Decode(out)
	res.Body.Close()
	c.Assert(err, IsNil)
	return out
}

func (s *S) TestSetAppRelease(c *C) {
	release := s.createTestRelease(c, &ct.Release{})
	app := s.createTestApp(c, &ct.App{Name: "setRelease"})

	out := s.setAppRelease(c, app.ID, release.ID)
	c.Assert(out, DeepEquals, release)

	gotRelease := &ct.Release{}
	res, err := s.Get("/apps/"+app.ID+"/release", gotRelease)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(gotRelease, DeepEquals, release)

	res, err = s.Get("/apps/"+app.Name+"/release", gotRelease)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(gotRelease, DeepEquals, release)

	var formations []ct.Formation
	formationsPath := "/apps/" + app.ID + "/formations"
	res, err = s.Get(formationsPath, &formations)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(formations, HasLen, 0)

	s.createTestFormation(c, &ct.Formation{AppID: app.ID, ReleaseID: release.ID, Processes: map[string]int{"web": 1}})
	newRelease := s.createTestRelease(c, &ct.Release{})
	s.setAppRelease(c, app.ID, newRelease.ID)
	res, err = s.Get(formationsPath, &formations)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(formations, HasLen, 1)
	c.Assert(formations[0].ReleaseID, Equals, newRelease.ID)
}
