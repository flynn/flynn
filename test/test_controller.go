package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cupcake/jsonschema"
	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/cli/config"
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

	events := make(chan *ct.JobEvent)
	stream, err := client.StreamJobEvents(app.ID, events)
	t.Assert(err, c.IsNil)
	defer stream.Close()

	t.Assert(client.PutFormation(&ct.Formation{
		AppID:     app.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"resources": 1},
	}), c.IsNil)
	jobID := waitForJobEvents(t, stream, events, jobEvents{"resources": {"up": 1, "down": 1}})
	log := flynn(t, "/", "-a", app.Name, "log", "--job", jobID, "--raw-output")

	assertResourceLimits(t, log.Output)
}
