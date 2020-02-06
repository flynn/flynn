// Copyright 2019 The Skycfg Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package skycfg

import (
	"fmt"
	"sort"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

// AssertModule contains assertion functions.
// The *TestContext returned can be used to track assertion failures.
// assert.* functions from this module will mutate the *TestContext.
// After execution is complete, TestContext.Failures will be non-empty
// if any of the assertions failed, and also contain details about the failures.
func AssertModule() *TestContext {
	ctx := &TestContext{}
	ctx.Attrs = starlark.StringDict{}

	// this loop populates the assert module with binary comparator functions
	// e.g. assert.equal, assert.lesser, etc.
	// soo tokenToString for all supported operations
	for op, str := range tokenToString {
		ctx.Attrs[str] = starlark.NewBuiltin(fmt.Sprintf("assert.%s", str), ctx.AssertBinaryImpl(op))
	}

	ctx.Attrs["fails"] = starlark.NewBuiltin("assert.fails", ctx.AssertFails)

	return ctx
}

// assertionError represents a failed assertion
type assertionError struct {
	op        *syntax.Token
	val1      starlark.Value
	val2      starlark.Value
	msg       string
	callStack starlark.CallStack
}

func (err assertionError) Error() string {
	callStack := err.callStack[:len(err.callStack)-1]
	position := callStack.At(0).Pos.String()
	backtrace := callStack.String()

	// use custom message if provided
	if err.msg != "" {
		return fmt.Sprintf("[%s] assertion failed: %s\n%s", position, err.msg, backtrace)
	}

	// straight boolean assertions like assert.true(false)
	if err.op == nil {
		return fmt.Sprintf("[%s] assertion failed\n%s", position, backtrace)
	}

	// binary assertions, like assert.eql(1, 2)
	return fmt.Sprintf(
		"[%s] assertion failed: %s (type: %s) %s %s (type: %s)\n%s",
		position,
		err.val1.String(),
		err.val1.Type(),
		err.op.String(),
		err.val2.String(),
		err.val2.Type(),
		backtrace,
	)
}

// TestContext is keeps track of whether there is a failure during a test execution
type TestContext struct {
	Attrs    starlark.StringDict
	Failures []error
}

var _ starlark.HasAttrs = (*Module)(nil)
var _ starlark.Value = (*TestContext)(nil)
var _ starlark.Callable = (*TestContext)(nil)

func (t *TestContext) Name() string          { return "assert" }
func (t *TestContext) String() string        { return "<test_context>" }
func (t *TestContext) Type() string          { return "test_context" }
func (t *TestContext) Freeze()               { t.Attrs.Freeze() }
func (t *TestContext) Truth() starlark.Bool  { return starlark.True }
func (t *TestContext) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable type: %s", t.Type()) }

func (t *TestContext) Attr(name string) (starlark.Value, error) {
	if val, ok := t.Attrs[name]; ok {
		return val, nil
	}
	return nil, nil
}

func (t *TestContext) AttrNames() []string {
	var names []string
	for name := range t.Attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// CallInternal is the implementation for assert(...)
func (t *TestContext) CallInternal(thread *starlark.Thread, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var val bool
	if err := starlark.UnpackPositionalArgs("assert", args, kwargs, 1, &val); err != nil {
		return nil, err
	}

	if !val {
		err := assertionError{
			callStack: thread.CallStack(),
		}
		t.Failures = append(t.Failures, err)
		return nil, err
	}

	return starlark.None, nil
}

// AssertBinaryImpl returns a function that implements comparing binary values in an assertion (i.e. assert_eq(1, 2))
func (t *TestContext) AssertBinaryImpl(op syntax.Token) func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var val1 starlark.Value
		var val2 starlark.Value
		if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 2, &val1, &val2); err != nil {
			return nil, err
		}

		passes, err := starlark.Compare(op, val1, val2)
		if err != nil {
			return nil, err
		}

		if !passes {
			err := assertionError{
				op:        &op,
				val1:      val1,
				val2:      val2,
				callStack: thread.CallStack(),
			}
			t.Failures = append(t.Failures, err)
			return nil, err
		}

		return starlark.None, nil
	}
}

func (t *TestContext) AssertFails(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("assert.fails: missing argument for fn")
	}

	failFn := args[0]
	failArgs := args[1:]

	if _, err := starlark.Call(thread, failFn, failArgs, kwargs); err != nil {
		if _, ok := err.(*starlark.EvalError); ok {
			// an eval error means the function failed and the assertion passes
			// return a struct with `message` as the string from the error
			s := starlark.NewBuiltin("struct", starlarkstruct.Make)
			result := starlarkstruct.FromStringDict(s, starlark.StringDict{
				"message": starlark.String(err.Error()),
			})
			return result, nil
		}

		return nil, err
	}

	// if no error was returned, the assertion fails
	err := assertionError{
		msg:       fmt.Sprintf("function %s should have failed", failFn.(starlark.Callable).Name()),
		callStack: thread.CallStack(),
	}
	t.Failures = append(t.Failures, err)
	return nil, err
}

var tokenToString = map[syntax.Token]string{
	syntax.LT:  "lesser",
	syntax.GT:  "greater",
	syntax.GE:  "greater_or_equal",
	syntax.LE:  "lesser_or_equal",
	syntax.EQL: "equal",
	syntax.NEQ: "not_equal",
}
