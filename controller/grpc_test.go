package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/controller/api"
	"github.com/flynn/flynn/controller/data"
	tu "github.com/flynn/flynn/controller/testutils"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	. "github.com/flynn/go-check"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/jackc/pgx"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

type GRPCSuite struct {
	db                  *postgres.DB
	api                 *controllerAPI
	grpc                api.ControllerClient
	grpcNoAuth          api.ControllerClient
	httpSrv             *httptest.Server
	tearDownFns         []func()
	scaleRequestNameMap map[string]string
}

var _ = Suite(&GRPCSuite{})

func (s *GRPCSuite) SetUpSuite(c *C) {
	dbname := "controller_grpc_test"
	db := setupTestDB(c, dbname)
	if err := data.MigrateDB(db); err != nil {
		c.Fatal(err)
	}

	// reconnect with que statements prepared now that schema is migrated
	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig: pgx.ConnConfig{
			Host:     "/var/run/postgresql",
			Database: dbname,
		},
		AfterConnect: data.PrepareStatements,
	})
	if err != nil {
		c.Fatal(err)
	}
	s.db = postgres.New(pgxpool, nil)

	authKeys := []string{"test-3bebef7a2bed81017fb2bfa411a6a0a2"}
	handler, grpcServer, ctrlAPI := appHandler(handlerConfig{
		db:     s.db,
		cc:     tu.NewFakeCluster(),
		lc:     newFakeLogAggregatorClient(),
		keys:   authKeys,
		keyIDs: []string{"test-auth-key"},
	})
	s.api = ctrlAPI
	s.httpSrv = httptest.NewServer(handler)
	s.onTeardown(func() { s.httpSrv.Close() })
	grpcListener := bufconn.Listen(bufSize)
	s.onTeardown(func() { grpcListener.Close() })
	go grpcServer.Serve(grpcListener)

	grpcClient := func(opts ...grpc.DialOption) api.ControllerClient {
		opts = append(opts, grpc.WithInsecure())
		opts = append(opts, grpc.WithDialer(func(string, time.Duration) (net.Conn, error) {
			return grpcListener.Dial()
		}))
		conn, err := grpc.Dial(grpcListener.Addr().String(), opts...)
		if err != nil {
			c.Fatalf("did not connect to server: %v", err)
		}
		s.onTeardown(func() { conn.Close() })
		return api.NewControllerClient(conn)
	}

	// Set up an authenticated connection to the server
	s.grpc = grpcClient(api.WithAuthKey(authKeys[0]))

	// Set up an unauthenticated connection to the server
	s.grpcNoAuth = grpcClient()

	// Wait for the server to start
	var req empty.Empty
	if _, err := s.grpc.Status(context.Background(), &req); err != nil {
		c.Fatal(err)
	}
}

func (s *GRPCSuite) onTeardown(f func()) {
	s.tearDownFns = append(s.tearDownFns, f)
}

func (s *GRPCSuite) TearDownSuite(c *C) {
	for _, fn := range s.tearDownFns {
		fn()
	}
}

func (s *GRPCSuite) SetUpTest(c *C) {
	c.Assert(s.db.Exec(`
		TRUNCATE
			apps, artifacts, deployments, events,
			formations, releases, scale_requests,
			job_cache
		CASCADE
	`), IsNil)
	s.scaleRequestNameMap = make(map[string]string)
}

func isErrCanceled(err error) bool {
	if s, ok := status.FromError(err); ok {
		if s.Code() == codes.Canceled {
			return true
		}
	}
	return false
}

func isErrDeadlineExceeded(err error) bool {
	if s, ok := status.FromError(err); ok {
		if s.Code() == codes.DeadlineExceeded {
			return true
		}
	}
	return false
}

func (s *GRPCSuite) createTestApp(c *C, app *api.App) *api.App {
	ctApp := app.ControllerType()
	err := s.api.appRepo.Add(ctApp)
	c.Assert(err, IsNil)
	return api.NewApp(ctApp)
}

func (s *GRPCSuite) updateTestApp(c *C, app *api.App) *api.App {
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	app, err := s.grpc.UpdateApp(ctx, &api.UpdateAppRequest{App: app})
	c.Assert(err, IsNil)
	return app
}

func (s *GRPCSuite) setTestAppRelease(c *C, app *api.App, releaseName string) *api.App {
	ctApp := app.ControllerType()
	releaseID := api.ParseIDFromName(releaseName, "releases")
	c.Assert(s.api.appRepo.SetRelease(ctApp, releaseID), IsNil)
	return api.NewApp(ctApp)
}

func (s *GRPCSuite) createTestRelease(c *C, parentName string, release *api.Release) *api.Release {
	ctRelease := release.ControllerType()
	ctRelease.AppID = api.ParseIDFromName(parentName, "apps")
	err := s.api.releaseRepo.Add(ctRelease)
	c.Assert(err, IsNil)
	return api.NewRelease(ctRelease)
}

func (s *GRPCSuite) createTestDeployment(c *C, releaseName string) *api.ExpandedDeployment {
	appID := api.ParseIDFromName(releaseName, "apps")
	releaseID := api.ParseIDFromName(releaseName, "releases")
	ctDeployment, err := s.api.deploymentRepo.AddExpanded(&ct.CreateDeploymentConfig{
		AppID:     appID,
		ReleaseID: releaseID,
	})
	c.Assert(err, IsNil)
	return api.NewExpandedDeployment(ctDeployment)
}

func (s *GRPCSuite) createTestDeploymentEvent(c *C, d *api.ExpandedDeployment, e *ct.DeploymentEvent) {
	e.AppID = api.ParseIDFromName(d.Name, "apps")
	e.DeploymentID = api.ParseIDFromName(d.Name, "deployments")
	e.ReleaseID = api.ParseIDFromName(d.NewRelease.Name, "releases")

	c.Assert(data.CreateEvent(s.db.Exec, &ct.Event{
		AppID:        e.AppID,
		DeploymentID: e.DeploymentID,
		ObjectID:     e.DeploymentID,
		ObjectType:   ct.EventTypeDeployment,
		Op:           ct.EventOpUpdate,
	}, e), IsNil)
}

func (s *GRPCSuite) createTestJob(c *C, d *api.ExpandedDeployment, j *ct.Job) *api.Job {
	j.AppID = api.ParseIDFromName(d.Name, "apps")
	j.DeploymentID = api.ParseIDFromName(d.Name, "deployments")
	j.ReleaseID = api.ParseIDFromName(d.NewRelease.Name, "releases")
	if j.UUID == "" {
		j.UUID = random.UUID()
	}
	if j.State == "" {
		j.State = ct.JobStatePending
	}
	c.Assert(s.api.jobRepo.Add(j), IsNil)
	return api.NewJob(j)
}

func (s *GRPCSuite) createTestArtifact(c *C, in *ct.Artifact) *ct.Artifact {
	if in.Type == "" {
		in.Type = ct.ArtifactTypeFlynn
		in.RawManifest = ct.ImageManifest{
			Type: ct.ImageManifestTypeV1,
		}.RawManifest()
	}
	if in.URI == "" {
		in.URI = fmt.Sprintf("https://example.com/%s", random.String(8))
	}
	c.Assert(s.api.artifactRepo.Add(in), IsNil)
	return in
}

func (s *GRPCSuite) createTestScaleRequest(c *C, req *api.CreateScaleRequest) *api.ScaleRequest {
	ctReq := req.ControllerType()
	ctReq, err := s.api.formationRepo.AddScaleRequest(ctReq, false)
	c.Assert(err, IsNil)
	scale := api.NewScaleRequest(ctReq)
	s.scaleRequestNameMap[scale.Name] = fmt.Sprintf("testScale%d", len(s.scaleRequestNameMap)+1)
	return scale
}

func (s *GRPCSuite) createTestScaleRequestForDeployment(c *C, req *api.CreateScaleRequest, deploymentName string) *api.ScaleRequest {
	ctReq := req.ControllerType()
	ctReq.DeploymentID = api.ParseIDFromName(deploymentName, "deployments")
	ctReq, err := s.api.formationRepo.AddScaleRequest(ctReq, false)
	c.Assert(err, IsNil)
	scale := api.NewScaleRequest(ctReq)
	return scale
}

func (s *GRPCSuite) updateTestScaleRequest(c *C, req *api.ScaleRequest) {
	ctReq := req.ControllerType()
	c.Assert(s.api.formationRepo.UpdateScaleRequest(ctReq), IsNil)
}

func (s *GRPCSuite) TestOptionsRequest(c *C) { // grpc-web
	req, err := http.NewRequest("OPTIONS", s.httpSrv.URL+"/grpc-web/fake", nil)
	c.Assert(err, IsNil)
	req.Header.Set("Origin", "http://localhost:3333")
	req.Header.Set("Access-Control-Request-Method", "POST")
	allowHeaders := []string{"Content-Type", "X-GRPC-Web"}
	req.Header.Set("Access-Control-Request-Headers", strings.Join(allowHeaders, ","))
	res, err := http.DefaultClient.Do(req)
	c.Assert(err, IsNil)
	c.Assert(res.StatusCode, Equals, 200)
	c.Assert(res.Header.Get("Access-Control-Allow-Credentials"), Equals, "true")
	accessControlAllowHeaders := strings.ToLower(res.Header.Get("Access-Control-Allow-Headers"))
	for _, h := range allowHeaders {
		c.Assert(strings.Contains(accessControlAllowHeaders, strings.ToLower(h)), Equals, true)
	}
	c.Assert(res.Header.Get("Access-Control-Allow-Origin"), Equals, req.Header.Get("Origin"))
	c.Assert(res.Header.Get("Access-Control-Allow-Methods"), Matches, ".*POST.*")
}

func (s *GRPCSuite) TestUnauthenticated(c *C) {
	ctx, _ := context.WithTimeout(context.Background(), 200*time.Millisecond)
	stream, err := s.grpcNoAuth.StreamApps(ctx, &api.StreamAppsRequest{})
	c.Assert(err, IsNil)
	_, err = stream.Recv()
	c.Assert(err, Not(IsNil))
	errStatus := status.Convert(err)
	c.Assert(errStatus.Code(), Equals, codes.Unauthenticated)
}

func unaryReceiveApps(s *GRPCSuite, c *C, req *api.StreamAppsRequest) (res *api.StreamAppsResponse, receivedEOF bool) {
	ctx, ctxCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer func() {
		if !receivedEOF {
			ctxCancel()
		}
	}()
	stream, err := s.grpc.StreamApps(ctx, req)
	c.Assert(err, IsNil)
	for i := 0; i < 2; i++ {
		r, err := stream.Recv()
		if err == io.EOF {
			receivedEOF = true
			return
		}
		if isErrCanceled(err) {
			return
		}
		c.Assert(err, IsNil)
		res = r
	}
	return
}

