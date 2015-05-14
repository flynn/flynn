package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/que-go"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	"github.com/flynn/flynn/controller/client"
	tu "github.com/flynn/flynn/controller/testutils"
	ct "github.com/flynn/flynn/controller/types"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/testutils/postgres"
)

func init() {
	schemaRoot, _ = filepath.Abs(filepath.Join("..", "schema"))
}

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct {
	cc   *tu.FakeCluster
	srv  *httptest.Server
	hc   handlerConfig
	c    *controller.Client
	flac *fakeLogAggregatorClient
}

var _ = Suite(&S{})

var authKey = "test"

func (s *S) SetUpSuite(c *C) {
	dbname := "controllertest"
	if err := pgtestutils.SetupPostgres(dbname); err != nil {
		c.Fatal(err)
	}

	dsn := fmt.Sprintf("dbname=%s", dbname)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		c.Fatal(err)
	}
	if err = migrateDB(db); err != nil {
		c.Fatal(err)
	}
	pg := postgres.New(db, dsn)

	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig: pgx.ConnConfig{
			Host:     "/var/run/postgresql",
			Database: dbname,
		},
		AfterConnect: que.PrepareStatements,
	})
	if err != nil {
		c.Fatal(err)
	}

	s.flac = newFakeLogAggregatorClient()
	s.cc = tu.NewFakeCluster()
	s.hc = handlerConfig{
		db:      pg,
		cc:      s.cc,
		lc:      s.flac,
		rc:      newFakeRouter(),
		pgxpool: pgxpool,
		keys:    []string{authKey},
	}
	handler := appHandler(s.hc)
	s.srv = httptest.NewServer(handler)
	client, err := controller.NewClient(s.srv.URL, authKey)
	c.Assert(err, IsNil)
	s.c = client
}

func (s *S) TestBadAuth(c *C) {
	res, err := http.Get(s.srv.URL + "/apps")
	c.Assert(err, IsNil)
	res.Body.Close()
	c.Assert(res.StatusCode, Equals, 401)

	req, err := http.NewRequest("GET", s.srv.URL+"/apps", nil)
	c.Assert(err, IsNil)
	req.SetBasicAuth("", authKey+"wrong")
	res, err = http.DefaultClient.Do(req)
	c.Assert(err, IsNil)
	res.Body.Close()
	c.Assert(res.StatusCode, Equals, 401)
}

func (s *S) createTestApp(c *C, in *ct.App) *ct.App {
	c.Assert(s.c.CreateApp(in), IsNil)
	return in
}

func (s *S) TestCreateApp(c *C) {
	for _, id := range []string{"", random.UUID()} {
		app := s.createTestApp(c, &ct.App{ID: id, Meta: map[string]string{"foo": "bar"}})
		c.Assert(app.Name, Not(Equals), "")
		c.Assert(app.ID, Not(Equals), "")
		if id != "" {
			c.Assert(app.ID, Equals, id)
		}
		c.Assert(app.Meta["foo"], Equals, "bar")

		gotApp, err := s.c.GetApp(app.ID)
		c.Assert(err, IsNil)
		c.Assert(gotApp, DeepEquals, app)

		gotApp, err = s.c.GetApp(app.Name)
		c.Assert(err, IsNil)
		c.Assert(gotApp, DeepEquals, app)

		gotApp, err = s.c.GetApp("fail" + app.ID)
		c.Assert(err, Equals, controller.ErrNotFound)
	}
}

func (s *S) TestSystemApp(c *C) {
	app := s.createTestApp(c, &ct.App{Meta: map[string]string{"flynn-system-app": "true"}})
	c.Assert(app.System(), Equals, true)

	app.Meta["flynn-system-app"] = "false"
	c.Assert(s.c.UpdateApp(app), IsNil)
	c.Assert(app.System(), Equals, false)

	delete(app.Meta, "flynn-system-app")
	c.Assert(s.c.UpdateApp(app), IsNil)
	c.Assert(app.System(), Equals, false)
}

