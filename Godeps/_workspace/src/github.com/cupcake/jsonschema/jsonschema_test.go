package jsonschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

var notSupported = map[string]struct{}{"uniqueItems.json": {}}

func TestDraft4(t *testing.T) {
	suites := []string{
		filepath.Join("JSON-Schema-Test-Suite", "tests", "draft4"),
		filepath.Join("tests", "draft4"),
	}
	var failures, successes int
	schemaCache := make(map[string]*Schema)
	for _, testResources := range suites {
		if _, err := os.Stat(testResources); err != nil {
			t.Error("Test suite missing. Run `git submodule update --init` to download it.")
		}
		err := filepath.Walk(testResources, testFileRunner(t, &failures, &successes, &schemaCache))
		if err != nil {
			t.Error(err.Error())
		}
	}
	t.Logf("%d failed, %d succeeded.", failures, successes)
}

func testFileRunner(t *testing.T, failures, successes *int, schemaCache *map[string]*Schema) func(string, os.FileInfo, error) error {
	return func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if _, ok := notSupported[info.Name()]; ok {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		var testFile []testCase
		err = json.NewDecoder(file).Decode(&testFile)
		if err != nil {
			return err
		}

		for _, cse := range testFile {
			schema, parseErr := ParseWithCache(bytes.NewReader(cse.Schema), true, schemaCache)
			for _, tst := range cse.Tests {
				if parseErr != nil {
					t.Error(parseErrMessage(parseErr, path, cse, tst))
					*failures++
					continue
				}
				var data interface{}
				decoder := json.NewDecoder(bytes.NewReader(tst.Data))
				decoder.UseNumber()
				decoder.Decode(&data)
				errorList := schema.Validate(data)
				err = correctValidation(path, cse, tst, errorList)
				if err != nil {
					t.Error(failureMessage(err, path, cse, tst, errorList))
					*failures++
					continue
				}
				*successes++
			}
		}
		return nil
	}
}

type testCase struct {
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
	Properties  json.RawMessage `json:"properties"`
	Required    json.RawMessage `json:"required"`
	Tests       []testInstance  `json:"tests"`
}

type testInstance struct {
	Description string          `json:"description"`
	Data        json.RawMessage `json:"data"`
	Valid       bool            `json:"valid"`
}

func correctValidation(path string, cse testCase, tst testInstance, errorList []ValidationError) error {
	validated := (len(errorList) == 0)
	var failureName string
	if validated && !tst.Valid {
		failureName = "schema.Validate validated bad data."
	} else if !validated && tst.Valid {
		failureName = "schema.Validate failed to validate good data."
	}

	if len(failureName) > 0 {
		return errors.New(failureName)
	}
	return nil
}

func parseErrMessage(err error, path string, cse testCase, tst testInstance) string {
	return fmt.Sprintf(`error resolving $ref: %s
file: %s
test case description: %s
schema: %s
test instance description: %s
test data: %s

`, err.Error(), path, cse.Description, cse.Schema, tst.Description, tst.Data)
}

func failureMessage(err error, path string, cse testCase, tst testInstance, errorList []ValidationError) string {
	return fmt.Sprintf(`%s
file: %s
test case description: %s
schema: %s
test instance description: %s
test data: %s
result of schema.Validate:
	expected: %t
	actual: %t
	actual validation errors: %s

`, err.Error(), path, cse.Description, cse.Schema, tst.Description, tst.Data, tst.Valid, len(errorList) == 0, errorList)
}