func (s *GRPCSuite) TestStreamApps(c *C) {
	testApp1 := s.createTestApp(c, &api.App{DisplayName: "test1"})
	testApp2 := s.createTestApp(c, &api.App{DisplayName: "test2", Labels: map[string]string{"test.labels-filter": "include"}})
	testApp3 := s.createTestApp(c, &api.App{DisplayName: "test3", Labels: map[string]string{"test.labels-filter": "exclude"}})

	streamAppsWithCancel := func(req *api.StreamAppsRequest) (api.Controller_StreamAppsClient, context.CancelFunc) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		stream, err := s.grpc.StreamApps(ctx, req)
		c.Assert(err, IsNil)
		return stream, cancel
	}

	receiveAppsStream := func(stream api.Controller_StreamAppsClient) *api.StreamAppsResponse {
		res, err := stream.Recv()
		if err == io.EOF || isErrCanceled(err) || isErrDeadlineExceeded(err) {
			return nil
		}
		c.Assert(err, IsNil)
		return res
	}

	// test fetching a single app
	res, receivedEOF := unaryReceiveApps(s, c, &api.StreamAppsRequest{PageSize: 1})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Apps), Equals, 1)
	c.Assert(res.Apps[0], DeepEquals, testApp3)
	c.Assert(receivedEOF, Equals, true)

	// test fetching a sinple app by name
	res, receivedEOF = unaryReceiveApps(s, c, &api.StreamAppsRequest{NameFilters: []string{testApp2.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Apps), Equals, 1)
	c.Assert(res.Apps[0], DeepEquals, testApp2)
	c.Assert(receivedEOF, Equals, true)

	// test fetching multiple apps by name
	res, receivedEOF = unaryReceiveApps(s, c, &api.StreamAppsRequest{NameFilters: []string{testApp1.Name, testApp2.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Apps), Equals, 2)
	c.Assert(res.Apps[0], DeepEquals, testApp2)
	c.Assert(res.Apps[1], DeepEquals, testApp1)
	c.Assert(receivedEOF, Equals, true)

	// test filtering by labels [OP_NOT_IN]
	res, receivedEOF = unaryReceiveApps(s, c, &api.StreamAppsRequest{LabelFilters: []*api.LabelFilter{
		{
			Expressions: []*api.LabelFilter_Expression{
				{
					Key:    "test.labels-filter",
					Op:     api.LabelFilter_Expression_OP_NOT_IN,
					Values: []string{"exclude"},
				},
			},
		},
	}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Apps), Equals, 2)
	// testApp3 has test.labels-filter: exclude
	c.Assert(res.Apps[0], DeepEquals, testApp2) // has test.labels-filter: include
	c.Assert(res.Apps[1], DeepEquals, testApp1) // has no labels
	c.Assert(receivedEOF, Equals, true)

	// test filtering by labels [OP_IN]
	res, receivedEOF = unaryReceiveApps(s, c, &api.StreamAppsRequest{LabelFilters: []*api.LabelFilter{
		{
			Expressions: []*api.LabelFilter_Expression{
				{
					Key:    "test.labels-filter",
					Op:     api.LabelFilter_Expression_OP_IN,
					Values: []string{"include"},
				},
			},
		},
	}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Apps), Equals, 1)
	// testApp3 has test.labels-filter: exclude
	c.Assert(res.Apps[0], DeepEquals, testApp2) // has test.labels-filter: include
	// testApp1 has no labels
	c.Assert(receivedEOF, Equals, true)

	// test filtering by labels [OP_EXISTS]
	res, receivedEOF = unaryReceiveApps(s, c, &api.StreamAppsRequest{LabelFilters: []*api.LabelFilter{
		{
			Expressions: []*api.LabelFilter_Expression{
				{
					Key: "test.labels-filter",
					Op:  api.LabelFilter_Expression_OP_EXISTS,
				},
			},
		},
	}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Apps), Equals, 2)
	c.Assert(res.Apps[0], DeepEquals, testApp3) // has test.labels-filter
	c.Assert(res.Apps[1], DeepEquals, testApp2) // has test.labels-filter
	// testApp1 has no labels
	c.Assert(receivedEOF, Equals, true)

	// test filtering by labels [OP_NOT_EXISTS]
	res, receivedEOF = unaryReceiveApps(s, c, &api.StreamAppsRequest{LabelFilters: []*api.LabelFilter{
		{
			Expressions: []*api.LabelFilter_Expression{
				{
					Key: "test.labels-filter",
					Op:  api.LabelFilter_Expression_OP_NOT_EXISTS,
				},
			},
		},
	}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Apps), Equals, 1)
	// testApp3 and testApp2 both have test.labels-filter
	c.Assert(res.Apps[0], DeepEquals, testApp1) // has no labels
	c.Assert(receivedEOF, Equals, true)

	// test filtering by labels with pagination [OP_NOT_EXISTS]
	res, receivedEOF = unaryReceiveApps(s, c, &api.StreamAppsRequest{PageSize: 1, LabelFilters: []*api.LabelFilter{
		{
			Expressions: []*api.LabelFilter_Expression{
				{
					Key: "test.labels-filter",
					Op:  api.LabelFilter_Expression_OP_NOT_EXISTS,
				},
			},
		},
	}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Apps), Equals, 1)
	// testApp3 and testApp2 both have test.labels-filter
	c.Assert(res.Apps[0], DeepEquals, testApp1) // has no labels
	c.Assert(receivedEOF, Equals, true)

	// test streaming updates
	stream, cancel := streamAppsWithCancel(&api.StreamAppsRequest{NameFilters: []string{testApp1.Name}, StreamUpdates: true})
	res = receiveAppsStream(stream)
	c.Assert(res, Not(IsNil))
	c.Assert(res.Apps[0], DeepEquals, testApp1)
	testApp4 := s.createTestApp(c, &api.App{DisplayName: "test4"}) // through in a create to test that the we get the update and not the create
	testApp1.Labels = map[string]string{"test.one": "1"}
	updatedTestApp1 := s.updateTestApp(c, testApp1)
	c.Assert(updatedTestApp1.Labels, DeepEquals, testApp1.Labels)
	res = receiveAppsStream(stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Apps), Equals, 1)
	c.Assert(res.Apps[0], DeepEquals, updatedTestApp1)
	cancel()

	// test streaming updates that don't match the LabelFilters [OP_EXISTS]
	stream, cancel = streamAppsWithCancel(&api.StreamAppsRequest{LabelFilters: []*api.LabelFilter{
		{
			Expressions: []*api.LabelFilter_Expression{
				{
					Key: "test.labels-filter-update",
					Op:  api.LabelFilter_Expression_OP_EXISTS,
				},
			},
		},
	}, StreamUpdates: true})
	receiveAppsStream(stream) // initial page
	testApp1.Labels = map[string]string{"test.labels": "exclude me"}
	updatedTestApp1 = s.updateTestApp(c, testApp1)
	c.Assert(updatedTestApp1.Labels, DeepEquals, testApp1.Labels)
	res = receiveAppsStream(stream)
	c.Assert(res, IsNil)
	cancel()

	// test streaming updates without filters
	stream, cancel = streamAppsWithCancel(&api.StreamAppsRequest{StreamUpdates: true})
	receiveAppsStream(stream)                                      // initial page
	testApp5 := s.createTestApp(c, &api.App{DisplayName: "test5"}) // through in a create to test that the we get the update and not the create
	testApp1.Labels = map[string]string{"test.two": "2"}
	updatedTestApp1 = s.updateTestApp(c, testApp1)
	c.Assert(updatedTestApp1.Labels, DeepEquals, testApp1.Labels)
	res = receiveAppsStream(stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Apps), Equals, 1)
	c.Assert(res.Apps[0], DeepEquals, updatedTestApp1)
	cancel()

	// test streaming updates (new release)
	stream, cancel = streamAppsWithCancel(&api.StreamAppsRequest{StreamUpdates: true})
	receiveAppsStream(stream) // initial page
	testRelease1 := s.createTestRelease(c, testApp5.Name, &api.Release{})
	testApp5 = s.setTestAppRelease(c, testApp5, testRelease1.Name)
	res = receiveAppsStream(stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Apps), Equals, 1)
	c.Assert(res.Apps[0], DeepEquals, testApp5)
	c.Assert(res.Apps[0].Release, Equals, testRelease1.Name)
	cancel()

	// test streaming creates
	stream, cancel = streamAppsWithCancel(&api.StreamAppsRequest{StreamCreates: true})
	receiveAppsStream(stream) // initial page
	testApp1.Labels = map[string]string{"test.three": "3"}
	updatedTestApp1 = s.updateTestApp(c, testApp1) // through in a update to test that we get the create and not the update
	testApp6 := s.createTestApp(c, &api.App{DisplayName: "test6"})
	res = receiveAppsStream(stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Apps), Equals, 1)
	c.Assert(res.Apps[0], DeepEquals, testApp6)
	cancel()

	// test streaming creates that don't match the NameFilters
	stream, cancel = streamAppsWithCancel(&api.StreamAppsRequest{NameFilters: []string{testApp1.Name}, StreamCreates: true})
	receiveAppsStream(stream) // initial page
	testApp1.Labels = map[string]string{"test.four": "4"}
	updatedTestApp1 = s.updateTestApp(c, testApp1) // through in a update to test that we get the create and not the update
	testApp7 := s.createTestApp(c, &api.App{DisplayName: "test7"})
	res = receiveAppsStream(stream)
	c.Assert(res, IsNil)
	cancel()

	// test streaming creates that don't match the LabelFilters [OP_EXISTS]
	stream, cancel = streamAppsWithCancel(&api.StreamAppsRequest{LabelFilters: []*api.LabelFilter{
		{
			Expressions: []*api.LabelFilter_Expression{
				{
					Key: "test.labels-filter-create",
					Op:  api.LabelFilter_Expression_OP_EXISTS,
				},
			},
		},
	}, StreamCreates: true})
	receiveAppsStream(stream) // initial page
	testApp1.Labels = map[string]string{"test.four": "5"}
	updatedTestApp1 = s.updateTestApp(c, testApp1) // through in a update to test that we get the create and not the update
	testApp8 := s.createTestApp(c, &api.App{DisplayName: "test8"})
	res = receiveAppsStream(stream)
	c.Assert(res, IsNil)
	cancel()

	// test unary pagination
	res, receivedEOF = unaryReceiveApps(s, c, &api.StreamAppsRequest{PageSize: 1})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Apps), Equals, 1)
	c.Assert(res.Apps[0].DisplayName, DeepEquals, testApp8.DisplayName)
	c.Assert(receivedEOF, Equals, true)
	c.Assert(res.NextPageToken, Not(Equals), "")
	c.Assert(res.PageComplete, Equals, true)
	for i, testApp := range []*api.App{testApp7, testApp6, testApp5, testApp4, testApp3} {
		comment := Commentf("iteraction %d", i)
		res, receivedEOF = unaryReceiveApps(s, c, &api.StreamAppsRequest{PageSize: 1, PageToken: res.NextPageToken})
		c.Assert(res, Not(IsNil), comment)
		c.Assert(len(res.Apps), Equals, 1, comment)
		c.Assert(res.Apps[0].DisplayName, DeepEquals, testApp.DisplayName, comment)
		c.Assert(receivedEOF, Equals, true, comment)
		c.Assert(res.NextPageToken, Not(Equals), "", comment)
		c.Assert(res.PageComplete, Equals, true, comment)
	}
	res, receivedEOF = unaryReceiveApps(s, c, &api.StreamAppsRequest{PageSize: 2, PageToken: res.NextPageToken})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Apps), Equals, 2)
	c.Assert(res.Apps[0], DeepEquals, testApp2)
	c.Assert(res.Apps[1].DisplayName, DeepEquals, testApp1.DisplayName)
	c.Assert(receivedEOF, Equals, true)
	c.Assert(res.NextPageToken, Equals, "")
	c.Assert(res.PageComplete, Equals, true)
}