func (s *S) TestUpdateApp(c *C) {
	meta := map[string]string{"foo": "bar"}
	app := s.createTestApp(c, &ct.App{Name: "update-app", Meta: meta})
	c.Assert(app.Meta, DeepEquals, meta)

	app = &ct.App{ID: app.ID}
	meta = map[string]string{"foo": "baz", "bar": "foo"}
	app.Meta = meta
	c.Assert(s.c.UpdateApp(app), IsNil)
	c.Assert(app.Meta, DeepEquals, meta)

	app, err := s.c.GetApp(app.ID)
	c.Assert(err, IsNil)
	c.Assert(app.Meta, DeepEquals, meta)
}

func (s *S) TestDeleteApp(c *C) {
	for i, useName := range []bool{false, true} {
		app := s.createTestApp(c, &ct.App{Name: fmt.Sprintf("delete-app-%d", i)})

		var appID string
		if useName {
			appID = app.Name
		} else {
			appID = app.ID
		}
		_, err := s.c.DeleteApp(appID)
		c.Assert(err, IsNil)

		_, err = s.c.GetApp(appID)
		c.Assert(err, Equals, controller.ErrNotFound)
	}
}

func (s *S) TestDeleteAppUUIDName(c *C) {
	for _, useName := range []bool{false, true} {
		app := s.createTestApp(c, &ct.App{Name: random.UUID()})

		var appID string
		if useName {
			appID = app.Name
		} else {
			appID = app.ID
		}
		_, err := s.c.DeleteApp(appID)
		c.Assert(err, IsNil)

		_, err = s.c.GetApp(appID)
		c.Assert(err, Equals, controller.ErrNotFound)
	}
}

func (s *S) TestDeleteNonExistentApp(c *C) {
	for _, useUUID := range []bool{false, true} {
		var appID string

		if useUUID {
			appID = "foobar"
		} else {
			appID = random.UUID()
		}
		_, err := s.c.DeleteApp(appID)
		c.Assert(err, Equals, controller.ErrNotFound)
	}
}

func (s *S) TestRecreateApp(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "recreate-app"})

	// Post a duplicate
	c.Assert(s.c.CreateApp(&ct.App{Name: "recreate-app"}), Not(IsNil)) // TODO: This should probably be a 4xx error

	// Delete the original
	_, err := s.c.DeleteApp(app.ID)
	c.Assert(err, IsNil)

	// Create the same key
	app = s.createTestApp(c, &ct.App{Name: "recreate-app"})
	c.Assert(app.Name, Equals, "recreate-app")
}

func (s *S) createTestArtifact(c *C, in *ct.Artifact) *ct.Artifact {
	if in.Type == "" {
		in.Type = "docker"
	}
	if in.URI == "" {
		in.URI = fmt.Sprintf("https://example.com/%s", random.String(8))
	}
	c.Assert(s.c.CreateArtifact(in), IsNil)
	return in
}

func (s *S) TestCreateArtifact(c *C) {
	for i, id := range []string{"", random.UUID()} {
		in := &ct.Artifact{
			ID:   id,
			Type: "docker",
			URI:  fmt.Sprintf("docker://flynn/host?id=adsf%d", i),
		}
		out := s.createTestArtifact(c, in)

		c.Assert(out.Type, Equals, in.Type)
		c.Assert(out.URI, Equals, in.URI)
		c.Assert(out.ID, Not(Equals), "")
		if id != "" {
			c.Assert(out.ID, Equals, id)
		}

		gotArtifact, err := s.c.GetArtifact(out.ID)
		c.Assert(err, IsNil)
		c.Assert(gotArtifact, DeepEquals, out)

		_, err = s.c.GetArtifact("fail" + out.ID)
		c.Assert(err, Equals, controller.ErrNotFound)
	}
}

func (s *S) createTestRelease(c *C, in *ct.Release) *ct.Release {
	if in.ArtifactID == "" {
		in.ArtifactID = s.createTestArtifact(c, &ct.Artifact{}).ID
	}
	c.Assert(s.c.CreateRelease(in), IsNil)
	return in
}

func (s *S) createTestKey(c *C, in string) *ct.Key {
	key, err := s.c.CreateKey(in)
	c.Assert(err, IsNil)
	return key
}

