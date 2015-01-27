package jsonpointer

// Source code is organized to appear in the same order it appears in godoc.

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

var ExampleJson map[string]interface{}

var JSONBytes []byte = []byte(`{
    "foo": ["bar", "baz"],
    "": 0,
    "a/b": 1,
    "c%d": 2,
    "e^f": 3,
    "g|h": 4,
    "i\\j": 5,
    "k\"l": 6,
    " ": 7,
    "m~n": 8
}`)

func init() {
	err := json.Unmarshal([]byte(JSONBytes), &ExampleJson)
	if err != nil {
		fmt.Printf("Failed to unmarshall example JSON:\n%s\n", JSONBytes)
		fmt.Printf("err: %q", err.Error())
		os.Exit(1)
	}
}

func TestArrayIndexOutOfBounds(t *testing.T) {
	pointer, _ := NewJSONPointerFromString("/foo/3")
	_, err := pointer.Set(ExampleJson, "shouldn't happen", -1)
	if _, ok := err.(ArrayIndexOutOfBounds); !ok {
		t.Log("TestArrayIndexOutOfBounds did not get a " +
			"ArrayIndexOutOfBounds error.")
	}
}

// Calling Get with a pointer value that dereferences an array index beyond it's
// bounds will return an ArrayIndexOutOfBounds error.  When calling set this
// also applies except that if the index is equal to the array length it is
// considered valid and performs ano": ["bar", "baz"], append.  See above for
// the JSON data being evaluated.
func ExampleArrayIndexOutOfBounds_Error() {
	pointer, _ := NewJSONPointerFromString("/foo/2")
	_, err := pointer.Get(ExampleJson, -1)
	fmt.Println(err)
	// output:
	// array index 2 is beyond the array bounds.
}

func TestBadEscapeSequence(t *testing.T) {
	_, err := NewJSONPointerFromString("/foo/~~")
	_, ok := err.(BadEscapeSequence)
	if !ok {
		t.Log("NewJSONPointer should return a BadEscapeSequence error for the" +
			"string \"/foo/~~\"")
		t.Fail()
	}
}

// Providing a string with an invalid escape sequence to NewJSONPointer will
// return a BadEscapeSequence error.  See above for the JSON data being
// evaluated.
func ExampleBadEscapeSequence_Error() {
	_, err := NewJSONPointerFromString("/foo/~2")
	fmt.Print(err)
	// output:
	// the escape sequence "~2" found in the JSON pointer "/foo/~2" is invalid
}

func TestInvalidArrayIndex(t *testing.T) {
	pointer, _ := NewJSONPointerFromString("/foo/!")
	_, err := pointer.Set(ExampleJson, "shouldn't happen!", -1)
	if _, ok := err.(InvalidArrayIndex); !ok {
		t.Log("TestInvalidArrayIndex did not get a InvalidArrayIndex error.")
	}
	// output:
	// cannot index an array by the value "!"
}

// Calling Get or Set with a pointer value that dereferences an array index
// where the token is non-numeric will return an InvalidArrayIndex error.  When
// calling Set this also applies except that the token "-" is evaluated to the
// length of the array (and thus performs an append.)  See above for the JSON
// data being evaluated.
func ExampleInvalidArrayIndex_Error() {
	pointer, _ := NewJSONPointerFromString("/foo/!")
	_, err := pointer.Get(ExampleJson, -1)
	fmt.Println(err)
	// output:
	// cannot index an array by the value "!"
}