func (s *GRPCSuite) TestStreamAppsUnaryPagination(c *C) {
	testApp1 := s.createTestApp(c, &api.App{DisplayName: "test1"})
	testApp2 := s.createTestApp(c, &api.App{DisplayName: "test2"})
	testApp3 := s.createTestApp(c, &api.App{DisplayName: "test3"})
	testApp4 := s.createTestApp(c, &api.App{DisplayName: "test4"})
	testApp5 := s.createTestApp(c, &api.App{DisplayName: "test5"})
	testApp6 := s.createTestApp(c, &api.App{DisplayName: "test6"})
	testApp7 := s.createTestApp(c, &api.App{DisplayName: "test7"})
	testApp8 := s.createTestApp(c, &api.App{DisplayName: "test8"})

	// page 1
	res, receivedEOF := unaryReceiveApps(s, c, &api.StreamAppsRequest{PageSize: 3})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Apps), Equals, 3)
	c.Assert(receivedEOF, Equals, true)
	c.Assert(res.Apps[0].DisplayName, DeepEquals, testApp8.DisplayName)
	c.Assert(res.Apps[1].DisplayName, DeepEquals, testApp7.DisplayName)
	c.Assert(res.Apps[2].DisplayName, DeepEquals, testApp6.DisplayName)
	c.Assert(res.NextPageToken, Not(Equals), "")
	c.Assert(res.PageComplete, Equals, true)

	// page 2
	res, receivedEOF = unaryReceiveApps(s, c, &api.StreamAppsRequest{PageToken: res.NextPageToken})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Apps), Equals, 3)
	c.Assert(receivedEOF, Equals, true)
	c.Assert(res.Apps[0].DisplayName, DeepEquals, testApp5.DisplayName)
	c.Assert(res.Apps[1].DisplayName, DeepEquals, testApp4.DisplayName)
	c.Assert(res.Apps[2].DisplayName, DeepEquals, testApp3.DisplayName)
	c.Assert(res.NextPageToken, Not(Equals), "")
	c.Assert(res.PageComplete, Equals, true)

	// page 3 (last)
	res, receivedEOF = unaryReceiveApps(s, c, &api.StreamAppsRequest{PageToken: res.NextPageToken})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Apps), Equals, 2)
	c.Assert(receivedEOF, Equals, true)
	c.Assert(res.Apps[0].DisplayName, DeepEquals, testApp2.DisplayName)
	c.Assert(res.Apps[1].DisplayName, DeepEquals, testApp1.DisplayName)
	c.Assert(res.NextPageToken, Equals, "")
	c.Assert(res.PageComplete, Equals, true)
}

func (s *GRPCSuite) TestStreamAppsPaginationWithUpdates(c *C) {
	// TODO(jvatic): test paginating with StreamUpdates set
}

func unaryReceiveReleases(s *GRPCSuite, c *C, req *api.StreamReleasesRequest) (res *api.StreamReleasesResponse, receivedEOF bool) {
	ctx, ctxCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer func() {
		if !receivedEOF {
			ctxCancel()
		}
	}()
	stream, err := s.grpc.StreamReleases(ctx, req)
	c.Assert(err, IsNil)
	for i := 0; i < 2; i++ {
		r, err := stream.Recv()
		if err == io.EOF {
			receivedEOF = true
			return
		}
		if isErrCanceled(err) {
			return
		}
		c.Assert(err, IsNil)
		res = r
	}
	return
}