func (s *S) TestCreateRelease(c *C) {
	for _, id := range []string{"", random.UUID()} {
		out := s.createTestRelease(c, &ct.Release{ID: id})
		if id != "" {
			c.Assert(out.ID, Equals, id)
		}

		gotRelease, err := s.c.GetRelease(out.ID)
		c.Assert(err, IsNil)
		c.Assert(gotRelease, DeepEquals, out)

		_, err = s.c.GetRelease("fail" + out.ID)
		c.Assert(err, Equals, controller.ErrNotFound)
	}
}

func (s *S) TestCreateFormation(c *C) {
	for i, useName := range []bool{false, true} {
		release := s.createTestRelease(c, &ct.Release{
			Processes: map[string]ct.ProcessType{"web": {}},
		})
		app := s.createTestApp(c, &ct.App{Name: fmt.Sprintf("create-formation-%d", i)})

		// First create a formation with an invalid process type. Will fail.
		in := &ct.Formation{ReleaseID: release.ID, AppID: app.ID, Processes: map[string]int{"foo": 1}}
		if useName {
			in.AppID = app.Name
		}
		err := s.c.PutFormation(in)
		c.Assert(hh.IsValidationError(err), Equals, true)

		// Now edit the formation to have valid process types. Should succeed.
		in.Processes = map[string]int{"web": 1}
		out := s.createTestFormation(c, in)
		c.Assert(out.AppID, Equals, app.ID)
		c.Assert(out.ReleaseID, Equals, release.ID)
		c.Assert(out.Processes["web"], Equals, 1)

		var appID string
		if useName {
			appID = app.Name
		} else {
			appID = app.ID
		}
		gotFormation, err := s.c.GetFormation(appID, release.ID)
		c.Assert(err, IsNil)
		c.Assert(gotFormation, DeepEquals, out)

		_, err = s.c.GetFormation(appID, release.ID+"fail")
		c.Assert(err, Equals, controller.ErrNotFound)
	}
}

func (s *S) createTestFormation(c *C, formation *ct.Formation) *ct.Formation {
	c.Assert(s.c.PutFormation(formation), IsNil)
	return formation
}

func (s *S) TestDeleteFormation(c *C) {
	for i, useName := range []bool{false, true} {
		release := s.createTestRelease(c, &ct.Release{})
		app := s.createTestApp(c, &ct.App{Name: fmt.Sprintf("delete-formation-%d", i)})
		s.createTestFormation(c, &ct.Formation{ReleaseID: release.ID, AppID: app.ID})

		var appID string
		if useName {
			appID = app.Name
		} else {
			appID = app.ID
		}
		c.Assert(s.c.DeleteFormation(appID, release.ID), IsNil)

		_, err := s.c.GetFormation(appID, release.ID)
		c.Assert(err, Equals, controller.ErrNotFound)
	}
}

func (s *S) TestCreateKey(c *C) {
	in := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC5r1JfsAYIFi86KBa7C5nqKo+BLMJk29+5GsjelgBnCmn4J/QxOrVtovNcntoRLUCRwoHEMHzs3Tc6+PdswIxpX1l3YC78kgdJe6LVb962xUgP6xuxauBNRO7tnh9aPGyLbjl9j7qZAcn2/ansG1GBVoX1GSB58iBsVDH18DdVzlGwrR4OeNLmRQj8kuJEuKOoKEkW55CektcXjV08K3QSQID7aRNHgDpGGgp6XDi0GhIMsuDUGHAdPGZnqYZlxuUFaCW2hK6i1UkwnQCCEv/9IUFl2/aqVep2iX/ynrIaIsNKm16o0ooZ1gCHJEuUKRPUXhZUXqkRXqqHd3a4CUhH jonathan@titanous.com"
	out := s.createTestKey(c, in)

	c.Assert(out.ID, Equals, "7ab054ff4a2009fadc67e1f8b380dbee")
	c.Assert(out.Key, Equals, in[:strings.LastIndex(in, " ")])
	c.Assert(out.Comment, Equals, "jonathan@titanous.com")

	gotKey, err := s.c.GetKey(out.ID)
	c.Assert(err, IsNil)
	c.Assert(gotKey, DeepEquals, out)

	_, err = s.c.GetKey(out.ID + "fail")
	c.Assert(err, Equals, controller.ErrNotFound)
}

