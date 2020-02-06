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
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"hash"

	"go.starlark.net/starlark"
)

func HashModule() starlark.Value {
	return &Module{
		Name: "hash",
		Attrs: starlark.StringDict{
			"md5":    starlark.NewBuiltin("hash.md5", fnHash(md5.New)),
			"sha1":   starlark.NewBuiltin("hash.sha1", fnHash(sha1.New)),
			"sha256": starlark.NewBuiltin("hash.sha256", fnHash(sha256.New)),
		},
	}
}

func fnHash(hash func() hash.Hash) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(t *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var s starlark.String
		if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &s); err != nil {
			return nil, err
		}

		h := hash()
		h.Write([]byte(string(s)))
		return starlark.String(fmt.Sprintf("%x", h.Sum(nil))), nil
	}
}