func (s *GRPCSuite) TestStreamReleases(c *C) {
	testApp1 := s.createTestApp(c, &api.App{DisplayName: "test1"})
	testApp2 := s.createTestApp(c, &api.App{DisplayName: "test2"})
	testApp3 := s.createTestApp(c, &api.App{DisplayName: "test3"})
	testApp4 := s.createTestApp(c, &api.App{DisplayName: "test4"})
	testRelease1 := s.createTestRelease(c, testApp2.Name, &api.Release{Env: map[string]string{"ONE": "1"}, Labels: map[string]string{"test.int": "1"}})
	testRelease2 := s.createTestRelease(c, testApp1.Name, &api.Release{Env: map[string]string{"TWO": "2"}, Labels: map[string]string{"test.int": "2"}})
	testRelease3 := s.createTestRelease(c, testApp1.Name, &api.Release{Env: map[string]string{"THREE": "3"}, Labels: map[string]string{"test.string": "foo"}})
	testRelease4 := s.createTestRelease(c, testApp3.Name, &api.Release{Env: map[string]string{"FOUR": "4"}, Labels: map[string]string{"test.string": "bar", "test.int": "4"}})

	streamReleasesWithCancel := func(req *api.StreamReleasesRequest) (api.Controller_StreamReleasesClient, context.CancelFunc) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		stream, err := s.grpc.StreamReleases(ctx, req)
		c.Assert(err, IsNil)
		return stream, cancel
	}

	receiveReleasesStream := func(stream api.Controller_StreamReleasesClient) *api.StreamReleasesResponse {
		res, err := stream.Recv()
		if err == io.EOF || isErrCanceled(err) || isErrDeadlineExceeded(err) {
			return nil
		}
		c.Assert(err, IsNil)
		return res
	}

	// test fetching the latest release
	res, receivedEOF := unaryReceiveReleases(s, c, &api.StreamReleasesRequest{PageSize: 1})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Releases), Equals, 1)
	c.Assert(strings.HasPrefix(res.Releases[0].Name, testApp3.Name), Equals, true)
	c.Assert(res.Releases[0], DeepEquals, testRelease4)
	c.Assert(receivedEOF, Equals, true)

	// test fetching a single release by name
	res, receivedEOF = unaryReceiveReleases(s, c, &api.StreamReleasesRequest{NameFilters: []string{testRelease2.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Releases), Equals, 1)
	c.Assert(strings.HasPrefix(res.Releases[0].Name, testApp1.Name), Equals, true)
	c.Assert(res.Releases[0], DeepEquals, testRelease2)
	c.Assert(receivedEOF, Equals, true)

	// test fetching a single release by name with page size set to 1
	res, receivedEOF = unaryReceiveReleases(s, c, &api.StreamReleasesRequest{PageSize: 1, NameFilters: []string{testRelease2.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Releases), Equals, 1)
	c.Assert(strings.HasPrefix(res.Releases[0].Name, testApp1.Name), Equals, true)
	c.Assert(res.Releases[0], DeepEquals, testRelease2)
	c.Assert(receivedEOF, Equals, true)

	// test fetching a single release by app name
	res, receivedEOF = unaryReceiveReleases(s, c, &api.StreamReleasesRequest{NameFilters: []string{testApp2.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Releases), Equals, 1)
	c.Assert(strings.HasPrefix(res.Releases[0].Name, testApp2.Name), Equals, true)
	c.Assert(res.Releases[0], DeepEquals, testRelease1)
	c.Assert(receivedEOF, Equals, true)

	// test fetching multiple releases by name
	res, receivedEOF = unaryReceiveReleases(s, c, &api.StreamReleasesRequest{NameFilters: []string{testRelease1.Name, testRelease2.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Releases), Equals, 2)
	c.Assert(strings.HasPrefix(res.Releases[0].Name, testApp1.Name), Equals, true)
	c.Assert(res.Releases[0], DeepEquals, testRelease2)
	c.Assert(strings.HasPrefix(res.Releases[1].Name, testApp2.Name), Equals, true)
	c.Assert(res.Releases[1], DeepEquals, testRelease1)
	c.Assert(receivedEOF, Equals, true)

	// test fetching multiple releases by app name
	res, receivedEOF = unaryReceiveReleases(s, c, &api.StreamReleasesRequest{NameFilters: []string{testApp1.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Releases), Equals, 2)
	c.Assert(strings.HasPrefix(res.Releases[0].Name, testApp1.Name), Equals, true)
	c.Assert(res.Releases[0], DeepEquals, testRelease3)
	c.Assert(strings.HasPrefix(res.Releases[1].Name, testApp1.Name), Equals, true)
	c.Assert(res.Releases[1], DeepEquals, testRelease2)
	c.Assert(receivedEOF, Equals, true)

	// test fetching multiple releases by a mixture of app name and release name
	res, receivedEOF = unaryReceiveReleases(s, c, &api.StreamReleasesRequest{NameFilters: []string{testApp1.Name, testRelease2.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Releases), Equals, 1)
	c.Assert(strings.HasPrefix(res.Releases[0].Name, testApp1.Name), Equals, true)
	c.Assert(res.Releases[0], DeepEquals, testRelease2)
	c.Assert(receivedEOF, Equals, true)

	// test filtering by labels [OP_IN]
	res, receivedEOF = unaryReceiveReleases(s, c, &api.StreamReleasesRequest{LabelFilters: []*api.LabelFilter{
		{
			Expressions: []*api.LabelFilter_Expression{
				{
					Key:    "test.int",
					Op:     api.LabelFilter_Expression_OP_IN,
					Values: []string{"1", "2"},
				},
			},
		},
	}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Releases), Equals, 2)
	c.Assert(strings.HasPrefix(res.Releases[0].Name, testApp1.Name), Equals, true)
	c.Assert(res.Releases[0], DeepEquals, testRelease2)
	c.Assert(strings.HasPrefix(res.Releases[1].Name, testApp2.Name), Equals, true)
	c.Assert(res.Releases[1], DeepEquals, testRelease1)
	c.Assert(receivedEOF, Equals, true)

	// test filtering by labels [OP_NOT_IN]
	res, receivedEOF = unaryReceiveReleases(s, c, &api.StreamReleasesRequest{LabelFilters: []*api.LabelFilter{
		{
			Expressions: []*api.LabelFilter_Expression{
				{
					Key:    "test.int",
					Op:     api.LabelFilter_Expression_OP_NOT_IN,
					Values: []string{"2", "4"},
				}, // AND
				{
					Key:    "test.string",
					Op:     api.LabelFilter_Expression_OP_NOT_IN,
					Values: []string{"foo", "bar"},
				},
			},
		},
	}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Releases), Equals, 1)
	c.Assert(strings.HasPrefix(res.Releases[0].Name, testApp2.Name), Equals, true)
	c.Assert(res.Releases[0], DeepEquals, testRelease1)
	c.Assert(receivedEOF, Equals, true)

	// test filtering by labels [OP_EXISTS]
	res, receivedEOF = unaryReceiveReleases(s, c, &api.StreamReleasesRequest{LabelFilters: []*api.LabelFilter{
		{
			Expressions: []*api.LabelFilter_Expression{
				{
					Key: "test.int",
					Op:  api.LabelFilter_Expression_OP_EXISTS,
				}, // AND
				{
					Key: "test.string",
					Op:  api.LabelFilter_Expression_OP_EXISTS,
				},
			},
		},
	}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Releases), Equals, 1)
	c.Assert(strings.HasPrefix(res.Releases[0].Name, testApp3.Name), Equals, true)
	c.Assert(res.Releases[0], DeepEquals, testRelease4)
	c.Assert(receivedEOF, Equals, true)

	// test filtering by labels [OP_NOT_EXISTS]
	res, receivedEOF = unaryReceiveReleases(s, c, &api.StreamReleasesRequest{LabelFilters: []*api.LabelFilter{
		{
			Expressions: []*api.LabelFilter_Expression{
				{
					Key: "test.int",
					Op:  api.LabelFilter_Expression_OP_NOT_EXISTS,
				},
			},
		}, // OR
		{
			Expressions: []*api.LabelFilter_Expression{
				{
					Key: "test.string",
					Op:  api.LabelFilter_Expression_OP_NOT_EXISTS,
				},
			},
		},
	}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Releases), Equals, 3)
	c.Assert(strings.HasPrefix(res.Releases[0].Name, testApp1.Name), Equals, true)
	c.Assert(res.Releases[0], DeepEquals, testRelease3)
	c.Assert(strings.HasPrefix(res.Releases[1].Name, testApp1.Name), Equals, true)
	c.Assert(res.Releases[1], DeepEquals, testRelease2)
	c.Assert(strings.HasPrefix(res.Releases[2].Name, testApp2.Name), Equals, true)
	c.Assert(res.Releases[2], DeepEquals, testRelease1)
	c.Assert(receivedEOF, Equals, true)

	// test streaming creates for specific app
	stream, cancel := streamReleasesWithCancel(&api.StreamReleasesRequest{NameFilters: []string{testApp4.Name}, StreamCreates: true})
	receiveReleasesStream(stream) // initial page
	testRelease5 := s.createTestRelease(c, testApp3.Name, &api.Release{Env: map[string]string{"Five": "5"}, Labels: map[string]string{"test.string": "baz"}})
	testRelease6 := s.createTestRelease(c, testApp4.Name, &api.Release{Env: map[string]string{"Six": "6"}, Labels: map[string]string{"test.string": "biz"}})
	res = receiveReleasesStream(stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Releases), Equals, 1)
	c.Assert(res.Releases[0], DeepEquals, testRelease6)
	cancel()

	// test creates are not streamed when flag not set
	stream, cancel = streamReleasesWithCancel(&api.StreamReleasesRequest{NameFilters: []string{testApp4.Name}})
	receiveReleasesStream(stream) // initial page
	testRelease7 := s.createTestRelease(c, testApp4.Name, &api.Release{Env: map[string]string{"Seven": "7"}, Labels: map[string]string{"test.string": "flu"}})
	res = receiveReleasesStream(stream)
	c.Assert(res, IsNil)
	cancel()

	// test streaming creates that don't match the LabelFilters [OP_EXISTS]
	stream, cancel = streamReleasesWithCancel(&api.StreamReleasesRequest{LabelFilters: []*api.LabelFilter{
		{
			Expressions: []*api.LabelFilter_Expression{
				{
					Key: "test.int",
					Op:  api.LabelFilter_Expression_OP_EXISTS,
				},
			},
		},
	}, StreamCreates: true})
	receiveReleasesStream(stream) // initial page
	testRelease8 := s.createTestRelease(c, testApp4.Name, &api.Release{Labels: map[string]string{"test.string": "hue"}})
	res = receiveReleasesStream(stream)
	c.Assert(res, IsNil)
	cancel()

	// test streaming creates that don't match the NameFilters
	stream, cancel = streamReleasesWithCancel(&api.StreamReleasesRequest{NameFilters: []string{testApp4.Name}, StreamCreates: true})
	receiveReleasesStream(stream) // initial page
	testRelease9 := s.createTestRelease(c, testApp3.Name, &api.Release{Labels: map[string]string{"test.string": "bue"}})
	res = receiveReleasesStream(stream)
	c.Assert(res, IsNil)
	cancel()

	// test unary pagination
	res, receivedEOF = unaryReceiveReleases(s, c, &api.StreamReleasesRequest{PageSize: 1})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Releases), Equals, 1)
	c.Assert(res.Releases[0], DeepEquals, testRelease9)
	c.Assert(receivedEOF, Equals, true)
	c.Assert(res.NextPageToken, Not(Equals), "")
	c.Assert(res.PageComplete, Equals, true)
	for i, testRelease := range []*api.Release{testRelease8, testRelease7, testRelease6, testRelease5, testRelease4, testRelease3} {
		comment := Commentf("iteraction %d", i)
		res, receivedEOF = unaryReceiveReleases(s, c, &api.StreamReleasesRequest{PageSize: 1, PageToken: res.NextPageToken})
		c.Assert(res, Not(IsNil), comment)
		c.Assert(len(res.Releases), Equals, 1, comment)
		c.Assert(res.Releases[0], DeepEquals, testRelease, comment)
		c.Assert(receivedEOF, Equals, true, comment)
		c.Assert(res.NextPageToken, Not(Equals), "", comment)
		c.Assert(res.PageComplete, Equals, true, comment)
	}
	res, receivedEOF = unaryReceiveReleases(s, c, &api.StreamReleasesRequest{PageSize: 2, PageToken: res.NextPageToken})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Releases), Equals, 2)
	c.Assert(res.Releases[0], DeepEquals, testRelease2)
	c.Assert(res.Releases[1], DeepEquals, testRelease1)
	c.Assert(receivedEOF, Equals, true)
	c.Assert(res.NextPageToken, Equals, "")
	c.Assert(res.PageComplete, Equals, true)
}

func (s *GRPCSuite) TestStreamReleasesUnaryPagination(c *C) {
	testApp1 := s.createTestApp(c, &api.App{DisplayName: "test1"})
	testRelease1 := s.createTestRelease(c, testApp1.Name, &api.Release{Env: map[string]string{"NO": "1"}})
	testRelease2 := s.createTestRelease(c, testApp1.Name, &api.Release{Env: map[string]string{"NO": "2"}})
	testRelease3 := s.createTestRelease(c, testApp1.Name, &api.Release{Env: map[string]string{"NO": "3"}})
	testRelease4 := s.createTestRelease(c, testApp1.Name, &api.Release{Env: map[string]string{"NO": "4"}})
	testRelease5 := s.createTestRelease(c, testApp1.Name, &api.Release{Env: map[string]string{"NO": "5"}})
	testRelease6 := s.createTestRelease(c, testApp1.Name, &api.Release{Env: map[string]string{"NO": "6"}})
	testRelease7 := s.createTestRelease(c, testApp1.Name, &api.Release{Env: map[string]string{"NO": "7"}})
	testRelease8 := s.createTestRelease(c, testApp1.Name, &api.Release{Env: map[string]string{"NO": "8"}})

	// page 1
	res, receivedEOF := unaryReceiveReleases(s, c, &api.StreamReleasesRequest{PageSize: 3})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Releases), Equals, 3)
	c.Assert(receivedEOF, Equals, true)
	c.Assert(res.Releases[0], DeepEquals, testRelease8)
	c.Assert(res.Releases[1], DeepEquals, testRelease7)
	c.Assert(res.Releases[2], DeepEquals, testRelease6)
	c.Assert(res.NextPageToken, Not(Equals), "")
	c.Assert(res.PageComplete, Equals, true)

	// page 2
	res, receivedEOF = unaryReceiveReleases(s, c, &api.StreamReleasesRequest{PageToken: res.NextPageToken})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Releases), Equals, 3)
	c.Assert(receivedEOF, Equals, true)
	c.Assert(res.Releases[0], DeepEquals, testRelease5)
	c.Assert(res.Releases[1], DeepEquals, testRelease4)
	c.Assert(res.Releases[2], DeepEquals, testRelease3)
	c.Assert(res.NextPageToken, Not(Equals), "")
	c.Assert(res.PageComplete, Equals, true)

	// page 3 (last)
	res, receivedEOF = unaryReceiveReleases(s, c, &api.StreamReleasesRequest{PageToken: res.NextPageToken})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Releases), Equals, 2)
	c.Assert(receivedEOF, Equals, true)
	c.Assert(res.Releases[0], DeepEquals, testRelease2)
	c.Assert(res.Releases[1], DeepEquals, testRelease1)
	c.Assert(res.NextPageToken, Equals, "")
	c.Assert(res.PageComplete, Equals, true)
}

func (s *GRPCSuite) TestStreamReleasesPaginationWithStreamUpdates(c *C) {
	// TODO(jvatic): Test pagination with StreamUpdates set
}

func (s *GRPCSuite) streamScalesWithCancel(c *C, req *api.StreamScalesRequest) (api.Controller_StreamScalesClient, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	stream, err := s.grpc.StreamScales(ctx, req)
	c.Assert(err, IsNil)
	return stream, cancel
}

func (s *GRPCSuite) receiveScalesStream(c *C, stream api.Controller_StreamScalesClient) *api.StreamScalesResponse {
	res, err := stream.Recv()
	if err == io.EOF || isErrCanceled(err) || isErrDeadlineExceeded(err) {
		return nil
	}
	c.Assert(err, IsNil)
	return res
}

