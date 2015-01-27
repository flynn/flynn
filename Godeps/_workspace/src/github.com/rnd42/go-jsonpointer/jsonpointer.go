// jsonpointer follows the IETF RFC 6901 spec for dereferenceing arbitrary
// values from JSON data structures.  See http://tools.ietf.org/html/rfc6901 for
// the JSON Pointer spec.
//
// Note that unless otherwise stated code examples are evaluated against the
// example data structure from http://tools.ietf.org/html/rfc6901#section-5 :
//
//     {
//         "foo": ["bar", "baz"],
//         "": 0,
//         "a/b": 1,
//         "c%d": 2,
//         "e^f": 3,
//         "g|h": 4,
//         "i\\j": 5,
//         "k\"l": 6,
//         " ": 7,
//         "m~n": 8
//     }
//
// License
//
// This package is licensed under the ISC License, a modernized MIT/BSD
// equivelent license, see: https://en.wikipedia.org/wiki/ISC_license or the
// LICENSE file in the source directory for further details.
//
package jsonpointer

// Source code is organized to appear in the same order it appears in godoc.

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ArrayIndexOutOfBounds errors indicate that a valid, numeric index is beyond
// the bounds of the array (slice) being dereferenced.
type ArrayIndexOutOfBounds struct {
	index int
}

// Error Returns a informative error message as a string
func (e ArrayIndexOutOfBounds) Error() string {
	return fmt.Sprintf("array index %d is beyond the array bounds.", e.index)
}

var badEscapeSequence = regexp.MustCompile("~[^01]")

// BadEscapeSequence errors indicate that the provided JSON pointer string
// contained an invalid escape sequence.  Currenly the only allowed escape
// sequences are "~0" which escapes "~" and "~1" which escapes "/".  The '~'
// character must not be followed by any character other than '0' or '1'.
type BadEscapeSequence struct {
	pointer  string
	sequence string
}

// Error Returns a informative error message as a string
func (e BadEscapeSequence) Error() string {
	return fmt.Sprintf(
		"the escape sequence %q found in the JSON pointer %q is invalid",
		e.sequence,
		e.pointer)
}

// InvalidArrayIndex errors indicate that the JSONPointer could not dereference
// an array using the given token (I.E. the token was not numberic, or '-' in
// the case of Set() calls.)
type InvalidArrayIndex struct {
	index string
}

// Error Returns a informative error message as a string
func (e InvalidArrayIndex) Error() string {
	return fmt.Sprintf("cannot index an array by the value %q", e.index)
}

// JSONPointer follows IETF RFC 6901 for using a string value to test, access
// and/or modify a JSON data structure.  The zero value of a JSONPointer would
// be have zero tokens and a string representation of "" (the empty string.)
// This zero value is functionally usable and targets the root document itself.
type JSONPointer struct {
	pointer string
	tokens  []string
}

// NewJSONPointer attempts to create a new JSONPointer instance from it's string
// representation according to IETF RFC 6901 rules.  The primary use case for
// this would be to query a JSON data structure.
func NewJSONPointerFromString(pointer string) (*JSONPointer, error) {
	if len(pointer) != 0 && pointer[0] != '/' {
		return nil, JSONPointerSyntaxError{pointer}
	}
	bytes := []byte(pointer)
	if at := badEscapeSequence.FindIndex(bytes); at != nil {
		sequence := string(bytes[at[0]:at[1]])
		return nil, BadEscapeSequence{pointer, sequence}
	}
	tokens := strings.Split(pointer, "/")[1:] // remove the leading ""
	for index, token := range tokens {
		tokens[index] = strings.Replace(
			strings.Replace(token, "~1", "/", -1),
			"~0",
			"~",
			-1)
	}
	p := JSONPointer{pointer, tokens}
	return &p, nil
}

// NewJSONPointerFromTokens attempts to create a new JSONPointer instance from
// a slice of strings representing it's individual tokens.  The primary use case
// for this would be to convert a sequence of tokens into an IETF RFC 6901
// compliant string representation.
func NewJSONPointerFromTokens(tokens *[]string) *JSONPointer {
	pointers := make([]string, len(*tokens), len(*tokens))
	for index, token := range *tokens {
		pointers[index] = strings.Replace(
			strings.Replace(token, "~", "~0", -1),
			"/",
			"~1",
			-1)
	}
	var pointer string = ""
	if len(*tokens) != 0 {
		pointer = "/" + strings.Join(pointers, "/")
	}
	p := JSONPointer{pointer, *tokens}
	return &p
}

