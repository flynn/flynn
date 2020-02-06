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
	"fmt"
	"sort"

	"go.starlark.net/starlark"
)

// A Starlark module, for namespacing of built-in functions.
type Module struct {
	Name  string
	Attrs starlark.StringDict
}

var _ starlark.HasAttrs = (*Module)(nil)

func (mod *Module) String() string        { return fmt.Sprintf("<module %q>", mod.Name) }
func (mod *Module) Type() string          { return "module" }
func (mod *Module) Freeze()               { mod.Attrs.Freeze() }
func (mod *Module) Truth() starlark.Bool  { return starlark.True }
func (mod *Module) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable type: %s", mod.Type()) }

func (mod *Module) Attr(name string) (starlark.Value, error) {
	if val, ok := mod.Attrs[name]; ok {
		return val, nil
	}
	return nil, nil
}

func (mod *Module) AttrNames() []string {
	var names []string
	for name := range mod.Attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