func unaryReceiveScales(s *GRPCSuite, c *C, req *api.StreamScalesRequest) (res *api.StreamScalesResponse, receivedEOF bool) {
	ctx, ctxCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer func() {
		if !receivedEOF {
			ctxCancel()
		}
	}()
	stream, err := s.grpc.StreamScales(ctx, req)
	c.Assert(err, IsNil)
	for i := 0; i < 2; i++ {
		r, err := stream.Recv()
		if err == io.EOF {
			receivedEOF = true
			return
		}
		if isErrCanceled(err) {
			return
		}
		c.Assert(err, IsNil)
		res = r
	}
	return
}

func (s *GRPCSuite) TestStreamScales(c *C) {
	testApp1 := s.createTestApp(c, &api.App{DisplayName: "test1"})
	testApp2 := s.createTestApp(c, &api.App{DisplayName: "test2"})
	testApp3 := s.createTestApp(c, &api.App{DisplayName: "test3"})
	testRelease1 := s.createTestRelease(c, testApp1.Name, &api.Release{Labels: map[string]string{"i": "1"}, Processes: map[string]*api.ProcessType{"devnull": {Args: []string{"tail", "-f", "/dev/null"}, Service: "dev"}}})
	testRelease2 := s.createTestRelease(c, testApp2.Name, &api.Release{Labels: map[string]string{"i": "2"}, Processes: map[string]*api.ProcessType{"devnull": {Args: []string{"tail", "-f", "/dev/null"}, Service: "dev"}}})
	testRelease3 := s.createTestRelease(c, testApp2.Name, &api.Release{Labels: map[string]string{"i": "3"}, Processes: map[string]*api.ProcessType{"devnull": {Args: []string{"tail", "-f", "/dev/null"}, Service: "dev"}}})
	testRelease4 := s.createTestRelease(c, testApp1.Name, &api.Release{Labels: map[string]string{"i": "4"}, Processes: map[string]*api.ProcessType{"devnull": {Args: []string{"tail", "-f", "/dev/null"}, Service: "dev"}}})
	testRelease5 := s.createTestRelease(c, testApp3.Name, &api.Release{Labels: map[string]string{"i": "5"}, Processes: map[string]*api.ProcessType{"devnull": {Args: []string{"tail", "-f", "/dev/null"}, Service: "dev"}}})
	testRelease6 := s.createTestRelease(c, testApp3.Name, &api.Release{Labels: map[string]string{"i": "6"}, Processes: map[string]*api.ProcessType{"devnull": {Args: []string{"tail", "-f", "/dev/null"}, Service: "dev"}}})
	testScale1 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease1.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 1}}})
	testScale2 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease2.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 1}}})
	testScale3 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease3.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 2}}})
	testScale4 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease4.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 2}}})
	testScale5 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease5.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 1}}})
	testScale6 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease6.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 2}}})
	testScale7 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease6.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 3}}})

	testScales := []*api.ScaleRequest{
		testScale1,
		testScale2,
		testScale3,
		testScale3,
		testScale4,
		testScale5,
		testScale6,
		testScale7,
	}
	for _, scale := range testScales {
		scale.State = api.ScaleRequestState_SCALE_PENDING
		s.updateTestScaleRequest(c, scale)
	}
	testScale2.State = api.ScaleRequestState_SCALE_COMPLETE
	s.updateTestScaleRequest(c, testScale2)
	testScale4.State = api.ScaleRequestState_SCALE_COMPLETE
	s.updateTestScaleRequest(c, testScale4)

	testScaleDisplayName := func(scale *api.ScaleRequest) string {
		return s.scaleRequestNameMap[scale.Name]
	}
	assertScaleRequestsEqual := func(c *C, a, b *api.ScaleRequest) {
		c.Assert(a.Name, Equals, b.Name, Commentf("expected %s, got %s", testScaleDisplayName(b), testScaleDisplayName(a)))
	}

	// test fetching the latest scale
	res, receivedEOF := unaryReceiveScales(s, c, &api.StreamScalesRequest{PageSize: 1})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 1)
	c.Assert(res.ScaleRequests[0].Name, Equals, testScale7.Name)
	c.Assert(receivedEOF, Equals, true)

	// test fetching a single scale by name
	res, receivedEOF = unaryReceiveScales(s, c, &api.StreamScalesRequest{NameFilters: []string{testScale5.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 1)
	c.Assert(res.ScaleRequests[0].Name, Equals, testScale5.Name)
	c.Assert(receivedEOF, Equals, true)

	// test fetching a single scale by release name
	res, receivedEOF = unaryReceiveScales(s, c, &api.StreamScalesRequest{PageSize: 1, NameFilters: []string{testRelease3.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 1)
	c.Assert(res.ScaleRequests[0].Name, Equals, testScale3.Name)
	c.Assert(receivedEOF, Equals, true)

	// test fetching a single scale by app name
	res, receivedEOF = unaryReceiveScales(s, c, &api.StreamScalesRequest{PageSize: 1, NameFilters: []string{testApp2.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 1)
	c.Assert(res.ScaleRequests[0].Name, Equals, testScale3.Name)
	c.Assert(receivedEOF, Equals, true)

	// test fetching multiple scales by release name
	res, receivedEOF = unaryReceiveScales(s, c, &api.StreamScalesRequest{NameFilters: []string{testRelease6.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 2)
	c.Assert(res.ScaleRequests[0].Name, Equals, testScale7.Name)
	c.Assert(res.ScaleRequests[1].Name, Equals, testScale6.Name)
	c.Assert(receivedEOF, Equals, true)

	// test fetching multiple scales by app name
	res, receivedEOF = unaryReceiveScales(s, c, &api.StreamScalesRequest{NameFilters: []string{testApp1.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 2)
	c.Assert(res.ScaleRequests[0].Name, Equals, testScale4.Name)
	c.Assert(res.ScaleRequests[1].Name, Equals, testScale1.Name)
	c.Assert(receivedEOF, Equals, true)

	// test fetching multiple scales by a mixture of app name and release name
	res, receivedEOF = unaryReceiveScales(s, c, &api.StreamScalesRequest{NameFilters: []string{testApp1.Name, testRelease4.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 1)
	c.Assert(res.ScaleRequests[0].Name, Equals, testScale4.Name)
	c.Assert(receivedEOF, Equals, true)

	// test fetching multiple scales by state
	res, receivedEOF = unaryReceiveScales(s, c, &api.StreamScalesRequest{StateFilters: []api.ScaleRequestState{api.ScaleRequestState_SCALE_COMPLETE}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 2)
	c.Assert(res.ScaleRequests[0].Name, Equals, testScale4.Name)
	c.Assert(res.ScaleRequests[1].Name, Equals, testScale2.Name)
	c.Assert(receivedEOF, Equals, true)

	// test streaming creates for specific release
	stream, cancel := s.streamScalesWithCancel(c, &api.StreamScalesRequest{NameFilters: []string{testRelease6.Name}, StreamCreates: true})
	s.receiveScalesStream(c, stream) // initial page
	testScale8 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease5.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 3}}})
	testScale9 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease6.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 0}}})
	res = s.receiveScalesStream(c, stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 1)
	assertScaleRequestsEqual(c, res.ScaleRequests[0], testScale9)
	cancel()

	// test streaming creates for specific app or release
	stream, cancel = s.streamScalesWithCancel(c, &api.StreamScalesRequest{NameFilters: []string{testApp1.Name, testRelease6.Name}, StreamCreates: true})
	s.receiveScalesStream(c, stream)                                                                                                                                    // initial page
	testScale10 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease5.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 3}}}) // neither
	testScale11 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease6.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 0}}}) // testRelease6 create
	testScale4.State = api.ScaleRequestState_SCALE_CANCELLED
	s.updateTestScaleRequest(c, testScale4)                                                                                                                             // testApp1 update
	testScale12 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease1.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 0}}}) // testApp1 create
	res = s.receiveScalesStream(c, stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 1)
	assertScaleRequestsEqual(c, res.ScaleRequests[0], testScale11)
	res = s.receiveScalesStream(c, stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 1)
	assertScaleRequestsEqual(c, res.ScaleRequests[0], testScale12)
	cancel()

	// test creates are not streamed when flag not set
	stream, cancel = s.streamScalesWithCancel(c, &api.StreamScalesRequest{})
	s.receiveScalesStream(c, stream) // initial page
	testScale13 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease6.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 3}}})
	res = s.receiveScalesStream(c, stream)
	c.Assert(res, IsNil)
	cancel()

	time.Sleep(100 * time.Millisecond) // mitigate race for next test

	// test streaming updates (new scale for the app/release)
	stream, cancel = s.streamScalesWithCancel(c, &api.StreamScalesRequest{NameFilters: []string{testApp1.Name, testRelease6.Name}, StreamUpdates: true})
	s.receiveScalesStream(c, stream)                                                                                                                                    // initial page
	testScale14 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease4.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 3}}}) // testApp1
	testScale15 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease6.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 1}}})
	testScale6.State = api.ScaleRequestState_SCALE_PENDING
	s.updateTestScaleRequest(c, testScale6)
	testScale4.State = api.ScaleRequestState_SCALE_PENDING
	s.updateTestScaleRequest(c, testScale4)
	res = s.receiveScalesStream(c, stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 1) // canceled
	res = s.receiveScalesStream(c, stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 1)
	assertScaleRequestsEqual(c, res.ScaleRequests[0], testScale6)
	res = s.receiveScalesStream(c, stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 1)
	assertScaleRequestsEqual(c, res.ScaleRequests[0], testScale4)
	cancel()

	// test streaming updates (new scale for the app/release) with state filters
	stream, cancel = s.streamScalesWithCancel(c, &api.StreamScalesRequest{StateFilters: []api.ScaleRequestState{api.ScaleRequestState_SCALE_CANCELLED}, StreamUpdates: true})
	s.receiveScalesStream(c, stream) // initial page
	testScale7.State = api.ScaleRequestState_SCALE_PENDING
	s.updateTestScaleRequest(c, testScale7)
	testScale11.State = api.ScaleRequestState_SCALE_CANCELLED
	s.updateTestScaleRequest(c, testScale11)
	res = s.receiveScalesStream(c, stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 1)
	assertScaleRequestsEqual(c, res.ScaleRequests[0], testScale11)
	cancel()

	// test unary pagination
	res, receivedEOF = unaryReceiveScales(s, c, &api.StreamScalesRequest{PageSize: 20})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 15)
	res, receivedEOF = unaryReceiveScales(s, c, &api.StreamScalesRequest{PageSize: 1})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 1)
	assertScaleRequestsEqual(c, res.ScaleRequests[0], testScale15)
	c.Assert(receivedEOF, Equals, true)
	c.Assert(res.NextPageToken, Not(Equals), "")
	c.Assert(res.PageComplete, Equals, true)
	for i, testScale := range []*api.ScaleRequest{testScale14, testScale13, testScale12, testScale11, testScale10, testScale9, testScale8, testScale7, testScale6, testScale5, testScale4, testScale3} {
		comment := Commentf("iteraction %d", i)
		res, receivedEOF = unaryReceiveScales(s, c, &api.StreamScalesRequest{PageSize: 1, PageToken: res.NextPageToken})
		c.Assert(res, Not(IsNil), comment)
		c.Assert(len(res.ScaleRequests), Equals, 1, comment)
		c.Assert(res.ScaleRequests[0].Name, DeepEquals, testScale.Name, comment)
		c.Assert(receivedEOF, Equals, true, comment)
		c.Assert(res.NextPageToken, Not(Equals), "", comment)
		c.Assert(res.PageComplete, Equals, true, comment)
	}
	res, receivedEOF = unaryReceiveScales(s, c, &api.StreamScalesRequest{PageSize: 2, PageToken: res.NextPageToken})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 2)
	assertScaleRequestsEqual(c, res.ScaleRequests[0], testScale2)
	c.Assert(res.ScaleRequests[1].Name, DeepEquals, testScale1.Name)
	c.Assert(receivedEOF, Equals, true)
	c.Assert(res.NextPageToken, Equals, "")
	c.Assert(res.PageComplete, Equals, true)
}

