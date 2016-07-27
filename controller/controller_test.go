package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flynn/flynn/controller/client"
	"github.com/flynn/flynn/controller/schema"
	tu "github.com/flynn/flynn/controller/testutils"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/certgen"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/testutils/postgres"
	. "github.com/flynn/go-check"
	"github.com/jackc/pgx"
)

func init() {
	schemaRoot, _ = filepath.Abs(filepath.Join("..", "schema"))
}

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct {
	cc     *tu.FakeCluster
	srv    *httptest.Server
	hc     handlerConfig
	c      controller.Client
	flac   *fakeLogAggregatorClient
	caCert []byte
}

var _ = Suite(&S{})

var authKey = "test"

func setupTestDB(c *C, dbname string) *postgres.DB {
	if err := pgtestutils.SetupPostgres(dbname); err != nil {
		c.Fatal(err)
	}
	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig: pgx.ConnConfig{
			Host:     os.Getenv("PGHOST"),
			Database: dbname,
		},
	})
	if err != nil {
		c.Fatal(err)
	}
	return postgres.New(pgxpool, nil)
}

func (s *S) SetUpSuite(c *C) {
	dbname := "controllertest"
	db := setupTestDB(c, dbname)
	if err := migrateDB(db); err != nil {
		c.Fatal(err)
	}

	// reconnect with que statements prepared now that schema is migrated

	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig: pgx.ConnConfig{
			Host:     "/var/run/postgresql",
			Database: dbname,
		},
		AfterConnect: schema.PrepareStatements,
	})
	if err != nil {
		c.Fatal(err)
	}
	db = postgres.New(pgxpool, nil)

	ca, err := certgen.Generate(certgen.Params{IsCA: true})
	if err != nil {
		c.Fatal(err)
	}
	s.caCert = []byte(ca.PEM)

	s.flac = newFakeLogAggregatorClient()
	s.cc = tu.NewFakeCluster()
	s.hc = handlerConfig{
		db:     db,
		cc:     s.cc,
		lc:     s.flac,
		rc:     newFakeRouter(),
		keys:   []string{authKey},
		caCert: s.caCert,
	}
	handler := appHandler(s.hc)
	s.srv = httptest.NewServer(handler)
	client, err := controller.NewClient(s.srv.URL, authKey)
	c.Assert(err, IsNil)
	s.c = client
}

func (s *S) SetUpTest(c *C) {
	s.cc.SetHosts(make(map[string]*tu.FakeHostClient))
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

	app.Meta = nil
	strategy := "one-by-one"
	app.Strategy = strategy
	c.Assert(s.c.UpdateApp(app), IsNil)
	c.Assert(app.Meta, DeepEquals, meta)
	c.Assert(app.Strategy, Equals, strategy)

	app, err = s.c.GetApp(app.ID)
	c.Assert(err, IsNil)
	c.Assert(app.Meta, DeepEquals, meta)
	c.Assert(app.Strategy, Equals, strategy)

	timeout := int32(150)
	app = &ct.App{
		ID:            app.ID,
		DeployTimeout: timeout,
	}
	c.Assert(s.c.UpdateApp(app), IsNil)
	c.Assert(app.Meta, DeepEquals, meta)
	c.Assert(app.Strategy, Equals, strategy)
	c.Assert(app.DeployTimeout, Equals, timeout)

	app, err = s.c.GetApp(app.ID)
	c.Assert(err, IsNil)
	c.Assert(app.Meta, DeepEquals, meta)
	c.Assert(app.Strategy, Equals, strategy)
	c.Assert(app.DeployTimeout, Equals, timeout)
}

func (s *S) TestUpdateAppMeta(c *C) {
	meta := map[string]string{"foo": "bar"}
	app := s.createTestApp(c, &ct.App{Name: "update-app-meta", Meta: meta})
	c.Assert(app.Meta, DeepEquals, meta)

	app = &ct.App{ID: app.ID}
	meta = map[string]string{"foo": "baz", "bar": "foo"}
	app.Meta = meta
	c.Assert(s.c.UpdateAppMeta(app), IsNil)
	c.Assert(app.Meta, DeepEquals, meta)

	app = &ct.App{ID: app.ID}
	meta = map[string]string{}
	app.Meta = meta
	c.Assert(s.c.UpdateAppMeta(app), IsNil)
	c.Assert(app.Meta, DeepEquals, meta)

	app, err := s.c.GetApp(app.ID)
	c.Assert(err, IsNil)
	c.Assert(app.Meta, DeepEquals, meta)
}

