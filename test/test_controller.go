package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cupcake/jsonschema"
	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/pkg/exec"
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
