// Copyright 2018 The Skycfg Authors.
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

// Package skycfg is an extension library for the Starlark language that adds support
// for constructing Protocol Buffer messages.
package skycfg

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang/protobuf/proto"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	impl "github.com/stripe/skycfg/internal/go/skycfg"
)

// A FileReader controls how load() calls resolve and read other modules.
type FileReader interface {
	// Resolve parses the "name" part of load("name", "symbol") to a path. This
	// is not required to correspond to a true path on the filesystem, but should
	// be "absolute" within the semantics of this FileReader.
	//
	// fromPath will be empty when loading the root module passed to Load().
	Resolve(ctx context.Context, name, fromPath string) (path string, err error)

	// ReadFile reads the content of the file at the given path, which was
	// returned from Resolve().
	ReadFile(ctx context.Context, path string) ([]byte, error)
}

type localFileReader struct {
	root string
}

// LocalFileReader returns a FileReader that resolves and loads files from
// within a given filesystem directory.
func LocalFileReader(root string) FileReader {
	if root == "" {
		panic("LocalFileReader: empty root path")
	}
	return &localFileReader{root}
}

func (r *localFileReader) Resolve(ctx context.Context, name, fromPath string) (string, error) {
	if fromPath == "" {
		return name, nil
	}
	if filepath.Separator != '/' && strings.ContainsRune(name, filepath.Separator) {
		return "", fmt.Errorf("load(%q): invalid character in module name", name)
	}
	resolved := filepath.Join(r.root, filepath.FromSlash(path.Clean("/"+name)))
	return resolved, nil
}

func (r *localFileReader) ReadFile(ctx context.Context, path string) ([]byte, error) {
	return ioutil.ReadFile(path)
}

// NewProtoMessage returns a Starlark value representing the given Protobuf
// message. It can be returned back to a proto.Message() via AsProtoMessage().
func NewProtoMessage(msg proto.Message) starlark.Value {
	return impl.NewSkyProtoMessage(msg)
}

// AsProtoMessage returns a Protobuf message underlying the given Starlark
// value, which must have been created by NewProtoMessage(). Returns
// (_, false) if the value is not a valid message.
func AsProtoMessage(v starlark.Value) (proto.Message, bool) {
	return impl.ToProtoMessage(v)
}

// A Config is a Skycfg config file that has been fully loaded and is ready
// for execution.
type Config struct {
	filename string
	globals  starlark.StringDict
	locals   starlark.StringDict
	tests    []*Test
}

// A LoadOption adjusts details of how Skycfg configs are loaded.
type LoadOption interface {
	applyLoad(*loadOptions)
}

type loadOptions struct {
	globals       starlark.StringDict
	fileReader    FileReader
	protoRegistry impl.ProtoRegistry
}

type fnLoadOption func(*loadOptions)

func (fn fnLoadOption) applyLoad(opts *loadOptions) { fn(opts) }

type unstableProtoRegistry interface {
	impl.ProtoRegistry
}

// WithGlobals adds additional global symbols to the Starlark environment
// when loading a Skycfg config.
func WithGlobals(globals starlark.StringDict) LoadOption {
	return fnLoadOption(func(opts *loadOptions) {
		for key, value := range globals {
			opts.globals[key] = value
		}
	})
}

// WithFileReader changes the implementation of load() when loading a
// Skycfg config.
func WithFileReader(r FileReader) LoadOption {
	if r == nil {
		panic("WithFileReader: nil reader")
	}
	return fnLoadOption(func(opts *loadOptions) {
		opts.fileReader = r
	})
}

// WithProtoRegistry is an EXPERIMENTAL and UNSTABLE option to override
// how Protobuf message type names are mapped to Go types.
func WithProtoRegistry(r unstableProtoRegistry) LoadOption {
	if r == nil {
		panic("WithProtoRegistry: nil registry")
	}
	return fnLoadOption(func(opts *loadOptions) {
		opts.protoRegistry = r
	})
}