func (s *S) createTestArtifact(c *C, in *ct.Artifact) *ct.Artifact {
	if in.Type == "" {
		in.Type = host.ArtifactTypeDocker
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
			Type: host.ArtifactTypeDocker,
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
	if len(in.ArtifactIDs) == 0 {
		in.ArtifactIDs = []string{s.createTestArtifact(c, &ct.Artifact{Type: host.ArtifactTypeDocker}).ID}
		in.LegacyArtifactID = in.ArtifactIDs[0]
	}
	c.Assert(s.c.CreateRelease(in), IsNil)
	return in
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
		defer s.deleteTestFormation(out)
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

		expanded, err := s.c.GetExpandedFormation(appID, release.ID)
		c.Assert(err, IsNil)
		c.Assert(expanded.App.ID, Equals, app.ID)
		c.Assert(expanded.Release.ID, Equals, release.ID)
		c.Assert(expanded.ImageArtifact.ID, Equals, release.ImageArtifactID())
		c.Assert(expanded.Processes, DeepEquals, out.Processes)

		_, err = s.c.GetFormation(appID, release.ID+"fail")
		c.Assert(err, Equals, controller.ErrNotFound)
	}
}

func (s *S) createTestFormation(c *C, formation *ct.Formation) *ct.Formation {
	c.Assert(s.c.PutFormation(formation), IsNil)
	return formation
}

func (s *S) deleteTestFormation(formation *ct.Formation) {
	s.c.DeleteFormation(formation.AppID, formation.ReleaseID)
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

func (s *S) TestFlynnArtifact(c *C) {
	artifact := &ct.Artifact{
		Type: ct.ArtifactTypeFlynn,
		URI:  "http://example.com/manifest.json",
	}
	err := s.c.CreateArtifact(artifact)
	c.Assert(err, NotNil)
	c.Assert(hh.IsValidationError(err), Equals, true)

	artifact.Manifest = &ct.ImageManifest{
		Type: ct.ImageManifestTypeV1,
	}
	c.Assert(s.c.CreateArtifact(artifact), IsNil)

	gotArtifact, err := s.c.GetArtifact(artifact.ID)
	c.Assert(err, IsNil)
	c.Assert(gotArtifact, DeepEquals, artifact)
}

func (s *S) TestFileArtifact(c *C) {
	artifact := &ct.Artifact{
		Type: host.ArtifactTypeFile,
		URI:  "http://example.com/slug.tgz",
	}
	c.Assert(s.c.CreateArtifact(artifact), IsNil)

	gotArtifact, err := s.c.GetArtifact(artifact.ID)
	c.Assert(err, IsNil)
	c.Assert(gotArtifact, DeepEquals, artifact)
}

func (s *S) TestAppReleaseList(c *C) {
	app := s.createTestApp(c, &ct.App{Name: "app-release-list"})

	// create 2 releases with formations
	releases := make([]*ct.Release, 2)
	for i := 0; i < 2; i++ {
		r := s.createTestRelease(c, &ct.Release{})
		releases[i] = r
		s.createTestFormation(c, &ct.Formation{ReleaseID: r.ID, AppID: app.ID})
	}

	// create a release with no formation
	s.createTestRelease(c, &ct.Release{})

	// create a release for a different app
	r := s.createTestRelease(c, &ct.Release{})
	a := s.createTestApp(c, &ct.App{})
	s.createTestFormation(c, &ct.Formation{ReleaseID: r.ID, AppID: a.ID})

	// check only the first two releases are returned, and in descending order
	list, err := s.c.AppReleaseList(app.ID)
	c.Assert(err, IsNil)
	c.Assert(list, HasLen, len(releases))
	c.Assert(list[0], DeepEquals, releases[1])
	c.Assert(list[1], DeepEquals, releases[0])
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

func (s *S) TestGetCACertWithAuth(c *C) {
	cert, err := s.c.GetCACert()
	c.Assert(err, IsNil)
	c.Assert(cert, DeepEquals, s.caCert)
}

func (s *S) TestGetCACertWithInvalidAuth(c *C) {
	client, err := controller.NewClient(s.srv.URL, "invalid-key")
	c.Assert(err, IsNil)
	cert, err := client.GetCACert()
	c.Assert(err, Not(IsNil))
	c.Assert(len(cert), Equals, 0)
	c.Assert(strings.Contains(err.Error(), "unexpected status 401"), Equals, true)
}

func (s *S) TestGetCACertWithoutAuth(c *C) {
	client, err := controller.NewClient(s.srv.URL, "")
	c.Assert(err, IsNil)
	cert, err := client.GetCACert()
	c.Assert(err, IsNil)
	c.Assert(cert, DeepEquals, s.caCert)
}