// This example represents the example tests outlined in setion 5 of IETF RFC
// 6901 (http://tools.ietf.org/html/rfc6901#section-5) which all pass:
func ExampleNewJSONPointerFromString() {
	pointer, _ := NewJSONPointerFromString("")
	value, _ := pointer.Get(ExampleJson, -1)
	fmt.Println(reflect.DeepEqual(value, ExampleJson))

	pointer, _ = NewJSONPointerFromString("/foo")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(reflect.DeepEqual(value, ExampleJson["foo"]))

	pointer, _ = NewJSONPointerFromString("/foo/0")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(reflect.DeepEqual(value, "bar"))

	pointer, _ = NewJSONPointerFromString("/")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(0))

	pointer, _ = NewJSONPointerFromString("/a~1b")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(1))

	pointer, _ = NewJSONPointerFromString("/c%d")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(2))

	pointer, _ = NewJSONPointerFromString("/e^f")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(3))

	pointer, _ = NewJSONPointerFromString("/g|h")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(4))

	pointer, _ = NewJSONPointerFromString("/i\\j")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(5))

	pointer, _ = NewJSONPointerFromString("/k\"l")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(6))

	pointer, _ = NewJSONPointerFromString("/ ")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(7))

	pointer, _ = NewJSONPointerFromString("/m~0n")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(8))
	// output:
	// true
	// true
	// true
	// true
	// true
	// true
	// true
	// true
	// true
	// true
	// true
	// true
}

// This example represents the example tests outlined in setion 5 of IETF RFC
// 6901 as they would be implemented with NewJSONPointerFromTokens for
// comparison with the other constructor methods.  Note that this method has no
// error conditions providing a nicer (or at least more chainable) API.
func ExampleNewJSONPointerFromTokens() {
	value, _ := NewJSONPointerFromTokens(&[]string{}).Get(ExampleJson, -1)
	fmt.Println(reflect.DeepEqual(value, ExampleJson))

	value, _ = NewJSONPointerFromTokens(&[]string{"foo"}).Get(ExampleJson, -1)
	fmt.Println(reflect.DeepEqual(value, ExampleJson["foo"]))

	value, _ = NewJSONPointerFromTokens(&[]string{"foo", "0"}).Get(
		ExampleJson,
		-1)
	fmt.Println(reflect.DeepEqual(value, "bar"))

	value, _ = NewJSONPointerFromTokens(&[]string{""}).Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(0))

	value, _ = NewJSONPointerFromTokens(&[]string{"a/b"}).Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(1))

	value, _ = NewJSONPointerFromTokens(&[]string{"c%d"}).Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(2))

	value, _ = NewJSONPointerFromTokens(&[]string{"e^f"}).Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(3))

	value, _ = NewJSONPointerFromTokens(&[]string{"g|h"}).Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(4))

	value, _ = NewJSONPointerFromTokens(&[]string{"i\\j"}).Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(5))

	value, _ = NewJSONPointerFromTokens(&[]string{"k\"l"}).Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(6))

	value, _ = NewJSONPointerFromTokens(&[]string{" "}).Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(7))

	value, _ = NewJSONPointerFromTokens(&[]string{"m~n"}).Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(8))
	// output:
	// true
	// true
	// true
	// true
	// true
	// true
	// true
	// true
	// true
	// true
	// true
	// true
}

// This example represents the example tests outlined in setion 6 of IETF RFC
// 6901 (http://tools.ietf.org/html/rfc6901#section-6) which all pass.
func ExampleNewJSONPointerFromURIFragment() {
	pointer, _ := NewJSONPointerFromURIFragment("#")
	value, _ := pointer.Get(ExampleJson, -1)
	fmt.Println(reflect.DeepEqual(value, ExampleJson))

	pointer, _ = NewJSONPointerFromURIFragment("#/foo")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(reflect.DeepEqual(value, ExampleJson["foo"]))

	pointer, _ = NewJSONPointerFromURIFragment("#/foo/0")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(reflect.DeepEqual(value, "bar"))

	pointer, _ = NewJSONPointerFromURIFragment("#/")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(0))

	pointer, _ = NewJSONPointerFromURIFragment("#/a~1b")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(1))

	pointer, _ = NewJSONPointerFromURIFragment("#/c%25d")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(2))

	pointer, _ = NewJSONPointerFromURIFragment("#/e%5Ef")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(3))

	pointer, _ = NewJSONPointerFromURIFragment("#/g%7Ch")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(4))

	pointer, _ = NewJSONPointerFromURIFragment("#/i%5Cj")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(5))

	pointer, _ = NewJSONPointerFromURIFragment("#/k%22l")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(6))

	pointer, _ = NewJSONPointerFromURIFragment("#/%20")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(7))

	pointer, _ = NewJSONPointerFromURIFragment("#/m~0n")
	value, _ = pointer.Get(ExampleJson, -1)
	fmt.Println(value.(float64) == float64(8))
	// output:
	// true
	// true
	// true
	// true
	// true
	// true
	// true
	// true
	// true
	// true
	// true
	// true
}

