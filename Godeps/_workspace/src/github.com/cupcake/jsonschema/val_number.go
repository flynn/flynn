package jsonschema

import (
	"encoding/json"
	"fmt"
	"strings"
)

type maximum struct {
	json.Number
	exclusive bool
}

func (m maximum) isLargerThanInt(n int64) (bool, error) {
	if !strings.Contains(m.String(), ".") {
		max, err := m.Int64()
		if err != nil {
			return false, err
		}
		return max > n || !m.exclusive && max == n, nil
	} else {
		return m.isLargerThanFloat(float64(n))
	}
}

func (m maximum) isLargerThanFloat(n float64) (isLarger bool, err error) {
	max, err := m.Float64()
	if err != nil {
		return
	}
	return max > n || !m.exclusive && max == n, nil
}

func (m *maximum) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &m.Number)
}

func (m *maximum) SetSchema(v map[string]json.RawMessage) error {
	value, ok := v["exclusiveMaximum"]
	if ok {
		// Ignore errors from Unmarshal. If exclusiveMaximum is a non boolean JSON
		// value we leave it as false.
		json.Unmarshal(value, &m.exclusive)
	}
	return nil
}

func (m maximum) Validate(v interface{}) []ValidationError {
	normalized, err := normalizeNumber(v)
	if err != nil {
		return []ValidationError{{err.Error()}}
	}
	var isLarger bool
	switch n := normalized.(type) {
	case int64:
		isLarger, err = m.isLargerThanInt(n)
	case float64:
		isLarger, err = m.isLargerThanFloat(n)
	default:
		return nil
	}
	if err != nil {
		return nil
	}
	if !isLarger {
		maxErr := fmt.Sprintf("Value must be smaller than %s.", m)
		return []ValidationError{{maxErr}}
	}
	return nil
}

type minimum struct {
	json.Number
	exclusive bool
}

func (m minimum) isLargerThanInt(n int64) (bool, error) {
	if !strings.Contains(m.String(), ".") {
		min, err := m.Int64()
		if err != nil {
			return false, nil
		}
		return min > n || !m.exclusive && min == n, nil
	} else {
		return m.isLargerThanFloat(float64(n))
	}
}

func (m minimum) isLargerThanFloat(n float64) (isLarger bool, err error) {
	min, err := m.Float64()
	if err != nil {
		return
	}
	return min > n || !m.exclusive && min == n, nil
}

func (m *minimum) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &m.Number)
}

func (m *minimum) SetSchema(v map[string]json.RawMessage) error {
	value, ok := v["exclusiveminimum"]
	if ok {
		// Ignore errors from Unmarshal. If exclusiveminimum is a non boolean JSON
		// value we leave it as false.
		json.Unmarshal(value, &m.exclusive)
	}
	return nil
}

func (m minimum) Validate(v interface{}) []ValidationError {
	normalized, err := normalizeNumber(v)
	if err != nil {
		return []ValidationError{{err.Error()}}
	}
	var isLarger bool
	switch n := normalized.(type) {
	case int64:
		isLarger, err = m.isLargerThanInt(n)
	case float64:
		isLarger, err = m.isLargerThanFloat(n)
	default:
		return nil
	}
	if err != nil {
		return nil
	}
	if isLarger {
		minErr := fmt.Sprintf("Value must be larger than %s.", m)
		return []ValidationError{{minErr}}
	}
	return nil
}

type multipleOf int64

func (m *multipleOf) UnmarshalJSON(b []byte) error {
	var n int64
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*m = multipleOf(n)
	return nil
}

// Contrary to the spec, validation doesn't support floats in the schema
// or the data being validated. This is because of issues with math.Mod,
// e.g. math.Mod(0.0075, 0.0001) != 0.
func (m multipleOf) Validate(v interface{}) []ValidationError {
	normalized, err := normalizeNumber(v)
	if err != nil {
		return []ValidationError{{err.Error()}}
	}
	n, ok := normalized.(int64)
	if !ok {
		return nil
	}
	if n%int64(m) != 0 {
		mulErr := ValidationError{fmt.Sprintf("Value must be a multiple of %d.", m)}
		return []ValidationError{mulErr}
	}
	return nil
}
