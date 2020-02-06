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
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"go.starlark.net/starlark"
	yaml "gopkg.in/yaml.v2"
)

// UNSTABLE extension point for configuring how protobuf messages are loaded.
//
// This will be stabilized after the go-protobuf v2 API has reached GA.
type ProtoRegistry interface {
	// UNSTABLE lookup from full protobuf message name to a Go type of the
	// generated message struct.
	UnstableProtoMessageType(name string) (reflect.Type, error)

	// UNSTABLE lookup from go-protobuf enum name to the name->value map.
	UnstableEnumValueMap(name string) map[string]int32
}

func NewProtoModule(registry ProtoRegistry) *ProtoModule {
	mod := &ProtoModule{
		Registry: registry,
		attrs: starlark.StringDict{
			"clear":        starlark.NewBuiltin("proto.clear", fnProtoClear),
			"clone":        starlark.NewBuiltin("proto.clone", fnProtoClone),
			"from_json":    starlark.NewBuiltin("proto.from_json", fnProtoFromJson),
			"from_text":    starlark.NewBuiltin("proto.from_text", fnProtoFromText),
			"from_yaml":    starlark.NewBuiltin("proto.from_yaml", fnProtoFromYaml),
			"merge":        starlark.NewBuiltin("proto.merge", fnProtoMerge),
			"set_defaults": starlark.NewBuiltin("proto.set_defaults", fnProtoSetDefaults),
			"to_json":      starlark.NewBuiltin("proto.to_json", fnProtoToJson),
			"to_text":      starlark.NewBuiltin("proto.to_text", fnProtoToText),
			"to_yaml":      starlark.NewBuiltin("proto.to_yaml", fnProtoToYaml),
		},
	}
	mod.attrs["package"] = starlark.NewBuiltin("proto.package", mod.fnProtoPackage)
	return mod
}

type ProtoModule struct {
	Registry ProtoRegistry
	attrs    starlark.StringDict
}

var _ starlark.HasAttrs = (*ProtoModule)(nil)

func (mod *ProtoModule) String() string       { return fmt.Sprintf("<module %q>", "proto") }
func (mod *ProtoModule) Type() string         { return "module" }
func (mod *ProtoModule) Freeze()              { mod.attrs.Freeze() }
func (mod *ProtoModule) Truth() starlark.Bool { return starlark.True }
func (mod *ProtoModule) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: %s", mod.Type())
}

func (mod *ProtoModule) Attr(name string) (starlark.Value, error) {
	if val, ok := mod.attrs[name]; ok {
		return val, nil
	}
	return nil, nil
}

