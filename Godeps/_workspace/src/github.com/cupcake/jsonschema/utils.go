package jsonschema

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// normalizeNumber accepts any input and, if it is a supported number type,
// converts it to either int64 or float64. normalizeNumber raises an error
// if the input is an explicitly unsupported number type.
func normalizeNumber(v interface{}) (n interface{}, err error) {
	switch t := v.(type) {

	case json.Number:
		if strings.Contains(t.String(), ".") {
			n, err = t.Float64()
		} else {
			n, err = t.Int64()
		}

	case float32:
		n = float64(t)
	case float64:
		n = t

	case int:
		n = int64(t)
	case int8:
		n = int64(t)
	case int16:
		n = int64(t)
	case int32:
		n = int64(t)
	case int64:
		n = t

	case uint8:
		n = int64(t)
	case uint16:
		n = int64(t)
	case uint32:
		n = int64(t)
	case uint64:
		n = t
		err = fmt.Errorf("%s is not a supported type", reflect.TypeOf(v))

	default:
		n = t
	}

	return
}