func TestNewJSONPointerFromURIFragment(t *testing.T) {
	pointer, _ := NewJSONPointerFromURIFragment("/foo/%FF")
	if pointer == nil {
		t.Log("NewJSONPointerFromURIFragment should return a JSONPointer.")
		t.Fail()
	}
	_, err := NewJSONPointerFromURIFragment("#/foo/%G0") // Bad URI Escape
	if err == nil {
		t.Log("NewJSONPointerFromURIFragment should return an error for " +
			"invalid URI escape sequence %G0.")
		t.Fail()
	}
}

// Note that the first example is a JSON pointer to the root document, while the
// second example references the empty string key ("") directly under the root
// document.
func ExampleJSONPointer_Depth() {
	pointer, _ := NewJSONPointerFromString("")
	fmt.Println(pointer.Depth())

	pointer, _ = NewJSONPointerFromString("/")
	fmt.Println(pointer.Depth())

	pointer, _ = NewJSONPointerFromString("/foo")
	fmt.Println(pointer.Depth())

	pointer, _ = NewJSONPointerFromString("/foo/bar/baz")
	fmt.Println(pointer.Depth())
	// output:
	// 0
	// 1
	// 1
	// 3
}

func ExampleJSONPointer_Get() {
	pointer, err := NewJSONPointerFromString("/foo")
	value, err := pointer.Get(ExampleJson, -1)
	fmt.Println(value, err)

	pointer, err = NewJSONPointerFromString("/foo/0")
	value, err = pointer.Get(ExampleJson, -1)
	fmt.Println(value, err)

	pointer, err = NewJSONPointerFromString("/foo/0/boom")
	value, err = pointer.Get(ExampleJson, -1)
	fmt.Println(value, err)

	pointer, err = NewJSONPointerFromString("/foo/0/boom")
	value, err = pointer.Get(ExampleJson, 2)
	fmt.Println(value, err)
	// output:
	// [bar baz] <nil>
	// bar <nil>
	// <nil> cannot index primitave value "bar"
	// bar <nil>
}

func ExampleJSONPointer_Set() {
	// Construct the same JSON data structure used by other examples, but since
	// it will be modified we create a new instance that won't effect other
	// tests.
	var Json map[string]interface{}
	_ = json.Unmarshal([]byte(JSONBytes), &Json)

	pointer, _ := NewJSONPointerFromString("/foo/2")
	value, _ := pointer.Set(Json, "qux", -1)
	value, err := pointer.Get(value, 1)
	fmt.Println(value, err)

	// Lets see what happens with an array index greater than the length
	pointer, _ = NewJSONPointerFromString("/foo/4")
	_, err = pointer.Set(Json, "corge", -1)
	fmt.Println(err)

	// The '-' token is a special token that can only be used for Set operations
	// which will always append to the end of an array.
	pointer, _ = NewJSONPointerFromString("/foo/-")
	_, err = pointer.Set(Json, "corge", -1)
	value, _ = pointer.Get(Json, 1)
	fmt.Println(value)

	// Oops, that's the wrong metastatic variable!
	pointer, _ = NewJSONPointerFromString("/foo/3")
	_, err = pointer.Set(Json, "quux", -1)
	value, _ = pointer.Get(Json, 1)
	fmt.Println(value)

	pointer, _ = NewJSONPointerFromString("/o@p")
	_, _ = pointer.Set(Json, 9, -1)
	value, err = pointer.Get(Json, -1)
	fmt.Println(value)

	// Finally let's engineer a situation where an append must occur on the root
	// document to illustrate that in this corner case you will only see the
	// changed data if you capture the returned data:
	data := make([]interface{}, 2, 2)
	data[0], data[1] = "bar", "baz"
	pointer, _ = NewJSONPointerFromString("/2")
	value, _ = pointer.Set(data, "qux", -1)
	fmt.Println(data, value)
	// output:
	// [bar baz qux] <nil>
	// array index 4 is beyond the array bounds.
	// [bar baz qux corge]
	// [bar baz qux quux]
	// 9
	// [bar baz] [bar baz qux]
}