func (mod *ProtoModule) AttrNames() []string {
	var names []string
	for name := range mod.attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Implementation of the `proto.clear()` built-in function.
// Reset protobuf state to the default values.
func fnProtoClear(t *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var msg *skyProtoMessage
	if err := wantSingleProtoMessage("proto.clear", args, kwargs, &msg); err != nil {
		return nil, err
	}
	if err := msg.checkMutable("clear"); err != nil {
		return nil, err
	}
	msg.msg.Reset()
	msg.resetAttrCache()
	return msg, nil
}

// Implementation of the `proto.clone()` built-in function.
// Creates a deep copy of a protobuf.
func fnProtoClone(t *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var msg *skyProtoMessage
	if err := wantSingleProtoMessage("proto.clone", args, kwargs, &msg); err != nil {
		return nil, err
	}
	return NewSkyProtoMessage(proto.Clone(msg.msg)), nil
}

// Implementation of the `proto.merge()` built-in function.
// Merge merges src into dst. Repeated fields will be appended.
func fnProtoMerge(t *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var val1, val2 starlark.Value
	if err := starlark.UnpackPositionalArgs("proto.merge", args, kwargs, 2, &val1, &val2); err != nil {
		return nil, err
	}
	dst, ok := val1.(*skyProtoMessage)
	if !ok {
		return nil, fmt.Errorf("%s: for parameter 1: got %s, want proto.Message", "proto.merge", val1.Type())
	}
	src, ok := val2.(*skyProtoMessage)
	if !ok {
		return nil, fmt.Errorf("%s: for parameter 2: got %s, want proto.Message", "proto.merge", val2.Type())
	}
	if src.Type() != dst.Type() {
		return nil, fmt.Errorf("%s: types are not the same: got %s and %s", "proto.merge", src.Type(), dst.Type())
	}
	if err := dst.checkMutable("merge into"); err != nil {
		return nil, err
	}
	proto.Merge(dst.msg, src.msg)
	dst.resetAttrCache()
	return dst, nil
}

// Implementation of the `proto.set_default()` built-in function.
// Sets unset protobuf fields to their default values.
func fnProtoSetDefaults(t *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var msg *skyProtoMessage
	if err := wantSingleProtoMessage("proto.set_defaults", args, kwargs, &msg); err != nil {
		return nil, err
	}
	if err := msg.checkMutable("set field defaults of"); err != nil {
		return nil, err
	}
	proto.SetDefaults(msg.msg)
	return msg, nil
}

// Implementation of the `proto.package()` built-in function.
//
// Note: doesn't do any sort of input validation, because the go-protobuf
// message registration data isn't currently exported in a useful way
// (see https://github.com/golang/protobuf/issues/623).
func (mod *ProtoModule) fnProtoPackage(t *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var packageName string
	if err := starlark.UnpackPositionalArgs("proto.package", args, kwargs, 1, &packageName); err != nil {
		return nil, err
	}
	return &skyProtoPackage{
		registry: mod.Registry,
		name:     packageName,
	}, nil
}

func wantSingleProtoMessage(fnName string, args starlark.Tuple, kwargs []starlark.Tuple, msg **skyProtoMessage) error {
	var val starlark.Value
	if err := starlark.UnpackPositionalArgs(fnName, args, kwargs, 1, &val); err != nil {
		return err
	}
	gotMsg, ok := val.(*skyProtoMessage)
	if !ok {
		return fmt.Errorf("%s: for parameter 1: got %s, want proto.Message", fnName, val.Type())
	}
	*msg = gotMsg
	return nil
}

// Implementation of the `proto.to_text()` built-in function. Returns the
// text-formatted content of a protobuf message.
func fnProtoToText(t *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var msg *skyProtoMessage
	if err := wantSingleProtoMessage("proto.to_text", args, []starlark.Tuple{}, &msg); err != nil {
		return nil, err
	}
	var textMarshaler = &proto.TextMarshaler{Compact: true}
	if len(kwargs) > 0 {
		compact := true
		if err := starlark.UnpackArgs("proto.to_text", nil, kwargs, "compact", &compact); err != nil {
			return nil, err
		}
		if compact {
			textMarshaler.Compact = true
		} else {
			textMarshaler.Compact = false
		}
	}
	text := (textMarshaler).Text(msg.msg)
	return starlark.String(text), nil
}

// Implementation of the `proto.to_json()` built-in function. Returns the
// JSON-formatted content of a protobuf message.
func fnProtoToJson(t *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var msg *skyProtoMessage
	if err := wantSingleProtoMessage("proto.to_json", args, []starlark.Tuple{}, &msg); err != nil {
		return nil, err
	}
	compact := true
	if len(kwargs) > 0 {
		if err := starlark.UnpackArgs("proto.to_json", nil, kwargs, "compact", &compact); err != nil {
			return nil, err
		}
	}
	jsonData, err := msg.MarshalJSON()
	if err != nil {
		return nil, err
	}
	if !compact {
		var buf bytes.Buffer
		if err := json.Indent(&buf, jsonData, "", "\t"); err != nil {
			return nil, err
		}
		jsonData = buf.Bytes()
	}
	return starlark.String(jsonData), nil
}

// Implementation of the `proto.to_yaml()` built-in function. Returns the
// YAML-formatted content of a protobuf message.
func fnProtoToYaml(t *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var msg *skyProtoMessage
	if err := wantSingleProtoMessage("proto.to_yaml", args, kwargs, &msg); err != nil {
		return nil, err
	}
	jsonData, err := (&jsonpb.Marshaler{OrigName: true}).MarshalToString(msg.msg)
	if err != nil {
		return nil, err
	}
	var yamlMap yaml.MapSlice
	if err := yaml.Unmarshal([]byte(jsonData), &yamlMap); err != nil {
		return nil, err
	}
	yamlData, err := yaml.Marshal(yamlMap)
	if err != nil {
		return nil, err
	}
	return starlark.String(yamlData), nil
}

// Implementation of the `proto.from_text()` built-in function.
// Returns the Protobuf message for text-formatted content.
func fnProtoFromText(t *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var msgType starlark.Value
	var value starlark.String
	if err := starlark.UnpackPositionalArgs("proto.from_text", args, kwargs, 2, &msgType, &value); err != nil {
		return nil, err
	}
	protoMsgType, ok := msgType.(*skyProtoMessageType)
	if !ok {
		return nil, fmt.Errorf("%s: for parameter 2: got %s, want proto.MessageType", "proto.from_text", msgType.Type())
	}
	msg := proto.Clone(protoMsgType.emptyMsg)
	msg.Reset()
	if err := proto.UnmarshalText(string(value), msg); err != nil {
		return nil, err
	}
	return NewSkyProtoMessage(msg), nil
}

// Implementation of the `proto.from_json()` built-in function.
// Returns the Protobuf message for JSON-formatted content.
func fnProtoFromJson(t *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var msgType starlark.Value
	var value starlark.String
	if err := starlark.UnpackPositionalArgs("proto.from_json", args, kwargs, 2, &msgType, &value); err != nil {
		return nil, err
	}
	protoMsgType, ok := msgType.(*skyProtoMessageType)
	if !ok {
		return nil, fmt.Errorf("%s: for parameter 2: got %s, want proto.MessageType", "proto.from_json", msgType.Type())
	}
	msg := proto.Clone(protoMsgType.emptyMsg)
	msg.Reset()
	if err := jsonpb.UnmarshalString(string(value), msg); err != nil {
		return nil, err
	}
	return NewSkyProtoMessage(msg), nil
}

// Implementation of the `proto.from_yaml()` built-in function.
// Returns the Protobuf message for YAML-formatted content.
func fnProtoFromYaml(t *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var msgType starlark.Value
	var value starlark.String
	if err := starlark.UnpackPositionalArgs("proto.from_yaml", args, kwargs, 2, &msgType, &value); err != nil {
		return nil, err
	}
	protoMsgType, ok := msgType.(*skyProtoMessageType)
	if !ok {
		return nil, fmt.Errorf("%s: for parameter 2: got %s, want proto.MessageType", "proto.from_yaml", msgType.Type())
	}
	var msgBody interface{}
	if err := yaml.Unmarshal([]byte(value), &msgBody); err != nil {
		return nil, err
	}
	msgBody, err := convertMapStringInterface("proto.from_yaml", msgBody)
	if err != nil {
		return nil, err
	}
	jsonData, err := json.Marshal(msgBody)
	if err != nil {
		return nil, err
	}
	msg := proto.Clone(protoMsgType.emptyMsg)
	msg.Reset()
	if err := jsonpb.UnmarshalString(string(jsonData), msg); err != nil {
		return nil, err
	}
	return NewSkyProtoMessage(msg), nil
}

// Coverts map[interface{}]interface{} into map[string]interface{} for json.Marshaler
func convertMapStringInterface(fnName string, val interface{}) (interface{}, error) {
	switch items := val.(type) {
	case map[interface{}]interface{}:
		result := map[string]interface{}{}
		for k, v := range items {
			key, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("%s: TypeError: value %s (type `%s') can't be assigned to type 'string'.", fnName, k, reflect.TypeOf(k))
			}
			value, err := convertMapStringInterface(fnName, v)
			if err != nil {
				return nil, err
			}
			result[key] = value
		}
		return result, nil
	case []interface{}:
		for k, v := range items {
			value, err := convertMapStringInterface(fnName, v)
			if err != nil {
				return nil, err
			}
			items[k] = value
		}
	}
	return val, nil
}