// UnstablePredeclaredModules returns a Starlark string dictionary with
// predeclared Skycfg modules which can be used in starlark.ExecFile.
//
// Takes in unstableProtoRegistry as param (if nil will use standard proto
// registry).
//
// Currently provides these modules (see REAMDE for more detailed description):
//  * fail   - interrupts execution and prints a stacktrace.
//  * hash   - supports md5, sha1 and sha245 functions.
//  * json   - marshals plain values (dicts, lists, etc) to JSON.
//  * proto  - package for constructing Protobuf messages.
//  * struct - experimental Starlark struct support.
//  * yaml   - same as "json" package but for YAML.
//  * url    - utility package for encoding URL query string.
func UnstablePredeclaredModules(r unstableProtoRegistry) starlark.StringDict {
	modules, protoModule := predeclaredModules()
	protoModule.Registry = r
	return modules
}

// predeclaredModules is a helper that returns new predeclared modules.
// Returns proto module separately for (optional) extra initialization.
func predeclaredModules() (modules starlark.StringDict, proto *impl.ProtoModule) {
	proto = impl.NewProtoModule(nil /* TODO: registry from options */)
	modules = starlark.StringDict{
		"fail":   impl.Fail,
		"hash":   impl.HashModule(),
		"json":   impl.JsonModule(),
		"proto":  proto,
		"struct": starlark.NewBuiltin("struct", starlarkstruct.Make),
		"yaml":   impl.YamlModule(),
		"url":    impl.UrlModule(),
	}
	return
}

// Load reads a Skycfg config file from the filesystem.
func Load(ctx context.Context, filename string, opts ...LoadOption) (*Config, error) {
	modules, protoModule := predeclaredModules()
	parsedOpts := &loadOptions{
		globals:    modules,
		fileReader: LocalFileReader(filepath.Dir(filename)),
	}

	for _, opt := range opts {
		opt.applyLoad(parsedOpts)
	}
	protoModule.Registry = parsedOpts.protoRegistry
	configLocals, tests, err := loadImpl(ctx, parsedOpts, filename)
	if err != nil {
		return nil, err
	}
	return &Config{
		filename: filename,
		globals:  parsedOpts.globals,
		locals:   configLocals,
		tests:    tests,
	}, nil
}

func loadImpl(ctx context.Context, opts *loadOptions, filename string) (starlark.StringDict, []*Test, error) {
	reader := opts.fileReader

	type cacheEntry struct {
		globals starlark.StringDict
		err     error
	}
	cache := make(map[string]*cacheEntry)
	tests := []*Test{}

	var load func(thread *starlark.Thread, moduleName string) (starlark.StringDict, error)
	load = func(thread *starlark.Thread, moduleName string) (starlark.StringDict, error) {
		var fromPath string
		if thread.CallStackDepth() > 0 {
			fromPath = thread.CallFrame(0).Pos.Filename()
		}
		modulePath, err := reader.Resolve(ctx, moduleName, fromPath)
		if err != nil {
			return nil, err
		}

		e, ok := cache[modulePath]
		if e != nil {
			return e.globals, e.err
		}
		if ok {
			return nil, fmt.Errorf("cycle in load graph")
		}
		moduleSource, err := reader.ReadFile(ctx, modulePath)
		if err != nil {
			cache[modulePath] = &cacheEntry{nil, err}
			return nil, err
		}

		cache[modulePath] = nil
		globals, err := starlark.ExecFile(thread, modulePath, moduleSource, opts.globals)
		cache[modulePath] = &cacheEntry{globals, err}

		for name, val := range globals {
			if !strings.HasPrefix(name, "test_") {
				continue
			}
			if fn, ok := val.(starlark.Callable); ok {
				tests = append(tests, &Test{
					callable: fn,
				})
			}
		}
		return globals, err
	}
	locals, err := load(&starlark.Thread{
		Print: skyPrint,
		Load:  load,
	}, filename)
	return locals, tests, err
}

// Filename returns the original filename passed to Load().
func (c *Config) Filename() string {
	return c.filename
}

// Globals returns the set of variables in the Starlark global namespace,
// including any added to the config loader by WithGlobals().
func (c *Config) Globals() starlark.StringDict {
	return c.globals
}

// Locals returns the set of variables in the Starlark local namespace for
// the top-level module.
func (c *Config) Locals() starlark.StringDict {
	return c.locals
}

// An ExecOption adjusts details of how a Skycfg config's main function is
// executed.
type ExecOption interface {
	applyExec(*execOptions)
}