// One of the likely use cases for constructing JSONPointers from string slices
// as is done with NewJSONPointerFromTokens would be to construct a valid
// JSONPointer string for a given property/index path.  Here's how to do that:
func ExampleJSONPointer_String() {
	pointer := NewJSONPointerFromTokens(&[]string{})
	fmt.Printf("%q\n", pointer.String())

	pointer = NewJSONPointerFromTokens(&[]string{""})
	fmt.Printf("%q\n", pointer.String())

	pointer = NewJSONPointerFromTokens(&[]string{"foo"})
	fmt.Printf("%q\n", pointer.String())

	pointer = NewJSONPointerFromTokens(&[]string{"foo", "bar", "baz"})
	fmt.Printf("%q\n", pointer.String())
	// output:
	// ""
	// "/"
	// "/foo"
	// "/foo/bar/baz"
}

// Note that the difference between the first and second example is that the
// first is completely empty, while the second contains one string which happens
// to be the empty string ("".)
func ExampleJSONPointer_Tokens() {
	pointer, _ := NewJSONPointerFromString("")
	tokens := pointer.Tokens()
	fmt.Println(len(tokens), tokens)

	pointer, _ = NewJSONPointerFromString("/")
	tokens = pointer.Tokens()
	fmt.Println(len(tokens), tokens)

	pointer, _ = NewJSONPointerFromString("/foo")
	tokens = pointer.Tokens()
	fmt.Println(len(tokens), tokens)

	pointer, _ = NewJSONPointerFromString("/foo/bar/baz")
	tokens = pointer.Tokens()
	fmt.Println(len(tokens), tokens)
	// output:
	// 0 []
	// 1 []
	// 1 [foo]
	// 3 [foo bar baz]
}

func TestJSONPointerSyntaxError(t *testing.T) {
	_, err := NewJSONPointerFromString("boom")
	if _, ok := err.(JSONPointerSyntaxError); !ok {
		t.Log("TestJSONPointerSyntaxError did not get a JSONPointerSyntaxError")
		t.Fail()
	}
}

// Calling NewJSONPointerFromString with a string that neither is the empty
// string nor starts with a "/" is invalid and will return a
// JSONPointerSyntaxError
func ExampleJSONPointerSyntaxError_Error() {
	_, err := NewJSONPointerFromString("boom")
	fmt.Print(err)
	// output:
	// "boom" is not a valid JSON pointer string representation.
}

func TestUnindexableValue(t *testing.T) {
	pointer, _ := NewJSONPointerFromString("/foo/0/boom")
	_, err := pointer.Set(ExampleJson, "shouldn't happen!", -1)
	if _, ok := err.(UnindexableValue); !ok {
		t.Log("TestUnindexableValue did not get a UnindexableValue error.")
		t.Fail()
	}
}

// Calling Get or Set with a pointer that dereferences a primitave (null/nil,
// boolean/bool, number/float64 or string) before exausting all tokens to be
// evaluated will return a UnindexableValue error.  See above for the JSON data
// being evaluated.
func ExampleUnindexableValue_Error() {
	pointer, _ := NewJSONPointerFromString("/foo/0/boom/big")
	_, err := pointer.Set(ExampleJson, "badda", -1)
	fmt.Println(err)
	// output:
	// cannot index primitave value "bar"
}
