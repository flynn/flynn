package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cupcake/jsonschema"
	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/exec"
	"github.com/flynn/flynn/pkg/random"
)

type ControllerSuite struct {
	schemaPaths []string
	schemaCache map[string]*jsonschema.Schema
	Helper
}

var _ = c.Suite(&ControllerSuite{})

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
	cmd := exec.Command(exec.DockerImage(imageURIs["controller-examples"]), "/bin/flynn-controller-examples")
	cmd.Env = map[string]string{"CONTROLLER_KEY": s.clusterConf(t).Key}

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
	for key := range examples {
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
	cc := s.clusterConf(t)
	oldKey := cc.Key
	newKey := random.Hex(16)

	// allow auth to API with old and new keys
	set := flynn(t, "/", "-a", "controller", "env", "set", "-t", "web", fmt.Sprintf("AUTH_KEY=%s,%s", newKey, oldKey))
	t.Assert(set, Succeeds)

	// reconfigure components to use new key
	for _, app := range []string{"gitreceive", "taffy", "dashboard"} {
		set := flynn(t, "/", "-a", app, "env", "set", "CONTROLLER_KEY="+newKey)
		t.Assert(set, Succeeds)
	}

	// write a new flynnrc
	cc.Key = newKey
	conf := &config.Config{}
	err := conf.Add(cc, true)
	t.Assert(err, c.IsNil)
	err = conf.SaveTo(flynnrc)
	t.Assert(err, c.IsNil)

	// clear any cached configs
	s.Helper.config = nil
	s.Helper.controller = nil

	// use new key for deployer+controller
	set = flynn(t, "/", "-a", "controller", "env", "set", "AUTH_KEY="+newKey)
	t.Assert(set, Succeeds)

	// remove old key from API
	set = flynn(t, "/", "-a", "controller", "env", "unset", "-t", "web", "AUTH_KEY")
	t.Assert(set, Succeeds)
}

func (s *ControllerSuite) TestResourceLimitsOneOffJob(t *c.C) {
	app, release := s.createApp(t)

	rwc, err := s.controllerClient(t).RunJobAttached(app.ID, &ct.NewJob{
		ReleaseID: release.ID,
		Cmd:       []string{"sh", "-c", resourceCmd},
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
	err = watcher.WaitFor(ct.JobEvents{"resources": {"up": 1, "down": 1}}, scaleTimeout, func(e *ct.JobEvent) error {
		jobID = e.JobID
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
	t.Assert(r.flynn("key", "add", r.ssh.Pub), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)

	// wait for it to start
	service := app + "-web"
	_, err := s.discoverdClient(t).Instances(service, 10*time.Second)
	t.Assert(err, c.IsNil)

	// create some routes
	routes := []string{"foo.example.com", "bar.example.com"}
	for _, route := range routes {
		t.Assert(r.flynn("route", "add", "http", route), Succeeds)
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

	// delete app
	cmd := r.flynn("delete", "--yes")
	t.Assert(cmd, Succeeds)

	// check route cleanup
	t.Assert(cmd, OutputContains, fmt.Sprintf("removed %d routes", numRoutes))
	for _, route := range routes {
		assertRouteStatus(route, 404)
	}

	// check resource cleanup
	t.Assert(cmd, OutputContains, fmt.Sprintf("deprovisioned %d resources", numResources))

	// check creating and pushing same app name succeeds
	t.Assert(os.RemoveAll(r.dir), c.IsNil)
	r = s.newGitRepo(t, "http")
	t.Assert(r.flynn("create", app), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)
}

func (s *ControllerSuite) TestRouteEvents(t *c.C) {
	app := "app-route-events-" + random.String(8)
	client := s.controllerClient(t)

	// stream events
	events := make(chan *ct.Event)
	stream, err := client.StreamEvents(controller.StreamEventsOptions{
		AppID:       app,
		ObjectTypes: []ct.EventType{ct.EventTypeRoute, ct.EventTypeRouteDeletion},
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

	// create and push app
	r := s.newGitRepo(t, "http")
	t.Assert(r.flynn("create", app), Succeeds)
	t.Assert(r.flynn("key", "add", r.ssh.Pub), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)

	// wait for it to start
	service := app + "-web"
	_, err = s.discoverdClient(t).Instances(service, 10*time.Second)
	t.Assert(err, c.IsNil)

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
