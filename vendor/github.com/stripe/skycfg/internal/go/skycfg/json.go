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

package skycfg

import (
	"bytes"

	"go.starlark.net/starlark"
)

// JsonModule returns a Starlark module for JSON helpers.
func JsonModule() starlark.Value {
	return &Module{
		Name: "json",
		Attrs: starlark.StringDict{
			"marshal": jsonMarshal(),
		},
	}
}

// jsonMarshal returns a Starlark function for marshaling plain values
// (dicts, lists, etc) to JSON.
//
//  def json.marshal(value) -> str
func jsonMarshal() starlark.Callable {
	return starlark.NewBuiltin("json.marshal", fnJsonMarshal)
}

func fnJsonMarshal(t *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var v starlark.Value
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs, "value", &v); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := writeJSON(&buf, v); err != nil {
		return nil, err
	}
	return starlark.String(buf.String()), nil
}