// NewJSONPointerFromURIFragment works just like NewJSONPointerFromString except
// that it will unescape URL escape sequences (like %20 for a space).  A leading
// '#' fragment root token is optional in light of the fact that it is stripped
// from net/url standard library Url struct "Fragment" fields.
func NewJSONPointerFromURIFragment(fragment string) (*JSONPointer, error) {
	if fragment[0] == '#' {
		fragment = fragment[1:]
	}
	pointer, err := url.QueryUnescape(fragment)
	if err != nil {
		return nil, err
	}
	return NewJSONPointerFromString(pointer)
}

// Depth returns the number of tokens in a JSON Pointer.
func (p *JSONPointer) Depth() int {
	return len(p.tokens)
}

// Get returns the value from the provided JSON data structure identified by
// this JSONPointer.  The `depth` argument can be used to limit the number of
// tokens that will be evaluated into the structure, a value of -1 will evaluate
// the entire token chain, a value of 0 will return the provided data structure
// itself.
func (p *JSONPointer) Get(data interface{}, depth int) (interface{}, error) {
	result := data // Avoid changing the provided pointer's value.
	if depth < 0 || depth > len(p.tokens) {
		depth = len(p.tokens)
	}
	for _, token := range p.tokens[:depth] {
		switch target := result.(type) {
		case map[string]interface{}:
			result = target[token]
		case []interface{}:
			index, err := strconv.Atoi(token)
			if err != nil {
				return nil, InvalidArrayIndex{token}
			} else if index >= len(target) || index < 0 {
				return nil, ArrayIndexOutOfBounds{index}
			}
			result = target[index]
		default:
			return nil, UnindexableValue{result}
		}
	}
	return result, nil
}

// Set changes the value from the provided JSON data structure identified by
// this JSONPointer.  The `depth` argument can be used to limit the number of
// tokens that will be evaluated into the structure, a value of -1 will evaluate
// the entire token chain, a value of 0 will return the provided data structure
// itself.  Set always returns the provided data structure, this way if an
// append occurs on the root level document you will receive the updated array
// which may have been reallocated by Go.
func (p *JSONPointer) Set(data interface{}, value interface{}, depth int) (interface{}, error) {
	if depth < 0 || depth > len(p.tokens) {
		depth = len(p.tokens)
	}
	genericTarget, err := p.Get(data, depth-1) // Get the parent element.
	if err != nil {
		return data, err
	}
	token := p.tokens[depth-1] // get the final token.
	switch target := genericTarget.(type) {
	case map[string]interface{}:
		target[token] = value
	case []interface{}:
		arrayLen := len(target)
		var index int
		if token == "-" {
			index = arrayLen
		} else {
			index, err = strconv.Atoi(token)
			if err != nil {
				return nil, InvalidArrayIndex{token}
			}
		}
		if index > arrayLen {
			return nil, ArrayIndexOutOfBounds{index}
		} else if index == arrayLen {
			if depth == 1 {
				data = append(target, value)
			} else {
				target = append(target, value)
				genericTarget = target
				p.Set(data, genericTarget, depth-1)
			}
		} else {
			target[index] = value
		}
	default:
		return nil, UnindexableValue{genericTarget}
	}
	return data, nil
}

// String returns the unescaped IETF RFC 6901 string representation of this JSON
// pointer.
func (p *JSONPointer) String() string {
	return p.pointer
}

// Tokens returns the escaped tokens that will be used to dereference a JSON
// data structure.
func (p *JSONPointer) Tokens() []string {
	return p.tokens
}

// JSONPointerSyntaxError indicates that the provided string representation of a
// JSON pointer is invalid.  To be a valid JSON pointer string it must either:
// be the empty string (""), or start with a forward slash ("/").
type JSONPointerSyntaxError struct {
	pointer string
}

// Error Returns a informative error message as a string
func (e JSONPointerSyntaxError) Error() string {
	return fmt.Sprintf(
		"%q is not a valid JSON pointer string representation.",
		e.pointer)
}

// UnindexableValue error indicates that the JSONPointer reached a primitave
// value which cannot be indexed into before running out of tokens to
// dereference.
type UnindexableValue struct {
	value interface{}
}

// Error Returns a informative error message as a string
func (e UnindexableValue) Error() string {
	repr := fmt.Sprintf("%v", e.value)
	if s, ok := e.value.(string); ok {
		repr = `"` + s + `"`
	}
	return "cannot index primitave value " + repr
}