type execOptions struct {
	vars *starlark.Dict
}

type fnExecOption func(*execOptions)

func (fn fnExecOption) applyExec(opts *execOptions) { fn(opts) }

// WithVars adds key:value pairs to the ctx.vars dict passed to main().
func WithVars(vars starlark.StringDict) ExecOption {
	return fnExecOption(func(opts *execOptions) {
		for key, value := range vars {
			opts.vars.SetKey(starlark.String(key), value)
		}
	})
}

// Main executes main() from the top-level Skycfg config module, which is
// expected to return either None or a list of Protobuf messages.
func (c *Config) Main(ctx context.Context, opts ...ExecOption) ([]proto.Message, error) {
	parsedOpts := &execOptions{
		vars: &starlark.Dict{},
	}
	for _, opt := range opts {
		opt.applyExec(parsedOpts)
	}
	mainVal, ok := c.locals["main"]
	if !ok {
		return nil, fmt.Errorf("no `main' function found in %q", c.filename)
	}
	main, ok := mainVal.(starlark.Callable)
	if !ok {
		return nil, fmt.Errorf("`main' must be a function (got a %s)", mainVal.Type())
	}

	thread := &starlark.Thread{
		Print: skyPrint,
	}
	thread.SetLocal("context", ctx)
	mainCtx := &impl.Module{
		Name: "skycfg_ctx",
		Attrs: starlark.StringDict(map[string]starlark.Value{
			"vars": parsedOpts.vars,
		}),
	}
	args := starlark.Tuple([]starlark.Value{mainCtx})
	mainVal, err := starlark.Call(thread, main, args, nil)
	if err != nil {
		return nil, err
	}
	mainList, ok := mainVal.(*starlark.List)
	if !ok {
		if _, isNone := mainVal.(starlark.NoneType); isNone {
			return nil, nil
		}
		return nil, fmt.Errorf("`main' didn't return a list (got a %s)", mainVal.Type())
	}
	var msgs []proto.Message
	for ii := 0; ii < mainList.Len(); ii++ {
		maybeMsg := mainList.Index(ii)
		msg, ok := AsProtoMessage(maybeMsg)
		if !ok {
			return nil, fmt.Errorf("`main' returned something that's not a protobuf (a %s)", maybeMsg.Type())
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}

// A TestResult is the result of a test run
type TestResult struct {
	TestName string
	Failure  error
	Duration time.Duration
}

// A Test is a test case, which is a skycfg function whose name starts with `test_`.
type Test struct {
	callable starlark.Callable
}

// Name returns the name of the test (the name of the function)
func (t *Test) Name() string {
	return t.callable.Name()
}

// Run actually executes a test. It returns a TestResult if the test completes (even if it fails)
// The error return value will only be non-nil if the test execution itself errors.
func (t *Test) Run(ctx context.Context) (*TestResult, error) {
	thread := &starlark.Thread{
		Print: skyPrint,
	}
	thread.SetLocal("context", ctx)

	assertModule := impl.AssertModule()
	testCtx := &impl.Module{
		Name: "skycfg_test_ctx",
		Attrs: starlark.StringDict(map[string]starlark.Value{
			"vars":   &starlark.Dict{},
			"assert": assertModule,
		}),
	}
	args := starlark.Tuple([]starlark.Value{testCtx})

	result := TestResult{
		TestName: t.Name(),
	}

	startTime := time.Now()
	_, err := starlark.Call(thread, t.callable, args, nil)
	result.Duration = time.Since(startTime)
	if err != nil {
		// if there is no assertion error, there was something wrong with the execution itself
		if len(assertModule.Failures) == 0 {
			return nil, err
		}

		// there should only be one failure, because each test run gets its own *TestContext
		// and each assertion failure halts execution.
		if len(assertModule.Failures) > 1 {
			panic("A test run should only have one assertion failure. Something went wrong with the test infrastructure.")
		}
		result.Failure = assertModule.Failures[0]
	}

	return &result, nil
}

// Tests returns all tests defined in the config
func (c *Config) Tests() []*Test {
	return c.tests
}

func skyPrint(t *starlark.Thread, msg string) {
	fmt.Fprintf(os.Stderr, "[%v] %s\n", t.CallFrame(1).Pos, msg)
}
