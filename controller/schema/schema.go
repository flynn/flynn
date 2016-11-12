package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/cupcake/jsonschema"
	ct "github.com/flynn/flynn/controller/types"
)

var schemaCache map[string]*jsonschema.Schema

func Load(schemaRoot string) error {
	if schemaCache != nil {
		return nil
	}

	var schemaPaths []string
	walkFn := func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() && filepath.Ext(path) == ".json" {
			schemaPaths = append(schemaPaths, path)
		}
		return nil
	}
	filepath.Walk(schemaRoot, walkFn)

	schemaCache = make(map[string]*jsonschema.Schema, len(schemaPaths))
	for _, path := range schemaPaths {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		schema := &jsonschema.Schema{Cache: schemaCache}
		err = schema.ParseWithoutRefs(file)
		file.Close()
		if err != nil {
			return fmt.Errorf("schema: Error loading schema %s: %s", path, err)
		}
		cacheKey := "https://flynn.io/schema" + strings.TrimSuffix(filepath.Base(path), ".json")
		schemaCache[cacheKey] = schema
	}
	for _, schema := range schemaCache {
		schema.ResolveRefs(false)
	}

	return nil
}

func schemaForType(thing interface{}) *jsonschema.Schema {
	name := strings.ToLower(reflect.Indirect(reflect.ValueOf(thing)).Type().Name())
	if name == "newjob" {
		name = "new_job"
	}
	if name == "scalerequest" {
		name = "scale_request"
	}
	if name == "appupdate" {
		name = "app"
	}
	if name == "route" {
		return schemaCache["https://flynn.io/schema/router/route"]
	}
	cacheKey := "https://flynn.io/schema/controller/" + name
	return schemaCache[cacheKey]
}

func Validate(thing interface{}) error {
	schema := schemaForType(thing)
	if schema == nil {
		return errors.New(fmt.Sprintf("schema: Unknown resource: %T %v", thing, thing))
	}

	var data []byte
	var err error
	if data, err = json.Marshal(thing); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var validateData map[string]interface{}
	if err := decoder.Decode(&validateData); err != nil {
		return err
	}

	schemaErrs := schema.Validate(nil, validateData)
	if len(schemaErrs) > 0 {
		err := schemaErrs[0]
		return ct.ValidationError{
			Message: err.Description,
			Field:   err.DotNotation(),
		}
	}

	return nil
}