func (s *GRPCSuite) TestStreamScalesUnaryPagination(c *C) {
	testApp1 := s.createTestApp(c, &api.App{DisplayName: "test1"})
	testRelease1 := s.createTestRelease(c, testApp1.Name, &api.Release{Processes: map[string]*api.ProcessType{"devnull": {Args: []string{"tail", "-f", "/dev/null"}, Service: "dev"}}})
	testScale1 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease1.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 1}}})
	testScale2 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease1.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 2}}})
	testScale3 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease1.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 3}}})
	testScale4 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease1.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 4}}})
	testScale5 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease1.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 5}}})
	testScale6 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease1.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 6}}})
	testScale7 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease1.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 7}}})
	testScale8 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease1.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 8}}})

	assertScaleRequestsEqual := func(c *C, a, b *api.ScaleRequest) {
		c.Assert(a.Name, Equals, b.Name)
	}

	// page 1
	res, receivedEOF := unaryReceiveScales(s, c, &api.StreamScalesRequest{PageSize: 3})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 3)
	c.Assert(receivedEOF, Equals, true)
	assertScaleRequestsEqual(c, res.ScaleRequests[0], testScale8)
	assertScaleRequestsEqual(c, res.ScaleRequests[1], testScale7)
	assertScaleRequestsEqual(c, res.ScaleRequests[2], testScale6)
	c.Assert(res.NextPageToken, Not(Equals), "")
	c.Assert(res.PageComplete, Equals, true)

	// page 2
	res, receivedEOF = unaryReceiveScales(s, c, &api.StreamScalesRequest{PageToken: res.NextPageToken})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 3)
	c.Assert(receivedEOF, Equals, true)
	assertScaleRequestsEqual(c, res.ScaleRequests[0], testScale5)
	assertScaleRequestsEqual(c, res.ScaleRequests[1], testScale4)
	assertScaleRequestsEqual(c, res.ScaleRequests[2], testScale3)
	c.Assert(res.NextPageToken, Not(Equals), "")
	c.Assert(res.PageComplete, Equals, true)

	// page 3 (last page)
	res, receivedEOF = unaryReceiveScales(s, c, &api.StreamScalesRequest{PageToken: res.NextPageToken})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 2)
	c.Assert(receivedEOF, Equals, true)
	assertScaleRequestsEqual(c, res.ScaleRequests[0], testScale2)
	assertScaleRequestsEqual(c, res.ScaleRequests[1], testScale1)
	c.Assert(res.NextPageToken, Equals, "")
	c.Assert(res.PageComplete, Equals, true)
}

func (s *GRPCSuite) TestStreamScalesPaginationWithStreamUpdates(c *C) {
	// TODO(jvatic): pagination with StreamUpdates set
}

func (s *GRPCSuite) TestStreamScalesForApp(c *C) {
	testApp1 := s.createTestApp(c, &api.App{DisplayName: "test1"})
	testRelease1 := s.createTestRelease(c, testApp1.Name, &api.Release{Labels: map[string]string{"i": "1"}, Processes: map[string]*api.ProcessType{"devnull": {Args: []string{"tail", "-f", "/dev/null"}, Service: "dev"}}})

	// stream scales for the app
	stream, cancel := s.streamScalesWithCancel(c, &api.StreamScalesRequest{NameFilters: []string{testApp1.Name}, StreamUpdates: true, StreamCreates: true})
	s.receiveScalesStream(c, stream) // initial page

	// scale the release
	testScale1 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease1.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 1}}})
	// expect to get the scale request in stream
	res := s.receiveScalesStream(c, stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 1)
	c.Assert(res.ScaleRequests[0].Name, DeepEquals, testScale1.Name)

	// scale the release again
	testScale2 := s.createTestScaleRequest(c, &api.CreateScaleRequest{Parent: testRelease1.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 2}}})
	// expect to get the scale request in stream
	res = s.receiveScalesStream(c, stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 1)
	c.Assert(res.ScaleRequests[0].Name, DeepEquals, testScale2.Name)
	// expect to get the previous scale request in the stream (canceled)
	res = s.receiveScalesStream(c, stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.ScaleRequests), Equals, 1)
	c.Assert(res.ScaleRequests[0].Name, DeepEquals, testScale1.Name)
	c.Assert(res.ScaleRequests[0].State, DeepEquals, api.ScaleRequestState_SCALE_CANCELLED)

	// close the stream
	cancel()
}

func (s *GRPCSuite) TestStreamScalesForAppPagination(c *C) { // TODO(jvatic): implement test
}

func unaryReceiveDeployments(s *GRPCSuite, c *C, req *api.StreamDeploymentsRequest) (res *api.StreamDeploymentsResponse, receivedEOF bool) {
	ctx, ctxCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer func() {
		if !receivedEOF {
			ctxCancel()
		}
	}()
	stream, err := s.grpc.StreamDeployments(ctx, req)
	c.Assert(err, IsNil)
	for i := 0; i < 2; i++ {
		r, err := stream.Recv()
		if err == io.EOF {
			receivedEOF = true
			return
		}
		if isErrCanceled(err) {
			return
		}
		c.Assert(err, IsNil)
		res = r
	}
	return
}

func (s *GRPCSuite) TestCreateDeployment(c *C) {
	testApp1 := s.createTestApp(c, &api.App{DisplayName: "test1"})
	testArtifact1 := s.createTestArtifact(c, &ct.Artifact{})
	testRelease1 := s.createTestRelease(c, testApp1.Name, &api.Release{
		Labels:    map[string]string{"i": "1"},
		Artifacts: []string{testArtifact1.ID},
	})

	req := &api.CreateDeploymentRequest{
		Parent: testRelease1.Name,
	}
	ctx, _ := context.WithTimeout(context.Background(), 200*time.Millisecond)
	stream, err := s.grpc.CreateDeployment(ctx, req)
	c.Assert(err, IsNil)

	// deployment has no processes, so CreateDeployment should return right away
	_, err = stream.Recv()
	c.Assert(err, Equals, io.EOF)
}

func (s *GRPCSuite) TestStreamDeploymentEvents(c *C) {
	testApp1 := s.createTestApp(c, &api.App{DisplayName: "test1"})
	testArtifact1 := s.createTestArtifact(c, &ct.Artifact{})
	testRelease1 := s.createTestRelease(c, testApp1.Name, &api.Release{
		Labels:    map[string]string{"i": "1"},
		Artifacts: []string{testArtifact1.ID},
		Processes: map[string]*api.ProcessType{"devnull": {Args: []string{"tail", "-f", "/dev/null"}, Service: "dev"}},
	})
	testDeployment1 := s.createTestDeployment(c, testRelease1.Name)

	streamEvents := func(req *api.StreamDeploymentEventsRequest) (api.Controller_StreamDeploymentEventsClient, func(), error) {
		ctx, _ := context.WithTimeout(context.Background(), 200*time.Millisecond)
		stream, err := s.grpc.StreamDeploymentEvents(ctx, req)
		var closeOnce sync.Once
		closeFunc := func() {
			closeOnce.Do(func() {
				stream.CloseSend()
			})
		}
		return stream, closeFunc, err
	}

	for _, nameFilters := range [][]string{
		[]string{testDeployment1.Name},
		[]string{testApp1.Name},
		[]string{testApp1.Name, testDeployment1.Name},
	} {
		(func() {
			stream, closeFunc, err := streamEvents(&api.StreamDeploymentEventsRequest{
				NameFilters:   nameFilters,
				StreamCreates: true,
			})
			defer closeFunc()

			// we don't care about existing events
			res, err := stream.Recv()
			c.Assert(err, IsNil)

			s.createTestDeploymentEvent(c, testDeployment1, &ct.DeploymentEvent{Status: "pending"})
			res, err = stream.Recv()
			c.Assert(err, IsNil)
			c.Assert(len(res.Events), Equals, 1)
			c.Assert(res.Events[0].Parent, Equals, testDeployment1.Name)
			c.Assert(res.Events[0].DeploymentName, Equals, testDeployment1.Name)
			c.Assert(res.Events[0].Type, Equals, string(ct.EventTypeDeployment))

			testJob1 := s.createTestJob(c, testDeployment1, &ct.Job{})
			res, err = stream.Recv()
			c.Assert(err, IsNil)
			c.Assert(len(res.Events), Equals, 1)
			c.Assert(res.Events[0].Parent, Equals, testJob1.Name)
			c.Assert(res.Events[0].DeploymentName, Equals, testDeployment1.Name)
			c.Assert(res.Events[0].Type, Equals, string(ct.EventTypeJob))

			testScale1 := s.createTestScaleRequestForDeployment(c, &api.CreateScaleRequest{Parent: testRelease1.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 1}}}, testDeployment1.Name)
			res, err = stream.Recv()
			c.Assert(err, IsNil)
			c.Assert(len(res.Events), Equals, 1)
			c.Assert(res.Events[0].Parent, Equals, testScale1.Name)
			c.Assert(res.Events[0].DeploymentName, Equals, testDeployment1.Name)
			c.Assert(res.Events[0].Type, Equals, string(ct.EventTypeScaleRequest))
		})()
	}

	(func() {
		stream, closeFunc, err := streamEvents(&api.StreamDeploymentEventsRequest{
			NameFilters:   []string{testDeployment1.Name},
			TypeFilters:   []string{string(ct.EventTypeJob)},
			StreamCreates: true,
		})
		defer closeFunc()

		// we don't care about existing events
		res, err := stream.Recv()
		c.Assert(err, IsNil)

		s.createTestDeploymentEvent(c, testDeployment1, &ct.DeploymentEvent{Status: "pending"})

		s.createTestScaleRequestForDeployment(c, &api.CreateScaleRequest{Parent: testRelease1.Name, Config: &api.ScaleConfig{Processes: map[string]int32{"devnull": 1}}}, testDeployment1.Name)

		testJob1 := s.createTestJob(c, testDeployment1, &ct.Job{})
		res, err = stream.Recv()
		c.Assert(err, IsNil)
		c.Assert(len(res.Events), Equals, 1)
		c.Assert(res.Events[0].Parent, Equals, testJob1.Name)
		c.Assert(res.Events[0].DeploymentName, Equals, testDeployment1.Name)
		c.Assert(res.Events[0].Type, Equals, string(ct.EventTypeJob))
	})()
}

