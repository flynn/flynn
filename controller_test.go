package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/flynn/go-sql"
	"github.com/go-martini/martini"

	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/flynn-controller/utils"
	_ "github.com/flynn/pq"
	. "github.com/titanous/gocheck"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct {
	cc  *fakeCluster
	srv *httptest.Server
	m   *martini.Martini
}

var _ = Suite(&S{})

func (s *S) SetUpSuite(c *C) {
	dbname := "controllertest"
	if os.Getenv("PGDATABASE") != "" {
		dbname = os.Getenv("PGDATABASE")
	} else {
		os.Setenv("PGDATABASE", dbname)
	}
	if os.Getenv("PGSSLMODE") == "" {
		os.Setenv("PGSSLMODE", "disable")
	}

	db, err := sql.Open("postgres", "dbname=postgres")
	if err != nil {
		c.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbname)); err != nil {
		c.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf("CREATE DATABASE %s", dbname)); err != nil {
		c.Fatal(err)
	}
	db.Close()

	dsn := fmt.Sprintf("dbname=%s", dbname)
	db, err = sql.Open("postgres", dsn)
	if err != nil {
		c.Fatal(err)
	}
	if err = migrateDB(db); err != nil {
		c.Fatal(err)
	}
	dbw := testDBWrapper{DB: db, dsn: dsn}

	s.cc = newFakeCluster()
	handler, m := appHandler(handlerConfig{db: dbw, cc: s.cc, sc: newFakeRouter()})
	s.m = m
	s.srv = httptest.NewServer(handler)
}

type testDBWrapper struct {
	*sql.DB
	dsn string
}

func (w testDBWrapper) DSN() string       { return w.dsn }
func (w testDBWrapper) Database() *sql.DB { return w.DB }

func (s *S) send(method, path string, in, out interface{}) (*http.Response, error) {
	buf, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, s.srv.URL+path, bytes.NewBuffer(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if out != nil && res.StatusCode == 200 {
		defer res.Body.Close()
		return res, json.NewDecoder(res.Body).Decode(out)
	}
	return res, nil
}

func (s *S) Post(path string, in, out interface{}) (*http.Response, error) {
	return s.send("POST", path, in, out)
}

func (s *S) Put(path string, in, out interface{}) (*http.Response, error) {
	return s.send("PUT", path, in, out)
}

func (s *S) Get(path string, data interface{}) (*http.Response, error) {
	res, err := http.Get(s.srv.URL + path)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return res, fmt.Errorf("Unexpected status code %d", res.StatusCode)
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
	out := &ct.App{}
	res, err := s.Post("/apps", in, out)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	return out
}

func (s *S) TestCreateApp(c *C) {
	for i, id := range []string{"", utils.UUID()} {
		name := fmt.Sprintf("create-app-%d", i)
		app := s.createTestApp(c, &ct.App{ID: id, Name: name, Protected: true})
		c.Assert(app.Name, Equals, name)
		c.Assert(app.ID, Not(Equals), "")
		if id != "" {
			c.Assert(app.ID, Equals, id)
		}
		c.Assert(app.Protected, Equals, true)

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
}

func (s *S) TestUpdateApp(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "update-app"})
	c.Assert(app.Protected, Equals, false)

	gotApp := &ct.App{}
	res, err := s.Post("/apps/"+app.Name, map[string]bool{"protected": true}, gotApp)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(gotApp.Protected, Equals, true)

	res, err = s.Post("/apps/"+app.ID, map[string]bool{"protected": false}, gotApp)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(gotApp.Protected, Equals, false)
}

func (s *S) TestProtectedApp(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "protected-app", Protected: true})
	release := s.createTestRelease(c, &ct.Release{
		Processes: map[string]ct.ProcessType{"web": {}, "worker": {}},
	})

	path := formationPath(app.ID, release.ID)
	for _, t := range []struct {
		procs  map[string]int
		status int
	}{
		{nil, 400},
		{map[string]int{"web": 1}, 400},
		{map[string]int{"worker": 1, "web": 0}, 400},
		{map[string]int{"worker": 1, "web": 1}, 200},
	} {
		res, err := s.Put(path, &ct.Formation{Processes: t.procs}, nil)
		c.Assert(err, IsNil)
		res.Body.Close()
		c.Assert(res.StatusCode, Equals, t.status)
	}
}

func (s *S) createTestArtifact(c *C, in *ct.Artifact) *ct.Artifact {
	out := &ct.Artifact{}
	res, err := s.Post("/artifacts", in, out)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	return out
}

func (s *S) TestCreateArtifact(c *C) {
	for i, id := range []string{"", utils.UUID()} {
		in := &ct.Artifact{
			ID:   id,
			Type: "docker-image",
			URI:  fmt.Sprintf("docker://flynn/host?id=adsf%d", i),
		}
		out := s.createTestArtifact(c, in)

		c.Assert(out.Type, Equals, in.Type)
		c.Assert(out.URI, Equals, in.URI)
		c.Assert(out.ID, Not(Equals), "")
		if id != "" {
			c.Assert(out.ID, Equals, id)
		}

		gotArtifact := &ct.Artifact{}
		res, err := s.Get("/artifacts/"+out.ID, gotArtifact)
		c.Assert(err, IsNil)
		c.Assert(gotArtifact, DeepEquals, out)

		res, err = s.Get("/artifacts/fail"+out.ID, gotArtifact)
		c.Assert(res.StatusCode, Equals, 404)
	}
}

