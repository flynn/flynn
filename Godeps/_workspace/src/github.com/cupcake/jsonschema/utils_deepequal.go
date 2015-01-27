// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Deep equality test via reflection

package jsonschema

import (
	"encoding/json"
	"reflect"
	"strings"
)

// NEW FOR JSONSCHEMA
var stringType = reflect.TypeOf("")
var boolType = reflect.TypeOf(false)
var jsonNumberType = reflect.TypeOf(json.Number(""))

// During deepValueEqual, must keep track of checks that are
// in progress.  The comparison algorithm assumes that all
// checks in progress are true when it reencounters them.
// Visited comparisons are stored in a map indexed by visit.
type visit struct {
	a1  uintptr
	a2  uintptr
	typ reflect.Type
}

// Tests for deep equality using reflected types. The map argument tracks
// comparisons that have already been seen, which allows short circuiting on
// recursive types.
func deepValueEqual(v1, v2 reflect.Value, visited map[visit]bool, depth int) bool {
	if !v1.IsValid() || !v2.IsValid() {
		return v1.IsValid() == v2.IsValid()
	}

	// BEGIN NEW FOR JSONSCHEMA
	//
	// Go's deepValueEqual uses this line at the end of the v1.Kind() switch below to compare simple types:
	//
	//     return valueInterface(v1, false) == valueInterface(v2, false)
	//
	// this isn't an option for us because valueInterface isn't exported. We need to implement our own
	// comparison anyway because we want to make some type conversions (e.g. from json.Number to int64)
	// before testing for equality.
	//
	// So instead we handle simple types with our own type switch here before the v1.Kind() switch is run.
	//
	//
	// NOTE: We use Type() for the switch here instead of Kind() like in the v1.Kind() switch below,
	// because reflect.Kind doesn't distinguish between json.Number and string.
	//
	// NOTE: We use the switch on v2 here instead of v1 like the switch below. Since this package controls
	// the deserialization of schemas we know that if v2 is a number it will always be a json.Number.
	// We can't say the same for v1.
	//
	// TODO: We need to add tests where v2 is an integer and v1 is a float. Then we need code to cover
	// that situation.
	b1 := v1.Interface()
	b2 := v2.Interface()
	switch v2.Type() {
	case stringType:
		c1, ok1 := b1.(string)
		c2, ok2 := b2.(string)
		if !ok1 || !ok2 {
			return false
		}
		return c1 == c2
	case boolType:
		c1, ok1 := b1.(bool)
		c2, ok2 := b2.(bool)
		if !ok1 || !ok2 {
			return false
		}
		return c1 == c2
	case jsonNumberType:
		norm, err := normalizeNumber(b1)
		if err != nil {
			return false
		}
		jnum, ok := b2.(json.Number)
		if !ok {
			return false
		}
		if strings.Contains(jnum.String(), ".") {
			c2, err := jnum.Float64()
			if err != nil {
				return false
			}
			c1, ok := norm.(float64)
			if !ok {
				return false
			}
			return c1 == c2
		} else {
			c2, err := jnum.Int64()
			if err != nil {
				return false
			}
			c1, ok := norm.(int64)
			if !ok {
				return false
			}
			return c1 == c2
		}
	}
	// END NEW FOR JSONSCHEMA

	if v1.Type() != v2.Type() {
		return false
	}

	// if depth > 10 { panic("deepValueEqual") }	// for debugging
	hard := func(k reflect.Kind) bool {
		switch k {
		case reflect.Array, reflect.Map, reflect.Slice, reflect.Struct:
			return true
		}
		return false
	}

	if v1.CanAddr() && v2.CanAddr() && hard(v1.Kind()) {
		addr1 := v1.UnsafeAddr()
		addr2 := v2.UnsafeAddr()
		if addr1 > addr2 {
			// Canonicalize order to reduce number of entries in visited.
			addr1, addr2 = addr2, addr1
		}

		// Short circuit if references are identical ...
		if addr1 == addr2 {
			return true
		}

		// ... or already seen
		typ := v1.Type()
		v := visit{addr1, addr2, typ}
		if visited[v] {
			return true
		}

		// Remember for later.
		visited[v] = true
	}

	switch v1.Kind() {
	case reflect.Array:
		if v1.Len() != v2.Len() {
			return false
		}
		for i := 0; i < v1.Len(); i++ {
			if !deepValueEqual(v1.Index(i), v2.Index(i), visited, depth+1) {
				return false
			}
		}
		return true
	case reflect.Slice:
		if v1.IsNil() != v2.IsNil() {
			return false
		}
		if v1.Len() != v2.Len() {
			return false
		}
		if v1.Pointer() == v2.Pointer() {
			return true
		}
		for i := 0; i < v1.Len(); i++ {
			if !deepValueEqual(v1.Index(i), v2.Index(i), visited, depth+1) {
				return false
			}
		}
		return true
	case reflect.Interface:
		if v1.IsNil() || v2.IsNil() {
			return v1.IsNil() == v2.IsNil()
		}
		return deepValueEqual(v1.Elem(), v2.Elem(), visited, depth+1)
	case reflect.Ptr:
		return deepValueEqual(v1.Elem(), v2.Elem(), visited, depth+1)
	case reflect.Struct:
		for i, n := 0, v1.NumField(); i < n; i++ {
			if !deepValueEqual(v1.Field(i), v2.Field(i), visited, depth+1) {
				return false
			}
		}
		return true
	case reflect.Map:
		if v1.IsNil() != v2.IsNil() {
			return false
		}
		if v1.Len() != v2.Len() {
			return false
		}
		if v1.Pointer() == v2.Pointer() {
			return true
		}
		for _, k := range v1.MapKeys() {
			if !deepValueEqual(v1.MapIndex(k), v2.MapIndex(k), visited, depth+1) {
				return false
			}
		}
		return true
	case reflect.Func:
		if v1.IsNil() && v2.IsNil() {
			return true
		}
		// Can't do better than this:
		return false
	default:

		// REMOVED FOR JSONSCHEMA
		//
		// // Normal equality suffices
		// return valueInterface(v1, false) == valueInterface(v2, false)

		// ADDED FOR JSONSCHEMA
		return false
	}
}

// DeepEqual tests for deep equality. It uses normal == equality where
// possible but will scan elements of arrays, slices, maps, and fields of
// structs. In maps, keys are compared with == but elements use deep
// equality. DeepEqual correctly handles recursive types. Functions are equal
// only if they are both nil.
// An empty slice is not equal to a nil slice.
func DeepEqual(a1, a2 interface{}) bool {
	if a1 == nil || a2 == nil {
		return a1 == a2
	}
	v1 := reflect.ValueOf(a1)
	v2 := reflect.ValueOf(a2)

	// REMOVED FOR JSONSCHEMA
	//
	// if v1.Type() != v2.Type() {
	// 	return false
	// }

	return deepValueEqual(v1, v2, make(map[visit]bool), 0)
}