func (s *S) TestDeleteKey(c *C) {
	key := s.createTestKey(c, "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDJv/RsyRxiSAh7cU236LOCZ3vD9PO87Fi32QbojQxuGDotmk65fN6WUuL7DQjzUnWkFRu4w/svmb+9MuYK0L2b4Kc1rKXBYaytzWqGtv2VaAFObth40AlNr0V26hcTcBNQQPa23Z8LwQNgELn2b/o2CK+Pie1UbE5lHg8R+pm03cI7fYPB0jA6LIS+IVKHslVhjzxtN49xm9W0DiCxouHZEl+Fd5asgtg10HN7CV5l2+ZFyrPAkxkQrzWpkUMgfvU+xFamyczzBKMT0fTYo+TUM3w3w3njJvqXdHjo3anrUF65rSFxfeNkXoe/NQDdvWu+XBfEypWv25hlQv91JI0N")

	c.Assert(s.c.DeleteKey(key.ID), IsNil)

	_, err := s.c.GetKey(key.ID)
	c.Assert(err, Equals, controller.ErrNotFound)
}

func (s *S) TestRecreateKey(c *C) {
	key := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC3I4gHed4RioRMoJTFdVYp9S6QhHUtMe2cdQAmaN5lVuAaEe9GmJ/wtD4pd7sCpw9daCVOD/WWKCDunrwiEwMNzZKPFQPRfrGAgpCdweD+mk62n/DuaeKJFcfB4C/iLqUrYQ9q0QNnokchI4Ts/CaWoesJOQsbtxDwxcaOlYA/Yq/nY/RA3aK0ZfZqngrOjNRuvhnNFeCF94w2CwwX9ley+PtL0LSWOK2F9D/VEAoRMY89av6WQEoho3vLH7PIOP4OKdla7ezxP9nU14MN4PSv2yUS15mZ14SkA3EF+xmO0QXYUcUi4v5UxkBpoRYNAh32KMMD70pXPRCmWvZ5pRrH lewis@lmars.net"

	originalKey := s.createTestKey(c, key)
	c.Assert(originalKey.ID, Equals, "0c0432006c63fc965ef6946fb67ab559")
	c.Assert(originalKey.Key, Equals, key[:strings.LastIndex(key, " ")])
	c.Assert(originalKey.Comment, Equals, "lewis@lmars.net")

	// Post a duplicate
	_, err := s.c.CreateKey(key)
	c.Assert(err, IsNil)

	// Check there is still only one key
	list, err := s.c.KeyList()
	c.Assert(err, IsNil)
	c.Assert(list, HasLen, 1)

	// Delete the original
	c.Assert(s.c.DeleteKey(originalKey.ID), IsNil)

	// Create the same key
	newKey := s.createTestKey(c, key)
	c.Assert(newKey.ID, Equals, "0c0432006c63fc965ef6946fb67ab559")
	c.Assert(newKey.Key, Equals, key[:strings.LastIndex(key, " ")])
	c.Assert(newKey.Comment, Equals, "lewis@lmars.net")
}

func (s *S) TestAppList(c *C) {
	s.createTestApp(c, &ct.App{Name: "list-test"})

	list, err := s.c.AppList()
	c.Assert(err, IsNil)

	c.Assert(len(list) > 0, Equals, true)
	c.Assert(list[0].ID, Not(Equals), "")
}

func (s *S) TestReleaseList(c *C) {
	s.createTestRelease(c, &ct.Release{})

	list, err := s.c.ReleaseList()
	c.Assert(err, IsNil)

	c.Assert(len(list) > 0, Equals, true)
	c.Assert(list[0].ID, Not(Equals), "")
}

