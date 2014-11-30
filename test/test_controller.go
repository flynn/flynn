package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cupcake/jsonschema"
	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

type ControllerSuite struct {
	schemaPaths []string
	schemaCache map[string]*jsonschema.Schema
	Helper
}

var _ = c.Suite(&ControllerSuite{})

func (s *ControllerSuite) SetUpSuite(t *c.C) {
	s.clusterConf(t)

	var schemaPaths []string
	walkFn := func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			schemaPaths = append(schemaPaths, path)
		}
		return nil
	}
	schemaRoot, err := filepath.Abs(filepath.Join("..", "website", "schema"))
	t.Assert(err, c.IsNil)
	filepath.Walk(schemaRoot, walkFn)

	s.schemaCache = make(map[string]*jsonschema.Schema, len(schemaPaths))
	for _, path := range schemaPaths {
		file, err := os.Open(path)
		t.Assert(err, c.IsNil)
		schema := &jsonschema.Schema{Cache: s.schemaCache}
		err = schema.ParseWithoutRefs(file)
		t.Assert(err, c.IsNil)
		cacheKey := "https://flynn.io/schema" + strings.TrimSuffix(filepath.Base(path), ".json")
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

var jsonContentTypeMatcher = regexp.MustCompile(`\bjson`)

func unmarshalControllerExample(data []byte) (map[string]interface{}, error) {
	var example controllerExample
	if err := json.Unmarshal(data, &example); err != nil {
		return nil, err
	}

	if jsonContentTypeMatcher.MatchString(example.Request.Headers["Content-Type"]) {
		if body, ok := example.Request.Body.(string); ok {
			var reqBody interface{}
			if err := json.Unmarshal([]byte(body), &reqBody); err != nil {
				return nil, err
			}
			example.Request.Body = reqBody
		}
	}
	if jsonContentTypeMatcher.MatchString(example.Response.Headers["Content-Type"]) {
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
	err = json.Unmarshal(rawData, &out)
	return out, err
}

func (s *ControllerSuite) generateControllerExamples(t *c.C) map[string]interface{} {
	controllerDomain := strings.TrimPrefix(s.config.URL, "https://")
	examplesCmd := exec.Command(args.ControllerExamples)
	env := os.Environ()
	env = append(env, fmt.Sprintf("CONTROLLER_DOMAIN=%s", controllerDomain))
	env = append(env, fmt.Sprintf("CONTROLLER_KEY=%s", s.config.Key))
	if ips, err := net.LookupIP(controllerDomain); err == nil && len(ips) > 0 {
		env = append(env, fmt.Sprintf("ADDR=%s", ips[0]))
	}
	examplesCmd.Env = env

	out, err := examplesCmd.Output()
	t.Assert(err, c.IsNil)
	var controllerExamples map[string]json.RawMessage
	err = json.Unmarshal(out, &controllerExamples)
	t.Assert(err, c.IsNil)

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
	for key, data := range examples {
		cacheKey := "https://flynn.io/schema/examples/controller/" + key
		schema := s.schemaCache[cacheKey]
		t.Assert(schema, c.Not(c.IsNil))
		errs := schema.Validate(data)
		t.Assert(errs, c.HasLen, 0, c.Commentf("%s validation errors: %v\n", cacheKey, errs))
	}
}
