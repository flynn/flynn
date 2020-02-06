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
	"reflect"

	"github.com/golang/protobuf/proto"
	"go.starlark.net/starlark"
)

type defaultProtoRegistry struct{}

func (*defaultProtoRegistry) UnstableProtoMessageType(name string) (reflect.Type, error) {
	return proto.MessageType(name), nil
}

func (*defaultProtoRegistry) UnstableEnumValueMap(name string) map[string]int32 {
	return proto.EnumValueMap(name)
}

// NewProtoPackage creates a Starlark value representing a named Protobuf package.
//
// Protobuf packagess are conceptually similar to a C++ namespace or Ruby
// module, in that they're aggregated from multiple .proto source files.
func newProtoPackage(registry ProtoRegistry, name string) starlark.Value {
	return &skyProtoPackage{
		registry: registry,
		name:     name,
	}
}

type skyProtoPackage struct {
	registry ProtoRegistry
	name     string
}

func (pkg *skyProtoPackage) String() string       { return fmt.Sprintf("<proto.Package %q>", pkg.name) }
func (pkg *skyProtoPackage) Type() string         { return "proto.Package" }
func (pkg *skyProtoPackage) Freeze()              {}
func (pkg *skyProtoPackage) Truth() starlark.Bool { return starlark.True }
func (pkg *skyProtoPackage) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: %s", pkg.Type())
}

func (pkg *skyProtoPackage) AttrNames() []string {
	// TODO: Implement when go-protobuf gains support for listing the
	// registered message types in a Protobuf package.
	//
	// https://github.com/golang/protobuf/issues/623
	return nil
}

func (pkg *skyProtoPackage) Attr(attrName string) (starlark.Value, error) {
	fullName := fmt.Sprintf("%s.%s", pkg.name, attrName)
	registry := pkg.registry
	if registry == nil {
		registry = &defaultProtoRegistry{}
	}
	if ev := registry.UnstableEnumValueMap(fullName); ev != nil {
		return &skyProtoEnumType{
			name:     fullName,
			valueMap: ev,
		}, nil
	}
	return newMessageType(registry, "", fullName)
}
