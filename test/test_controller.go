package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cupcake/jsonschema"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/exec"
	"github.com/flynn/flynn/pkg/random"
	c "github.com/flynn/go-check"
)

type ControllerSuite struct {
	schemaCache map[string]*jsonschema.Schema
	Helper
}

var _ = c.ConcurrentSuite(&ControllerSuite{})

func (s *ControllerSuite) SetUpSuite(t *c.C) {
	var schemaPaths []string
	walkFn := func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() && filepath.Ext(path) == ".json" {
			schemaPaths = append(schemaPaths, path)
		}
		return nil
	}
	schemaRoot, err := filepath.Abs(filepath.Join("..", "schema"))
	t.Assert(err, c.IsNil)
	t.Assert(filepath.Walk(schemaRoot, walkFn), c.IsNil)

	s.schemaCache = make(map[string]*jsonschema.Schema, len(schemaPaths))
	for _, path := range schemaPaths {
		file, err := os.Open(path)
		t.Assert(err, c.IsNil)
		schema := &jsonschema.Schema{Cache: s.schemaCache}
		err = schema.ParseWithoutRefs(file)
		t.Assert(err, c.IsNil)
		cacheKey := "https://flynn.io/schema" + strings.TrimSuffix(strings.TrimPrefix(path, schemaRoot), ".json")
		s.schemaCache[cacheKey] = schema
		file.Close()
	}
	for _, schema := range s.schemaCache {
		schema.ResolveRefs(false)
	}
}

