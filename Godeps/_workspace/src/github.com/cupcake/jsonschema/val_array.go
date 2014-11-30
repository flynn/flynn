package jsonschema

import (
	"encoding/json"
	"fmt"
	"strconv"
)

type additionalItems struct {
	EmbeddedSchemas
	isTrue bool
}

func (a *additionalItems) UnmarshalJSON(b []byte) error {
	a.isTrue = true
	if err := json.Unmarshal(b, &a.isTrue); err == nil {
		return nil
	}
	return json.Unmarshal(b, &a.EmbeddedSchemas)
}

func (a additionalItems) Validate(v interface{}) []ValidationError {
	return nil
}

type maxItems int

func (m maxItems) Validate(v interface{}) []ValidationError {
	l, ok := v.([]interface{})
	if !ok {
		return nil
	}
	if len(l) > int(m) {
		maxErr := ValidationError{fmt.Sprintf("Array must have fewer than %d items.", m)}
		return []ValidationError{maxErr}
	}
	return nil
}

type minItems int

func (m minItems) Validate(v interface{}) []ValidationError {
	l, ok := v.([]interface{})
	if !ok {
		return nil
	}
	if len(l) < int(m) {
		minErr := ValidationError{fmt.Sprintf("Array must have more than %d items.", m)}
		return []ValidationError{minErr}
	}
	return nil
}

// The spec[1] is useless for this keyword. The implemention here is based on the tests and this[2] guide.
//
// [1] http://json-schema.org/latest/json-schema-validation.html#anchor37
// [2] http://spacetelescope.github.io/understanding-json-schema/reference/array.html
type items struct {
	EmbeddedSchemas
	schemaSlice       []*Schema
	additionalAllowed bool
	additionalItems   *Schema
}

func (i *items) UnmarshalJSON(b []byte) error {
	i.EmbeddedSchemas = make(EmbeddedSchemas)
	var s Schema
	if err := json.Unmarshal(b, &s); err == nil {
		i.EmbeddedSchemas[""] = &s
		return nil
	}
	if err := json.Unmarshal(b, &i.schemaSlice); err != nil {
		return err
	}
	for index, v := range i.schemaSlice {
		i.EmbeddedSchemas[strconv.Itoa(index)] = v
	}
	return nil
}

func (i *items) CheckNeighbors(m map[string]Node) {
	i.additionalAllowed = true
	v, ok := m["additionalItems"]
	if !ok {
		return
	}
	a, ok := v.Validator.(*additionalItems)
	if !ok {
		return
	}
	i.additionalAllowed = a.isTrue
	i.additionalItems = a.EmbeddedSchemas[""]
	return
}

func (i items) Validate(v interface{}) []ValidationError {
	var valErrs []ValidationError
	instances, ok := v.([]interface{})
	if !ok {
		return nil
	}
	if s, ok := i.EmbeddedSchemas[""]; ok {
		for _, value := range instances {
			valErrs = append(valErrs, s.Validate(value)...)
		}
	} else if len(i.schemaSlice) > 0 {
		for pos, value := range instances {
			if pos <= len(i.schemaSlice)-1 {
				s := i.schemaSlice[pos]
				valErrs = append(valErrs, s.Validate(value)...)
			} else if i.additionalAllowed {
				if i.additionalItems == nil {
					continue
				}
				valErrs = append(valErrs, i.additionalItems.Validate(value)...)
			} else if !i.additionalAllowed {
				return []ValidationError{{"Additional items aren't allowed."}}
			}
		}
	}
	return valErrs
}