func (s *S) TestKeyList(c *C) {
	s.createTestKey(c, "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCqE9AJti/17eigkIhA7+6TF9rdTVxjPv80UxIT6ELaNPHegqib5m94Wab4UoZAGtBPLKJs9o8LRO3H29X5q5eXCU5mwx4qQhcMEYkILWj0Y1T39Xi2RI3jiWcTsphAAYmy+uT2Nt740OK1FaQxfdzYx4cjsjtb8L82e35BkJE2TdjXWkeHxZWDZxMlZXme56jTNsqB2OuC0gfbAbrjSCkolvK1RJbBZSSBgKQrYXiyYjjLfcw2O0ZAKPBeS8ckVf6PO8s/+azZzJZ0Kl7YGHYEX3xRi6sJS0gsI4Y6+sddT1zT5kh0Bg3C8cKnZ1NiVXLH0pPKz68PhjWhwpOVUehD")

	list, err := s.c.KeyList()
	c.Assert(err, IsNil)

	c.Assert(len(list) > 0, Equals, true)
	c.Assert(list[0].ID, Not(Equals), "")

	for _, k := range list {
		c.Assert(s.c.DeleteKey(k.ID), IsNil)
	}

	list, err = s.c.KeyList()
	c.Assert(err, IsNil)
	c.Assert(list, HasLen, 0)
}

func (s *S) TestArtifactList(c *C) {
	s.createTestArtifact(c, &ct.Artifact{})

	list, err := s.c.ArtifactList()
	c.Assert(err, IsNil)

	c.Assert(len(list) > 0, Equals, true)
	c.Assert(list[0].ID, Not(Equals), "")
}

func (s *S) TestFormationList(c *C) {
	release := s.createTestRelease(c, &ct.Release{})
	app := s.createTestApp(c, &ct.App{Name: "formation-list"})
	s.createTestFormation(c, &ct.Formation{ReleaseID: release.ID, AppID: app.ID})

	list, err := s.c.FormationList(app.ID)
	c.Assert(err, IsNil)

	c.Assert(len(list) > 0, Equals, true)
	c.Assert(list[0].ReleaseID, Not(Equals), "")

	for _, f := range list {
		c.Assert(s.c.DeleteFormation(f.AppID, f.ReleaseID), IsNil)
	}

	list, err = s.c.FormationList(app.ID)
	c.Assert(err, IsNil)
	c.Assert(list, HasLen, 0)
}

func (s *S) setAppRelease(c *C, appID, id string) {
	c.Assert(s.c.SetAppRelease(appID, id), IsNil)
}

func (s *S) TestSetAppRelease(c *C) {
	release := s.createTestRelease(c, &ct.Release{})
	app := s.createTestApp(c, &ct.App{Name: "set-release"})

	s.setAppRelease(c, app.ID, release.ID)

	gotRelease, err := s.c.GetAppRelease(app.ID)
	c.Assert(err, IsNil)
	c.Assert(gotRelease, DeepEquals, release)

	gotRelease, err = s.c.GetAppRelease(app.Name)
	c.Assert(err, IsNil)
	c.Assert(gotRelease, DeepEquals, release)

	formations, err := s.c.FormationList(app.ID)
	c.Assert(err, IsNil)
	c.Assert(formations, HasLen, 0)
}

func (s *S) createTestProvider(c *C, provider *ct.Provider) *ct.Provider {
	c.Assert(s.c.CreateProvider(provider), IsNil)
	return provider
}

func (s *S) TestCreateProvider(c *C) {
	provider := s.createTestProvider(c, &ct.Provider{URL: "https://example.com", Name: "foo"})
	c.Assert(provider.Name, Equals, "foo")
	c.Assert(provider.URL, Equals, "https://example.com")
	c.Assert(provider.ID, Not(Equals), "")

	gotProvider, err := s.c.GetProvider(provider.ID)
	c.Assert(err, IsNil)
	c.Assert(gotProvider, DeepEquals, provider)

	gotProvider, err = s.c.GetProvider(provider.Name)
	c.Assert(err, IsNil)
	c.Assert(gotProvider, DeepEquals, provider)

	_, err = s.c.GetProvider("fail" + provider.ID)
	c.Assert(err, Equals, controller.ErrNotFound)
}

func (s *S) TestProviderList(c *C) {
	s.createTestProvider(c, &ct.Provider{URL: "https://example.org", Name: "list-test"})

	list, err := s.c.ProviderList()
	c.Assert(err, IsNil)

	c.Assert(len(list) > 0, Equals, true)
	c.Assert(list[0].ID, Not(Equals), "")
}