type controllerExampleRequest struct {
	Method  string            `json:"method,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    interface{}       `json:"body,omitempty"`
}

type controllerExampleResponse struct {
	Headers map[string]string `json:"headers,omitempty"`
	Body    interface{}       `json:"body,omitempty"`
}

type controllerExample struct {
	Request  controllerExampleRequest  `json:"request,omitempty"`
	Response controllerExampleResponse `json:"response,omitempty"`
}

var jsonContentTypePattern = regexp.MustCompile(`\bjson`)

func unmarshalControllerExample(data []byte) (map[string]interface{}, error) {
	var example controllerExample
	if err := json.Unmarshal(data, &example); err != nil {
		return nil, err
	}

	if jsonContentTypePattern.MatchString(example.Request.Headers["Content-Type"]) {
		if body, ok := example.Request.Body.(string); ok {
			var reqBody interface{}
			if err := json.Unmarshal([]byte(body), &reqBody); err != nil {
				return nil, err
			}
			example.Request.Body = reqBody
		}
	}
	if jsonContentTypePattern.MatchString(example.Response.Headers["Content-Type"]) {
		if body, ok := example.Response.Body.(string); ok {
			var resBody interface{}
			if err := json.Unmarshal([]byte(body), &resBody); err != nil {
				return nil, err
			}
			example.Response.Body = resBody
		}
	}

	rawData, err := json.Marshal(example)
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(rawData))
	decoder.UseNumber()
	return out, decoder.Decode(&out)
}

func (s *ControllerSuite) generateControllerExamples(t *c.C) map[string]interface{} {
	cmd := exec.CommandUsingCluster(
		s.clusterClient(t),
		s.createArtifact(t, "controller-examples"),
		"/bin/flynn-controller-examples",
	)
	cmd.Env = map[string]string{
		"CONTROLLER_KEY":      s.clusterConf(t).Key,
		"SKIP_MIGRATE_DOMAIN": "true",
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	t.Logf("stdout: %q", stdout.String())
	t.Logf("stderr: %q", stderr.String())
	t.Assert(err, c.IsNil)

	var controllerExamples map[string]json.RawMessage
	t.Assert(json.Unmarshal(stdout.Bytes(), &controllerExamples), c.IsNil)

	examples := make(map[string]interface{}, len(controllerExamples))
	for key, data := range controllerExamples {
		example, err := unmarshalControllerExample(data)
		t.Assert(err, c.IsNil)
		examples[key] = example
	}
	return examples
}

func (s *ControllerSuite) TestExampleOutput(t *c.C) {
	examples := s.generateControllerExamples(t)
	exampleKeys := make([]string, 0, len(examples))
	skipExamples := []string{"migrate_cluster_domain"}
examplesLoop:
	for key := range examples {
		for _, skipKey := range skipExamples {
			if key == skipKey {
				continue examplesLoop
			}
		}
		exampleKeys = append(exampleKeys, key)
	}
	sort.Strings(exampleKeys)
	for _, key := range exampleKeys {
		cacheKey := "https://flynn.io/schema/examples/controller/" + key
		schema := s.schemaCache[cacheKey]
		if schema == nil {
			continue
		}
		data := examples[key]
		errs := schema.Validate(nil, data)
		var jsonData []byte
		if len(errs) > 0 {
			jsonData, _ = json.MarshalIndent(data, "", "\t")
		}
		t.Assert(errs, c.HasLen, 0, c.Commentf("%s validation errors: %v\ndata: %v\n", cacheKey, errs, string(jsonData)))
	}
}

func (s *ControllerSuite) TestKeyRotation(t *c.C) {
	x := s.bootCluster(t, 1)
	defer x.Destroy()

	oldKey := x.Key
	newKey := random.Hex(16)

	// allow auth to API with old and new keys
	set := x.flynn("/", "-a", "controller", "env", "set", "-t", "web", fmt.Sprintf("AUTH_KEY=%s,%s", newKey, oldKey))
	t.Assert(set, Succeeds)

	// reconfigure components to use new key
	for _, app := range []string{"gitreceive", "docker-receive", "taffy", "dashboard"} {
		set := x.flynn("/", "-a", app, "env", "set", "CONTROLLER_KEY="+newKey)
		t.Assert(set, Succeeds)
	}

	// write a new flynnrc
	x.setKey(newKey)

	// use new key for deployer+controller
	set = x.flynn("/", "-a", "controller", "env", "set", "AUTH_KEY="+newKey)
	t.Assert(set, Succeeds)

	// remove old key from API
	set = x.flynn("/", "-a", "controller", "env", "unset", "-t", "web", "AUTH_KEY")
	t.Assert(set, Succeeds)
}

func (s *ControllerSuite) TestResourceLimitsOneOffJob(t *c.C) {
	app, release := s.createApp(t)

	rwc, err := s.controllerClient(t).RunJobAttached(app.ID, &ct.NewJob{
		ReleaseID: release.ID,
		Args:      []string{"sh", "-c", resourceCmd},
		Resources: testResources(),
	})
	t.Assert(err, c.IsNil)
	attachClient := cluster.NewAttachClient(rwc)
	var out bytes.Buffer
	exit, err := attachClient.Receive(&out, &out)
	t.Assert(exit, c.Equals, 0)
	t.Assert(err, c.IsNil)

	assertResourceLimits(t, out.String())
}

func (s *ControllerSuite) TestResourceLimitsReleaseJob(t *c.C) {
	client := s.controllerClient(t)
	app, release := s.createApp(t)

	watcher, err := client.WatchJobEvents(app.ID, release.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()

	t.Assert(client.PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"resources": 1},
	}), c.IsNil)
	var jobID string
	err = watcher.WaitFor(ct.JobEvents{"resources": {ct.JobStateUp: 1, ct.JobStateDown: 1}}, scaleTimeout, func(e *ct.Job) error {
		jobID = e.ID
		return nil
	})
	t.Assert(err, c.IsNil)
	log := flynn(t, "/", "-a", app.Name, "log", "--job", jobID, "--raw-output")

	assertResourceLimits(t, log.Output)
}

func (s *ControllerSuite) TestAppDelete(t *c.C) {
	client := s.controllerClient(t)

	type test struct {
		desc    string
		name    string
		create  bool
		useName bool
		delErr  error
	}

	for _, s := range []test{
		{
			desc:    "delete existing app by name",
			name:    "app-delete-" + random.String(8),
			create:  true,
			useName: true,
			delErr:  nil,
		},
		{
			desc:    "delete existing app by id",
			name:    "app-delete-" + random.String(8),
			create:  true,
			useName: false,
			delErr:  nil,
		},
		{
			desc:    "delete existing UUID app by name",
			name:    random.UUID(),
			create:  true,
			useName: true,
			delErr:  nil,
		},
		{
			desc:    "delete existing UUID app by id",
			name:    random.UUID(),
			create:  true,
			useName: false,
			delErr:  nil,
		},
		{
			desc:    "delete non-existent app",
			name:    "i-dont-exist",
			create:  false,
			useName: true,
			delErr:  controller.ErrNotFound,
		},
		{
			desc:    "delete non-existent UUID app",
			name:    random.UUID(),
			create:  false,
			useName: true,
			delErr:  controller.ErrNotFound,
		},
	} {
		debugf(t, "TestAppDelete: %s", s.desc)

		app := &ct.App{Name: s.name}
		if s.create {
			t.Assert(client.CreateApp(app), c.IsNil)
		}

		appID := app.ID
		if s.useName {
			appID = app.Name
		}

		_, err := client.DeleteApp(appID)
		t.Assert(err, c.Equals, s.delErr)

		if s.delErr == nil {
			_, err = client.GetApp(appID)
			t.Assert(err, c.Equals, controller.ErrNotFound)
		}
	}
}

func (s *ControllerSuite) TestAppDeleteCleanup(t *c.C) {
	app := "app-delete-cleanup-" + random.String(8)
	client := s.controllerClient(t)

	// create and push app
	r := s.newGitRepo(t, "http")
	t.Assert(r.flynn("create", app), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)

	// wait for it to start
	service := app + "-web"
	_, err := s.discoverdClient(t).Instances(service, 10*time.Second)
	t.Assert(err, c.IsNil)

	t.Assert(r.flynn("scale", "another-web=1"), Succeeds)
	_, err = s.discoverdClient(t).Instances(app+"-another-web", 10*time.Second)
	t.Assert(err, c.IsNil)

	// create some routes
	routes := []string{"foo.example.com", "bar.example.com", "another.example.com"}
	for _, route := range routes {
		if route == "another.example.com" {
			t.Assert(r.flynn("route", "add", "http", "-s", app+"-another-web", route), Succeeds)
		} else {
			t.Assert(r.flynn("route", "add", "http", route), Succeeds)
		}
	}
	routeList, err := client.RouteList(app)
	t.Assert(err, c.IsNil)
	numRoutes := len(routes) + 1 // includes default app route
	t.Assert(routeList, c.HasLen, numRoutes)

	assertRouteStatus := func(route string, status int) {
		req, err := http.NewRequest("GET", "http://"+routerIP, nil)
		t.Assert(err, c.IsNil)
		req.Host = route
		res, err := http.DefaultClient.Do(req)
		t.Assert(err, c.IsNil)
		t.Assert(res.StatusCode, c.Equals, status)
	}
	for _, route := range routes {
		assertRouteStatus(route, 200)
	}

	// provision resources
	t.Assert(r.flynn("resource", "add", "postgres"), Succeeds)
	resources, err := client.AppResourceList(app)
	t.Assert(err, c.IsNil)
	numResources := 1
	t.Assert(resources, c.HasLen, numResources)

	// create another release
	t.Assert(r.git("commit", "--allow-empty", "--message", "deploy"), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)
	releases, err := client.AppReleaseList(app)
	t.Assert(err, c.IsNil)

	// delete app
	cmd := r.flynn("delete", "--yes")
	t.Assert(cmd, Succeeds)

	// check route cleanup
	t.Assert(cmd, OutputContains, fmt.Sprintf("removed %d routes", numRoutes))
	for _, route := range routes {
		assertRouteStatus(route, 404)
	}

	// check release cleanup
	t.Assert(cmd, OutputContains, fmt.Sprintf("deleted %d releases", len(releases)))
	for _, release := range releases {
		_, err := client.GetRelease(release.ID)
		t.Assert(err, c.Equals, controller.ErrNotFound)
	}

	// check resource cleanup
	t.Assert(cmd, OutputContains, fmt.Sprintf("deprovisioned %d resources", numResources))

	// check creating and pushing same app name succeeds
	t.Assert(os.RemoveAll(r.dir), c.IsNil)
	r = s.newGitRepo(t, "http")
	t.Assert(r.flynn("create", app), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)
}

// https://github.com/flynn/flynn/issues/2257
func (s *ControllerSuite) TestResourceProvisionRecreatedApp(t *c.C) {
	app := "app-recreate-" + random.String(8)
	client := s.controllerClient(t)

	// create, delete, and recreate app
	r := s.newGitRepo(t, "http")
	t.Assert(r.flynn("create", app), Succeeds)
	t.Assert(r.flynn("delete", "--yes"), Succeeds)
	t.Assert(r.flynn("create", app), Succeeds)

	// provision resource
	t.Assert(r.flynn("resource", "add", "postgres"), Succeeds)
	resources, err := client.AppResourceList(app)
	t.Assert(err, c.IsNil)
	t.Assert(resources, c.HasLen, 1)
}

func (s *ControllerSuite) TestRouteEvents(t *c.C) {
	app := "app-route-events-" + random.String(8)
	client := s.controllerClient(t)

	// create and push app
	r := s.newGitRepo(t, "http")
	t.Assert(r.flynn("create", app), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)

	// wait for it to start
	service := app + "-web"
	_, err := s.discoverdClient(t).Instances(service, 10*time.Second)
	t.Assert(err, c.IsNil)

	// stream events
	events := make(chan *ct.Event)
	stream, err := client.StreamEvents(ct.StreamEventsOptions{
		AppID:       app,
		ObjectTypes: []ct.EventType{ct.EventTypeRoute, ct.EventTypeRouteDeletion},
		Past:        true,
	}, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	assertEventType := func(typ ct.EventType) {
		select {
		case event, ok := <-events:
			t.Assert(ok, c.Equals, true)
			t.Assert(event.ObjectType, c.Equals, typ, c.Commentf("event: %#v", event))
		case <-time.After(30 * time.Second):
			t.Assert(true, c.Equals, false, c.Commentf("timed out waiting for %s event", string(typ)))
		}
	}

	// default app route
	assertEventType(ct.EventTypeRoute)

	// create some routes
	routes := []string{"baz.example.com"}
	for _, route := range routes {
		t.Assert(r.flynn("route", "add", "http", route), Succeeds)
		assertEventType(ct.EventTypeRoute)
	}
	routeList, err := client.RouteList(app)
	t.Assert(err, c.IsNil)
	numRoutes := len(routes) + 1 // includes default app route
	t.Assert(routeList, c.HasLen, numRoutes)

	// delete app
	cmd := r.flynn("delete", "--yes")
	t.Assert(cmd, Succeeds)

	// check route deletion event
	assertEventType(ct.EventTypeRouteDeletion)
}

// TestAppEvents checks that streaming events for an app only receives events
// for that particular app.
func (s *ControllerSuite) TestAppEvents(t *c.C) {
	client := s.controllerClient(t)
	app1, release1 := s.createApp(t)
	app2, release2 := s.createApp(t)

	// stream events for app1
	events := make(chan *ct.Job)
	stream, err := client.StreamJobEvents(app1.ID, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	runJob := func(appID, releaseID string) {
		rwc, err := client.RunJobAttached(appID, &ct.NewJob{
			ReleaseID:  releaseID,
			Args:       []string{"/bin/true"},
			DisableLog: true,
		})
		t.Assert(err, c.IsNil)
		rwc.Close()
	}

	// generate events for app2 and wait for them
	watcher, err := client.WatchJobEvents(app2.ID, release2.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()
	runJob(app2.ID, release2.ID)
	t.Assert(watcher.WaitFor(
		ct.JobEvents{"": {ct.JobStateUp: 1, ct.JobStateDown: 1}},
		10*time.Second,
		func(e *ct.Job) error {
			debugf(t, "got %s job event for app2", e.State)
			return nil
		},
	), c.IsNil)

	// generate events for app1
	runJob(app1.ID, release1.ID)

	// check the stream only gets events for app1
	for {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatal("unexpected close of job event stream")
			}
			t.Assert(e.AppID, c.Equals, app1.ID)
			debugf(t, "got %s job event for app1", e.State)
			if e.State == ct.JobStateDown {
				return
			}
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for job events for app1")
		}
	}
}

func (s *ControllerSuite) TestBackup(t *c.C) {
	x := s.bootCluster(t, 1)
	defer x.Destroy()

	out, err := x.controller.Backup()
	t.Assert(err, c.IsNil)
	defer out.Close()
	data := make(map[string][]byte)
	tr := tar.NewReader(out)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		t.Assert(err, c.IsNil)
		b := make([]byte, h.Size)
		_, err = tr.Read(b)
		t.Assert(err, c.IsNil)
		_, filename := filepath.Split(h.Name)
		data[filename] = b
	}
	sql, ok := data["postgres.sql.gz"]
	t.Assert(ok, c.Equals, true)
	t.Assert(len(sql) > 0, c.Equals, true)
	flynn, ok := data["flynn.json"]
	t.Assert(ok, c.Equals, true)
	var apps map[string]*ct.ExpandedFormation
	t.Assert(json.Unmarshal(flynn, &apps), c.IsNil)
	for _, name := range []string{"postgres", "discoverd", "flannel", "controller"} {
		ef, ok := apps[name]
		t.Assert(ok, c.Equals, true)
		t.Assert(ef.App, c.Not(c.IsNil))
		t.Assert(ef.Release, c.Not(c.IsNil))
		t.Assert(ef.Processes, c.Not(c.IsNil))
		t.Assert(ef.App.Name, c.Equals, name)
	}
}