func (s *S) createTestRelease(c *C, in *ct.Release) *ct.Release {
	if in.ArtifactID == "" {
		in.ArtifactID = s.createTestArtifact(c, &ct.Artifact{}).ID
	}
	out := &ct.Release{}
	res, err := s.Post("/releases", in, out)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	return out
}

func (s *S) createTestKey(c *C, in *ct.Key) *ct.Key {
	out := &ct.Key{}
	res, err := s.Post("/keys", in, out)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	return out
}

func (s *S) TestCreateRelease(c *C) {
	for _, id := range []string{"", utils.UUID()} {
		in := &ct.Release{ID: id}
		out := s.createTestRelease(c, in)
		c.Assert(out.ArtifactID, Equals, in.ArtifactID)
		if id != "" {
			c.Assert(out.ID, Equals, id)
		}

		gotRelease := &ct.Release{}
		res, err := s.Get("/releases/"+out.ID, gotRelease)
		c.Assert(err, IsNil)
		c.Assert(gotRelease, DeepEquals, out)

		res, err = s.Get("/releases/fail"+out.ID, gotRelease)
		c.Assert(res.StatusCode, Equals, 404)
	}
}

func (s *S) TestCreateFormation(c *C) {
	for i, useName := range []bool{false, true} {
		release := s.createTestRelease(c, &ct.Release{})
		app := s.createTestApp(c, &ct.App{Name: fmt.Sprintf("create-formation-%d", i)})

		in := &ct.Formation{ReleaseID: release.ID, AppID: app.ID, Processes: map[string]int{"web": 1}}
		if useName {
			in.AppID = app.Name
		}
		out := s.createTestFormation(c, in)
		c.Assert(out.AppID, Equals, app.ID)
		c.Assert(out.ReleaseID, Equals, release.ID)
		c.Assert(out.Processes["web"], Equals, 1)

		gotFormation := &ct.Formation{}
		var path string
		if useName {
			path = formationPath(app.Name, release.ID)
		} else {
			path = formationPath(app.ID, release.ID)
		}
		res, err := s.Get(path, gotFormation)
		c.Assert(err, IsNil)
		c.Assert(res.StatusCode, Equals, 200)
		c.Assert(gotFormation, DeepEquals, out)

		res, err = s.Get(path+"fail", gotFormation)
		c.Assert(res.StatusCode, Equals, 404, Commentf("path:%s formation:", path+"fail"))
	}
}

func (s *S) createTestFormation(c *C, formation *ct.Formation) *ct.Formation {
	path := formationPath(formation.AppID, formation.ReleaseID)
	formation.AppID = ""
	formation.ReleaseID = ""
	out := &ct.Formation{}
	res, err := s.Put(path, formation, out)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	return out
}

func formationPath(appID, releaseID string) string {
	return "/apps/" + appID + "/formations/" + releaseID
}

func (s *S) TestDeleteFormation(c *C) {
	for i, useName := range []bool{false, true} {
		release := s.createTestRelease(c, &ct.Release{})
		app := s.createTestApp(c, &ct.App{Name: fmt.Sprintf("delete-formation-%d", i)})

		out := s.createTestFormation(c, &ct.Formation{ReleaseID: release.ID, AppID: app.ID})
		var path string
		if useName {
			path = formationPath(app.Name, release.ID)
		} else {
			path = formationPath(app.ID, release.ID)
		}
		res, err := s.Delete(path)
		c.Assert(err, IsNil)
		c.Assert(res.StatusCode, Equals, 200)

		res, err = s.Get(path, out)
		c.Assert(res.StatusCode, Equals, 404)
	}
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
	s.createTestApp(c, &ct.App{Name: "list-test"})

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
	app := s.createTestApp(c, &ct.App{Name: "formation-list"})
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
	out := &ct.Release{}
	res, err := s.Put("/apps/"+appID+"/release", &ct.Release{ID: id}, out)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	return out
}

func (s *S) TestSetAppRelease(c *C) {
	release := s.createTestRelease(c, &ct.Release{})
	app := s.createTestApp(c, &ct.App{Name: "set-release"})

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

func (s *S) createTestProvider(c *C, provider *ct.Provider) *ct.Provider {
	out := &ct.Provider{}
	res, err := s.Post("/providers", provider, out)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	return out
}

func (s *S) TestCreateProvider(c *C) {
	provider := s.createTestProvider(c, &ct.Provider{URL: "https://example.com", Name: "foo"})
	c.Assert(provider.Name, Equals, "foo")
	c.Assert(provider.URL, Equals, "https://example.com")
	c.Assert(provider.ID, Not(Equals), "")

	gotProvider := &ct.Provider{}
	res, err := s.Get("/providers/"+provider.ID, gotProvider)
	c.Assert(err, IsNil)
	c.Assert(gotProvider, DeepEquals, provider)

	res, err = s.Get("/providers/"+provider.Name, gotProvider)
	c.Assert(err, IsNil)
	c.Assert(gotProvider, DeepEquals, provider)

	res, err = s.Get("/apps/fail"+provider.ID, gotProvider)
	c.Assert(res.StatusCode, Equals, 404)
}

func (s *S) TestProviderList(c *C) {
	s.createTestProvider(c, &ct.Provider{URL: "https://example.org", Name: "list-test"})

	var list []ct.Provider
	res, err := s.Get("/providers", &list)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)

	c.Assert(len(list) > 0, Equals, true)
	c.Assert(list[0].ID, Not(Equals), "")
}
