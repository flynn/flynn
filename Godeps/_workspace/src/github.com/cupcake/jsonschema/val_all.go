package jsonschema

import (
	"encoding/json"
	"fmt"
	"strings"
)

type allOf struct {
	EmbeddedSchemas
}

// TODO: though it isn't covered by tests, this is isn't right because if the JSON
// used to create allOf.EmbeddedSchemas is an object or a single schema it will
// still be unmarshaled into allOf.EmbeddedSchemas. allOf should only recognize an
// array of schemas, not an object or a single schema.
//
// This (and similar validators) need custom UnmarshalJSON methods.
func (a allOf) Validate(v interface{}) (valErrs []ValidationError) {
	for _, s := range a.EmbeddedSchemas {
		valErrs = append(valErrs, s.Validate(v)...)
	}
	return
}

type anyOf struct {
	EmbeddedSchemas
}

func (a anyOf) Validate(v interface{}) []ValidationError {
	for _, s := range a.EmbeddedSchemas {
		if s.Validate(v) == nil {
			return nil
		}
	}
	return []ValidationError{
		{"Validation failed for each schema in 'anyOf'."}}
}

type enum []interface{}

func (a enum) Validate(v interface{}) []ValidationError {
	for _, b := range a {
		if DeepEqual(v, b) {
			return nil
		}
	}
	return []ValidationError{
		{fmt.Sprintf("Enum error. The data must be equal to one of these values %v.", a)}}
}

type not struct {
	EmbeddedSchemas
}

func (n not) Validate(v interface{}) []ValidationError {
	s, ok := n.EmbeddedSchemas[""]
	if !ok {
		return nil
	}
	if s.Validate(v) == nil {
		return []ValidationError{{"The 'not' schema didn't raise an error."}}
	}
	return nil
}

type oneOf struct {
	EmbeddedSchemas
}

func (a oneOf) Validate(v interface{}) []ValidationError {
	var succeeded int
	for _, s := range a.EmbeddedSchemas {
		if s.Validate(v) == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		return []ValidationError{{
			fmt.Sprintf("Validation passed for %d schemas in 'oneOf'.", succeeded)}}
	}
	return nil
}

// A dummy schema used if we don't recognize a schema key. We unmarshal the key's contents anyway
// because it might contain embedded schemas referenced elsewhere in the document.
//
// NOTE: this is the only validator that is hardcoded instead of being listed in validatorMap.
type other struct {
	EmbeddedSchemas
}

func (o other) Validate(v interface{}) []ValidationError {
	return nil
}

type ref string

func (r ref) Validate(v interface{}) []ValidationError {
	return nil
}

type typeValidator map[string]bool

func (t *typeValidator) UnmarshalJSON(b []byte) error {
	*t = make(typeValidator)
	var s string
	var l []string

	// The value of the "type" keyword can be a string or an array.
	if err := json.Unmarshal(b, &s); err != nil {
		err = json.Unmarshal(b, &l)
		if err != nil {
			return err
		}
	} else {
		l = []string{s}
	}

	for _, val := range l {
		(*t)[val] = true
	}
	return nil
}

func (t typeValidator) Validate(v interface{}) []ValidationError {
	if _, ok := t["any"]; ok {
		return nil
	}

	var s string

	switch x := v.(type) {

	case string:
		s = "string"
	case bool:
		s = "boolean"
	case nil:
		s = "null"
	case []interface{}:
		s = "array"
	case map[string]interface{}:
		s = "object"

	case json.Number:
		if strings.Contains(x.String(), ".") {
			s = "number"
		} else {
			s = "integer"
		}
	case float64:
		s = "number"
	}

	_, ok := t[s]

	// The "number" type includes the "integer" type.
	if !ok && s == "integer" {
		_, ok = t["number"]
	}

	if !ok {
		types := make([]string, 0, len(t))
		for key := range t {
			types = append(types, key)
		}
		return []ValidationError{{
			fmt.Sprintf("Value must be one of these types: %s. Got %T: %v", types, v, v)}}
	}
	return nil
}