func (s *GRPCSuite) TestStreamDeployments(c *C) {
	testApp1 := s.createTestApp(c, &api.App{DisplayName: "test1"})
	testApp2 := s.createTestApp(c, &api.App{DisplayName: "test2"})
	testApp3 := s.createTestApp(c, &api.App{DisplayName: "test3"})
	testArtifact1 := s.createTestArtifact(c, &ct.Artifact{})
	testArtifact2 := s.createTestArtifact(c, &ct.Artifact{})
	testRelease1 := s.createTestRelease(c, testApp1.Name, &api.Release{Labels: map[string]string{"i": "1"}, Artifacts: []string{testArtifact1.ID}})
	testRelease2 := s.createTestRelease(c, testApp1.Name, &api.Release{Labels: map[string]string{"i": "2"}, Artifacts: []string{testArtifact1.ID}})
	testRelease3 := s.createTestRelease(c, testApp3.Name, &api.Release{Labels: map[string]string{"i": "3"}, Artifacts: []string{testArtifact2.ID}})
	testRelease4 := s.createTestRelease(c, testApp2.Name, &api.Release{Labels: map[string]string{"i": "4"}})
	testDeployment1 := s.createTestDeployment(c, testRelease1.Name)
	testDeployment2 := s.createTestDeployment(c, testRelease2.Name)
	testDeployment3 := s.createTestDeployment(c, testRelease3.Name)
	testDeployment4 := s.createTestDeployment(c, testRelease4.Name)

	c.Assert(testDeployment1.Type, Equals, api.ReleaseType_CODE)
	c.Assert(testDeployment2.Type, Equals, api.ReleaseType_CONFIG)
	c.Assert(testDeployment3.Type, Equals, api.ReleaseType_CODE)
	c.Assert(testDeployment4.Type, Equals, api.ReleaseType_CONFIG)

	streamDeploymentsWithCancel := func(req *api.StreamDeploymentsRequest) (api.Controller_StreamDeploymentsClient, context.CancelFunc) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		stream, err := s.grpc.StreamDeployments(ctx, req)
		c.Assert(err, IsNil)
		return stream, cancel
	}

	receiveDeploymentsStream := func(stream api.Controller_StreamDeploymentsClient) *api.StreamDeploymentsResponse {
		res, err := stream.Recv()
		if err == io.EOF || isErrCanceled(err) || isErrDeadlineExceeded(err) {
			return nil
		}
		c.Assert(err, IsNil)
		return res
	}

	assertDeploymentsEqual := func(c *C, a, b *api.ExpandedDeployment) {
		comment := Commentf("Obtained %#v", a)
		if a != nil {
			comment = Commentf("Obtained %T{NewRelease: %#v ...}", a, a.NewRelease)
		}
		c.Assert(a.Name, DeepEquals, b.Name, comment)
		c.Assert(a.OldRelease, DeepEquals, b.OldRelease, comment)
		c.Assert(a.NewRelease, DeepEquals, b.NewRelease, comment)
		c.Assert(a.Type, DeepEquals, b.Type, comment)
		c.Assert(a.Strategy, DeepEquals, b.Strategy, comment)
		c.Assert(a.Status, DeepEquals, b.Status, comment)
		c.Assert(a.Processes, DeepEquals, b.Processes, comment)
		c.Assert(a.Tags, DeepEquals, b.Tags, comment)
		c.Assert(a.DeployTimeout, DeepEquals, b.DeployTimeout, comment)
		c.Assert(a.CreateTime, DeepEquals, b.CreateTime, comment)
		c.Assert(a.ExpireTime, DeepEquals, b.ExpireTime, comment)
		c.Assert(a.EndTime, DeepEquals, b.EndTime, comment)
	}

	// test fetching the latest deployment
	res, receivedEOF := unaryReceiveDeployments(s, c, &api.StreamDeploymentsRequest{PageSize: 1})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 1)
	assertDeploymentsEqual(c, res.Deployments[0], testDeployment4)
	c.Assert(receivedEOF, Equals, true)

	// test fetching a single deployment by name
	res, receivedEOF = unaryReceiveDeployments(s, c, &api.StreamDeploymentsRequest{NameFilters: []string{testDeployment2.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 1)
	assertDeploymentsEqual(c, res.Deployments[0], testDeployment2)
	c.Assert(receivedEOF, Equals, true)

	// test fetching a single deployment by app name
	res, receivedEOF = unaryReceiveDeployments(s, c, &api.StreamDeploymentsRequest{NameFilters: []string{testApp3.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 1)
	assertDeploymentsEqual(c, res.Deployments[0], testDeployment3)
	c.Assert(receivedEOF, Equals, true)

	// test fetching multiple deployments by name
	res, receivedEOF = unaryReceiveDeployments(s, c, &api.StreamDeploymentsRequest{NameFilters: []string{testDeployment3.Name, testDeployment2.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 2)
	assertDeploymentsEqual(c, res.Deployments[0], testDeployment3)
	assertDeploymentsEqual(c, res.Deployments[1], testDeployment2)
	c.Assert(receivedEOF, Equals, true)

	// test fetching multiple deployments by app name
	res, receivedEOF = unaryReceiveDeployments(s, c, &api.StreamDeploymentsRequest{NameFilters: []string{testApp1.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 2)
	assertDeploymentsEqual(c, res.Deployments[0], testDeployment2)
	assertDeploymentsEqual(c, res.Deployments[1], testDeployment1)
	c.Assert(receivedEOF, Equals, true)

	// test fetching multiple deployments by a mixture of app name and deployment name
	res, receivedEOF = unaryReceiveDeployments(s, c, &api.StreamDeploymentsRequest{NameFilters: []string{testDeployment2.Name, testApp1.Name}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 1)
	assertDeploymentsEqual(c, res.Deployments[0], testDeployment2)
	c.Assert(receivedEOF, Equals, true)

	// test fetching multiple deployments by type [ANY]
	res, receivedEOF = unaryReceiveDeployments(s, c, &api.StreamDeploymentsRequest{PageSize: 1, TypeFilters: []api.ReleaseType{api.ReleaseType_ANY}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 1)
	assertDeploymentsEqual(c, res.Deployments[0], testDeployment4)
	c.Assert(receivedEOF, Equals, true)

	// test fetching multiple deployments by type [CODE]
	res, receivedEOF = unaryReceiveDeployments(s, c, &api.StreamDeploymentsRequest{TypeFilters: []api.ReleaseType{api.ReleaseType_CODE}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 2)
	assertDeploymentsEqual(c, res.Deployments[0], testDeployment3)
	assertDeploymentsEqual(c, res.Deployments[1], testDeployment1)
	c.Assert(receivedEOF, Equals, true)

	// test fetching multiple deployments by type [CONFIG]
	res, receivedEOF = unaryReceiveDeployments(s, c, &api.StreamDeploymentsRequest{TypeFilters: []api.ReleaseType{api.ReleaseType_CONFIG}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 2)
	assertDeploymentsEqual(c, res.Deployments[0], testDeployment4)
	assertDeploymentsEqual(c, res.Deployments[1], testDeployment2)
	c.Assert(receivedEOF, Equals, true)

	// test fetching filtering deployments by status
	res, receivedEOF = unaryReceiveDeployments(s, c, &api.StreamDeploymentsRequest{StatusFilters: []api.DeploymentStatus{
		api.DeploymentStatus_FAILED,
	}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 0)
	testDeployment2.Status = api.DeploymentStatus_FAILED
	s.createTestDeploymentEvent(c, testDeployment2, &ct.DeploymentEvent{Status: "failed"})
	res, receivedEOF = unaryReceiveDeployments(s, c, &api.StreamDeploymentsRequest{StatusFilters: []api.DeploymentStatus{
		api.DeploymentStatus_FAILED,
	}})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 1)
	assertDeploymentsEqual(c, res.Deployments[0], testDeployment2)

	// test streaming creates for specific app
	stream, cancel := streamDeploymentsWithCancel(&api.StreamDeploymentsRequest{NameFilters: []string{testApp2.Name}, StreamCreates: true})
	receiveDeploymentsStream(stream) // initial page
	testRelease5 := s.createTestRelease(c, testApp2.Name, &api.Release{Labels: map[string]string{"i": "5"}})
	testDeployment5 := s.createTestDeployment(c, testRelease5.Name)
	res = receiveDeploymentsStream(stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 1)
	assertDeploymentsEqual(c, res.Deployments[0], testDeployment5)
	cancel()

	// test creates are not streamed when flag not set
	stream, cancel = streamDeploymentsWithCancel(&api.StreamDeploymentsRequest{NameFilters: []string{testApp2.Name}})
	receiveDeploymentsStream(stream) // initial page
	testRelease6 := s.createTestRelease(c, testApp2.Name, &api.Release{Labels: map[string]string{"i": "6"}})
	testDeployment6 := s.createTestDeployment(c, testRelease6.Name)
	res = receiveDeploymentsStream(stream)
	c.Assert(res, IsNil)
	cancel()

	// test streaming creates that don't match the NameFilters
	stream, cancel = streamDeploymentsWithCancel(&api.StreamDeploymentsRequest{NameFilters: []string{testApp2.Name}, StreamCreates: true})
	receiveDeploymentsStream(stream) // initial page
	testRelease7 := s.createTestRelease(c, testApp1.Name, &api.Release{Labels: map[string]string{"i": "7"}})
	testDeployment7 := s.createTestDeployment(c, testRelease7.Name)
	res = receiveDeploymentsStream(stream)
	c.Assert(res, IsNil)
	cancel()

	// test streaming creates without filters
	stream, cancel = streamDeploymentsWithCancel(&api.StreamDeploymentsRequest{StreamCreates: true})
	receiveDeploymentsStream(stream) // initial page
	testRelease8 := s.createTestRelease(c, testApp2.Name, &api.Release{Labels: map[string]string{"i": "8"}})
	testDeployment8 := s.createTestDeployment(c, testRelease8.Name)
	res = receiveDeploymentsStream(stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 1)
	assertDeploymentsEqual(c, res.Deployments[0], testDeployment8)
	cancel()

	// test streaming creates that don't match the TypeFilters
	stream, cancel = streamDeploymentsWithCancel(&api.StreamDeploymentsRequest{TypeFilters: []api.ReleaseType{api.ReleaseType_CODE}, StreamCreates: true})
	receiveDeploymentsStream(stream) // initial page
	testRelease9 := s.createTestRelease(c, testApp3.Name, &api.Release{Labels: map[string]string{"i": "9"}, Artifacts: testRelease3.Artifacts})
	testDeployment9 := s.createTestDeployment(c, testRelease9.Name) // doesn't match TypeFilters
	testRelease10 := s.createTestRelease(c, testApp3.Name, &api.Release{Labels: map[string]string{"i": "10"}})
	testDeployment10 := s.createTestDeployment(c, testRelease10.Name) // matches TypeFilters
	res = receiveDeploymentsStream(stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 1)
	assertDeploymentsEqual(c, res.Deployments[0], testDeployment10)
	cancel()

	// test streaming updates
	stream, cancel = streamDeploymentsWithCancel(&api.StreamDeploymentsRequest{StreamUpdates: true})
	receiveDeploymentsStream(stream) // initial page
	testRelease11 := s.createTestRelease(c, testApp3.Name, &api.Release{Labels: map[string]string{"i": "11"}})
	testDeployment11 := s.createTestDeployment(c, testRelease11.Name) // make sure creates aren't streamed
	testDeployment10.Status = api.DeploymentStatus_PENDING
	s.createTestDeploymentEvent(c, testDeployment10, &ct.DeploymentEvent{Status: "pending"})
	res = receiveDeploymentsStream(stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 1)
	assertDeploymentsEqual(c, res.Deployments[0], testDeployment10)
	cancel()

	// test streaming updates respects NameFilters [app name]
	stream, cancel = streamDeploymentsWithCancel(&api.StreamDeploymentsRequest{NameFilters: []string{testApp2.Name}, StreamUpdates: true})
	receiveDeploymentsStream(stream) // initial page
	testDeployment10.Status = api.DeploymentStatus_COMPLETE
	s.createTestDeploymentEvent(c, testDeployment10, &ct.DeploymentEvent{Status: "complete"})
	testDeployment6.Status = api.DeploymentStatus_PENDING
	s.createTestDeploymentEvent(c, testDeployment6, &ct.DeploymentEvent{Status: "pending"})
	res = receiveDeploymentsStream(stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 1)
	assertDeploymentsEqual(c, res.Deployments[0], testDeployment6)
	cancel()

	// test streaming updates respects NameFilters [deployment name]
	stream, cancel = streamDeploymentsWithCancel(&api.StreamDeploymentsRequest{NameFilters: []string{testDeployment5.Name}, StreamUpdates: true})
	receiveDeploymentsStream(stream) // initial page
	testDeployment6.Status = api.DeploymentStatus_COMPLETE
	s.createTestDeploymentEvent(c, testDeployment6, &ct.DeploymentEvent{Status: "complete"})
	testDeployment5.Status = api.DeploymentStatus_PENDING
	s.createTestDeploymentEvent(c, testDeployment5, &ct.DeploymentEvent{Status: "pending"})
	res = receiveDeploymentsStream(stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 1)
	assertDeploymentsEqual(c, res.Deployments[0], testDeployment5)
	cancel()

	// test streaming updates respects StatusFilters
	stream, cancel = streamDeploymentsWithCancel(&api.StreamDeploymentsRequest{StatusFilters: []api.DeploymentStatus{api.DeploymentStatus_FAILED}, StreamUpdates: true})
	receiveDeploymentsStream(stream) // initial page
	testDeployment5.Status = api.DeploymentStatus_PENDING
	s.createTestDeploymentEvent(c, testDeployment5, &ct.DeploymentEvent{Status: "pending"})
	testDeployment6.Status = api.DeploymentStatus_FAILED
	s.createTestDeploymentEvent(c, testDeployment6, &ct.DeploymentEvent{Status: "failed"})
	res = receiveDeploymentsStream(stream)
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 1)
	assertDeploymentsEqual(c, res.Deployments[0], testDeployment6)
	cancel()

	// test unary pagination
	res, receivedEOF = unaryReceiveDeployments(s, c, &api.StreamDeploymentsRequest{PageSize: 1})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 1)
	c.Assert(res.Deployments[0], DeepEquals, testDeployment11)
	c.Assert(receivedEOF, Equals, true)
	c.Assert(res.NextPageToken, Not(Equals), "")
	c.Assert(res.PageComplete, Equals, true)
	for i, testDeployment := range []*api.ExpandedDeployment{testDeployment10, testDeployment9, testDeployment8, testDeployment7, testDeployment6, testDeployment5, testDeployment4, testDeployment3} {
		comment := Commentf("iteraction %d", i)
		res, receivedEOF = unaryReceiveDeployments(s, c, &api.StreamDeploymentsRequest{PageSize: 1, PageToken: res.NextPageToken})
		c.Assert(res, Not(IsNil), comment)
		c.Assert(len(res.Deployments), Equals, 1, comment)
		c.Assert(res.Deployments[0], DeepEquals, testDeployment, comment)
		c.Assert(receivedEOF, Equals, true, comment)
		c.Assert(res.NextPageToken, Not(Equals), "", comment)
		c.Assert(res.PageComplete, Equals, true, comment)
	}
	res, receivedEOF = unaryReceiveDeployments(s, c, &api.StreamDeploymentsRequest{PageSize: 2, PageToken: res.NextPageToken})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 2)
	c.Assert(res.Deployments[0], DeepEquals, testDeployment2)
	c.Assert(res.Deployments[1], DeepEquals, testDeployment1)
	c.Assert(receivedEOF, Equals, true)
	c.Assert(res.NextPageToken, Equals, "")
	c.Assert(res.PageComplete, Equals, true)
}

func (s *GRPCSuite) TestStreamDeploymentsUnaryPagination(c *C) {
	testApp1 := s.createTestApp(c, &api.App{DisplayName: "test1"})
	testArtifact1 := s.createTestArtifact(c, &ct.Artifact{})
	testRelease1 := s.createTestRelease(c, testApp1.Name, &api.Release{Labels: map[string]string{"i": "1"}, Artifacts: []string{testArtifact1.ID}})
	testRelease2 := s.createTestRelease(c, testApp1.Name, &api.Release{Labels: map[string]string{"i": "2"}, Artifacts: []string{testArtifact1.ID}})
	testRelease3 := s.createTestRelease(c, testApp1.Name, &api.Release{Labels: map[string]string{"i": "3"}, Artifacts: []string{testArtifact1.ID}})
	testRelease4 := s.createTestRelease(c, testApp1.Name, &api.Release{Labels: map[string]string{"i": "4"}, Artifacts: []string{testArtifact1.ID}})
	testRelease5 := s.createTestRelease(c, testApp1.Name, &api.Release{Labels: map[string]string{"i": "5"}, Artifacts: []string{testArtifact1.ID}})
	testRelease6 := s.createTestRelease(c, testApp1.Name, &api.Release{Labels: map[string]string{"i": "6"}, Artifacts: []string{testArtifact1.ID}})
	testRelease7 := s.createTestRelease(c, testApp1.Name, &api.Release{Labels: map[string]string{"i": "7"}, Artifacts: []string{testArtifact1.ID}})
	testRelease8 := s.createTestRelease(c, testApp1.Name, &api.Release{Labels: map[string]string{"i": "8"}, Artifacts: []string{testArtifact1.ID}})
	testDeployment1 := s.createTestDeployment(c, testRelease1.Name)
	testDeployment2 := s.createTestDeployment(c, testRelease2.Name)
	testDeployment3 := s.createTestDeployment(c, testRelease3.Name)
	testDeployment4 := s.createTestDeployment(c, testRelease4.Name)
	testDeployment5 := s.createTestDeployment(c, testRelease5.Name)
	testDeployment6 := s.createTestDeployment(c, testRelease6.Name)
	testDeployment7 := s.createTestDeployment(c, testRelease7.Name)
	testDeployment8 := s.createTestDeployment(c, testRelease8.Name)

	// page 1
	res, receivedEOF := unaryReceiveDeployments(s, c, &api.StreamDeploymentsRequest{PageSize: 3})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 3)
	c.Assert(receivedEOF, Equals, true)
	c.Assert(res.Deployments[0], DeepEquals, testDeployment8)
	c.Assert(res.Deployments[1], DeepEquals, testDeployment7)
	c.Assert(res.Deployments[2], DeepEquals, testDeployment6)
	c.Assert(res.NextPageToken, Not(Equals), "")
	c.Assert(res.PageComplete, Equals, true)

	// page 2
	res, receivedEOF = unaryReceiveDeployments(s, c, &api.StreamDeploymentsRequest{PageToken: res.NextPageToken})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 3)
	c.Assert(receivedEOF, Equals, true)
	c.Assert(res.Deployments[0], DeepEquals, testDeployment5)
	c.Assert(res.Deployments[1], DeepEquals, testDeployment4)
	c.Assert(res.Deployments[2], DeepEquals, testDeployment3)
	c.Assert(res.NextPageToken, Not(Equals), "")
	c.Assert(res.PageComplete, Equals, true)

	// page 3 (last)
	res, receivedEOF = unaryReceiveDeployments(s, c, &api.StreamDeploymentsRequest{PageToken: res.NextPageToken})
	c.Assert(res, Not(IsNil))
	c.Assert(len(res.Deployments), Equals, 2)
	c.Assert(receivedEOF, Equals, true)
	c.Assert(res.Deployments[0], DeepEquals, testDeployment2)
	c.Assert(res.Deployments[1], DeepEquals, testDeployment1)
	c.Assert(res.NextPageToken, Equals, "")
	c.Assert(res.PageComplete, Equals, true)
}
