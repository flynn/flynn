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
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	dpb "github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/wrappers"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// A Starlark built-in type representing a Protobuf message. Provides attributes
// for accessing message fields using their original protobuf names.
type skyProtoMessage struct {
	msg    proto.Message
	val    reflect.Value
	fields []*proto.Properties
	oneofs map[string]*proto.OneofProperties
	frozen bool

	// lets the message wrapper keep track of per-field wrappers, for freezing.
	attrCache map[string]starlark.Value
}

var _ starlark.HasAttrs = (*skyProtoMessage)(nil)
var _ starlark.HasSetField = (*skyProtoMessage)(nil)
var _ starlark.Comparable = (*skyProtoMessage)(nil)

func (msg *skyProtoMessage) String() string {
	return fmt.Sprintf("<%s %s>", msg.Type(), proto.CompactTextString(msg.msg))
}
func (msg *skyProtoMessage) Type() string         { return messageTypeName(msg.msg) }
func (msg *skyProtoMessage) Truth() starlark.Bool { return starlark.True }

func (msg *skyProtoMessage) Freeze() {
	if !msg.frozen {
		msg.frozen = true
		for _, attr := range msg.attrCache {
			attr.Freeze()
		}
	}
}

func (msg *skyProtoMessage) CompareSameType(op syntax.Token, y starlark.Value, depth int) (bool, error) {
	other, ok := y.(*skyProtoMessage)
	if !ok {
		return false, nil
	}

	switch op {
	case syntax.EQL:
		eql := proto.Equal(msg.msg, other.msg)
		return eql, nil
	case syntax.NEQ:
		eql := proto.Equal(msg.msg, other.msg)
		return !eql, nil
	default:
		return false, fmt.Errorf("Only == and != operations are supported on protobufs, found %s", op.String())
	}
}

func (msg *skyProtoMessage) Hash() (uint32, error) {
	return 0, fmt.Errorf("skyProtoMessage.Hash: TODO")
}

func (msg *skyProtoMessage) MarshalJSON() ([]byte, error) {
	if msg.looksLikeKubernetesGogo() {
		return json.Marshal(msg.msg)
	}

	var jsonMarshaler = &jsonpb.Marshaler{OrigName: true}
	jsonData, err := jsonMarshaler.MarshalToString(msg.msg)
	if err != nil {
		return nil, err
	}
	return []byte(jsonData), nil
}

func (msg *skyProtoMessage) looksLikeKubernetesGogo() bool {
	path := msg.val.Type().PkgPath()
	return strings.HasPrefix(path, "k8s.io/api/") || strings.HasPrefix(path, "k8s.io/apimachinery/")
}

func NewSkyProtoMessage(msg proto.Message) *skyProtoMessage {
	wrapper := &skyProtoMessage{
		msg:       msg,
		val:       reflect.ValueOf(msg).Elem(),
		oneofs:    make(map[string]*proto.OneofProperties),
		attrCache: make(map[string]starlark.Value),
	}

	protoProps := protoGetProperties(wrapper.val.Type())
	for _, prop := range protoProps.Prop {
		if prop.Tag == 0 {
			// Skip attributes that don't correspond to a protobuf field.
			continue
		}
		wrapper.fields = append(wrapper.fields, prop)
	}
	for fieldName, prop := range protoProps.OneofTypes {
		wrapper.fields = append(wrapper.fields, prop.Prop)
		wrapper.oneofs[fieldName] = prop
	}
	return wrapper
}

func ToProtoMessage(val starlark.Value) (proto.Message, bool) {
	if msg, ok := val.(*skyProtoMessage); ok {
		return msg.msg, true
	}
	return nil, false
}

func (msg *skyProtoMessage) checkMutable(verb string) error {
	if msg.frozen {
		return fmt.Errorf("cannot %s frozen message", verb)
	}
	return nil
}

func (msg *skyProtoMessage) resetAttrCache() {
	msg.attrCache = make(map[string]starlark.Value)
}

func (msg *skyProtoMessage) Attr(name string) (starlark.Value, error) {
	if attr, ok := msg.attrCache[name]; ok {
		return attr, nil
	}
	for _, field := range msg.fields {
		if field.OrigName != name {
			continue
		}
		var out starlark.Value
		if oneofProp, isOneof := msg.oneofs[name]; isOneof {
			out = msg.getOneofField(name, oneofProp)
		} else {
			out = valueToStarlark(msg.val.FieldByName(field.Name))
		}
		if msg.frozen {
			out.Freeze()
		}
		msg.attrCache[name] = out
		return out, nil
	}
	return nil, nil
}

func (msg *skyProtoMessage) getOneofField(name string, prop *proto.OneofProperties) starlark.Value {
	ifaceField := msg.val.Field(prop.Field)
	if ifaceField.IsNil() {
		return starlark.None
	}
	if ifaceField.Elem().Type() == prop.Type {
		return valueToStarlark(ifaceField.Elem().Elem().Field(0))
	}
	return starlark.None
}

func (msg *skyProtoMessage) AttrNames() []string {
	var names []string
	for _, field := range msg.fields {
		names = append(names, field.OrigName)
	}
	sort.Strings(names)
	return names
}

func (msg *skyProtoMessage) SetField(name string, sky starlark.Value) error {
	var prop *proto.Properties
	for _, fieldProp := range msg.fields {
		if name != fieldProp.OrigName {
			continue
		}
		prop = fieldProp
		break
	}
	if prop == nil {
		return fmt.Errorf("AttributeError: `%s' value has no field %q", msg.Type(), name)
	}
	if oneofProp, isOneof := msg.oneofs[name]; isOneof {
		return msg.setOneofField(name, oneofProp, sky)
	}
	return msg.setSingleField(name, prop, sky)
}

func (msg *skyProtoMessage) setOneofField(name string, prop *proto.OneofProperties, sky starlark.Value) error {
	// Oneofs are stored in a two-part format, where `msg.val` has a field of an intermediate interface
	// type that can be constructed from the property type.
	ifaceField := msg.val.Field(prop.Field)

	field, ok := prop.Type.Elem().FieldByName(prop.Prop.Name)
	if !ok {
		return fmt.Errorf("InternalError: field %q not found in generated type %v", name, prop.Type)
	}
	val, err := valueFromStarlark(field.Type, sky)
	if err != nil {
		return err
	}
	if err := msg.checkMutable("set field of"); err != nil {
		return err
	}

	// Construct the intermediate per-field struct.
	box := reflect.New(prop.Type.Elem())
	box.Elem().Field(0).Set(val)

	delete(msg.attrCache, name)
	ifaceField.Set(box)
	return nil
}

func (msg *skyProtoMessage) setSingleField(name string, prop *proto.Properties, sky starlark.Value) error {
	field, ok := msg.val.Type().FieldByName(prop.Name)
	if !ok {
		return fmt.Errorf("InternalError: field %q not found in generated type %v", prop.OrigName, msg.val.Type())
	}

	val, err := valueFromStarlark(field.Type, sky)
	if err != nil {
		return err
	}
	if err := msg.checkMutable("set field of"); err != nil {
		return err
	}
	delete(msg.attrCache, name)
	msg.val.FieldByName(prop.Name).Set(val)
	return nil
}

func valueToStarlark(val reflect.Value) starlark.Value {
	if scalar := scalarToStarlark(val); scalar != nil {
		return scalar
	}
	iface := val.Interface()
	if msg, ok := iface.(proto.Message); ok {
		return NewSkyProtoMessage(msg)
	}
	t := val.Type()
	if t.Kind() == reflect.Struct {
		// Might have been generated by gogo-protobuf
		//
		// Need to check if this is a non-pointer map value, which
		// cannot be addressed and therefore can never become a
		// `proto.Message`.
		if val.CanAddr() {
			if msg, ok := val.Addr().Interface().(proto.Message); ok {
				return NewSkyProtoMessage(msg)
			}
		}
	}
	// Handle []byte ([]uint8) -> string special case.
	if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
		return starlark.String(string(val.Interface().([]byte)))
	}
	if t.Kind() == reflect.Slice {
		var items []starlark.Value
		for ii := 0; ii < val.Len(); ii++ {
			items = append(items, valueToStarlark(val.Index(ii)))
		}
		return &protoRepeated{
			field: val,
			list:  starlark.NewList(items),
		}
	}
	if t.Kind() == reflect.Map {
		dict := &starlark.Dict{}
		for _, keyVal := range val.MapKeys() {
			elemVal := val.MapIndex(keyVal)
			key := valueToStarlark(keyVal)
			elem := valueToStarlark(elemVal)
			if err := dict.SetKey(key, elem); err != nil {
				panic(fmt.Sprintf("dict.SetKey(%s, %s): %v", key, elem, err))
			}
		}
		return &protoMap{
			field: val,
			dict:  dict,
		}
	}
	// This should be impossible, because the set of types present
	// in a generated protobuf struct is small and limited.
	panic(fmt.Errorf("valueToStarlark: unknown type %v", val.Type()))
}

func scalarToStarlark(val reflect.Value) starlark.Value {
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return starlark.None
		}
		val = val.Elem()
	}
	iface := val.Interface()
	switch f := iface.(type) {
	case int32:
		return starlark.MakeInt64(int64(f))
	case int64:
		return starlark.MakeInt64(f)
	case uint32:
		return starlark.MakeUint64(uint64(f))
	case uint64:
		return starlark.MakeUint64(f)
	case float32:
		return starlark.Float(f)
	case float64:
		return starlark.Float(f)
	case string:
		return starlark.String(f)
	case bool:
		return starlark.Bool(f)
	case time.Duration:
		return NewSkyProtoMessage(ptypes.DurationProto(f))
	}
	if enum, ok := iface.(protoEnum); ok {
		return &skyProtoEnumValue{
			typeName:  enumTypeName(enum),
			valueName: enum.String(),
			value:     val.Convert(reflect.TypeOf(int32(0))).Interface().(int32),
		}
	}
	return nil
}

// maybeConvertString checks if type is not assignable and is string (string
// alias) and attempts to convert it.
// Returns false if can't convert, true if successfully converted or conversion
// was not required.
func maybeConvertString(v reflect.Value, t reflect.Type) (reflect.Value, bool) {
	if vt := v.Type(); !vt.AssignableTo(t) {
		// Only attempt to convert string aliases.
		if vt.Kind() != reflect.String || !vt.ConvertibleTo(t) {
			return reflect.Value{}, false
		}
		return v.Convert(t), true
	}
	return v, true
}

// maybeConvertToWrapper checks if [sky] is a primitive and [t] is a corresponding
// protobuf wrapper type and attempts to convert it.
// Returns a non-nil Value and nil error if successfully converted, a non-nil error
// if unsuccessful conversion, and nil Value and nil error if no conversion attempt
// was required.
func maybeConvertToWrapper(t reflect.Type, sky starlark.Value) (*reflect.Value, error) {
	BoolValueType := reflect.TypeOf(&wrappers.BoolValue{})
	StringValueType := reflect.TypeOf(&wrappers.StringValue{})
	DoubleValueType := reflect.TypeOf(&wrappers.DoubleValue{})
	Int32ValueType := reflect.TypeOf(&wrappers.Int32Value{})
	Int64ValueType := reflect.TypeOf(&wrappers.Int64Value{})
	BytesValueType := reflect.TypeOf(&wrappers.BytesValue{})
	UInt32ValueType := reflect.TypeOf(&wrappers.UInt32Value{})
	UInt64ValueType := reflect.TypeOf(&wrappers.UInt64Value{})

	switch t {
	case UInt32ValueType:
		switch sky := sky.(type) {
		case starlark.Int:
			uint64Val, ok := sky.Uint64()
			if ok && uint64Val <= math.MaxUint32 {
				val := reflect.ValueOf(&wrappers.UInt32Value{Value: uint32(uint64Val)})
				return &val, nil
			}
			return nil, fmt.Errorf("ValueError: value %v is not exactly representable as type `uint32'.", sky)
		}
	case UInt64ValueType:
		switch sky := sky.(type) {
		case starlark.Int:
			uint64Val, ok := sky.Uint64()
			if ok {
				val := reflect.ValueOf(&wrappers.UInt64Value{Value: uint64Val})
				return &val, nil
			}
			return nil, fmt.Errorf("ValueError: value %v is not exactly representable as type `uint64'.", sky)
		}
	case BoolValueType:
		switch sky := sky.(type) {
		case starlark.Bool:
			val := reflect.ValueOf(&wrappers.BoolValue{Value: bool(sky)})
			return &val, nil
		}
	case BytesValueType:
		if stringVal, ok := starlark.AsString(sky); ok {
			val := reflect.ValueOf(&wrappers.BytesValue{Value: []byte(stringVal)})
			return &val, nil
		}
	case StringValueType:
		if stringVal, ok := starlark.AsString(sky); ok {
			val := reflect.ValueOf(&wrappers.StringValue{Value: stringVal})
			return &val, nil
		}
	case DoubleValueType:
		if float64Val, ok := starlark.AsFloat(sky); ok {
			val := reflect.ValueOf(&wrappers.DoubleValue{Value: float64Val})
			return &val, nil
		}
	case Int32ValueType:
		switch sky := sky.(type) {
		case starlark.Int:
			int32Val, err := starlark.AsInt32(sky)
			if err == nil {
				val := reflect.ValueOf(&wrappers.Int32Value{Value: int32(int32Val)})
				return &val, nil
			}
			return nil, fmt.Errorf("ValueError: value %v overflows type `int32'.", sky)
		}
	case Int64ValueType:
		switch sky := sky.(type) {
		case starlark.Int:
			int64Val, ok := sky.Int64()
			if ok {
				val := reflect.ValueOf(&wrappers.Int64Value{Value: int64Val})
				return &val, nil
			}
			return nil, fmt.Errorf("ValueError: value %v is not exactly representable as type `int64'.", sky)
		}
	case UInt32ValueType:
		switch sky := sky.(type) {
		case starlark.Int:
			uint64Val, ok := sky.Uint64()
			if ok && uint64Val <= math.MaxUint32 {
				val := reflect.ValueOf(&wrappers.UInt32Value{Value: uint32(uint64Val)})
				return &val, nil
			}
			return nil, fmt.Errorf("ValueError: value %v is not exactly representable as type `uint32'.", sky)
		}
	case UInt64ValueType:
		switch sky := sky.(type) {
		case starlark.Int:
			uint64Val, ok := sky.Uint64()
			if ok {
				val := reflect.ValueOf(&wrappers.UInt64Value{Value: uint64Val})
				return &val, nil
			}
			return nil, fmt.Errorf("ValueError: value %v is not exactly representable as type `uint64'.", sky)
		}
	}
	return nil, nil
}

func valueFromStarlark(t reflect.Type, sky starlark.Value) (reflect.Value, error) {
	switch sky := sky.(type) {
	case starlark.Int, starlark.Float, starlark.String, starlark.Bool:
		value, err := maybeConvertToWrapper(t, sky)
		if err != nil {
			return reflect.Value{}, err
		} else if value != nil {
			return *value, err
		}

		scalar, err := scalarFromStarlark(t, sky)
		if err != nil {
			return reflect.Value{}, err
		}

		// Handle the use of typedefs in Kubernetes and "string" ->
		// "bytes" conversion.
		scalar, ok := maybeConvertString(scalar, t)
		if !ok {
			return reflect.Value{}, typeError(t, sky)
		}
		return scalar, nil
	case starlark.NoneType:
		if t.Kind() == reflect.Ptr {
			return reflect.Zero(t), nil
		}
		// Give a better error message for true type mismatch, instead of
		// "pointer or non-pointer" caused by Go's different representation
		// of proto3 messages.
		if t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
			return reflect.Value{}, typeError(t, sky)
		}
		return reflect.Value{}, fmt.Errorf("TypeError: value None can't be assigned to type `%s' in proto3 mode.", t)
	case *skyProtoEnumValue:
		return enumFromStarlark(t, sky)
	case *skyProtoMessage:
		if reflect.TypeOf(sky.msg) == t {
			return reflect.ValueOf(sky.msg), nil
		}
		if reflect.TypeOf(sky.msg) == reflect.PtrTo(t) {
			return reflect.ValueOf(sky.msg).Elem(), nil
		}

		dpb, ok := sky.msg.(*dpb.Duration)
		if ok && t == reflect.TypeOf(time.Duration(0)) {
			d, err := ptypes.Duration(dpb)
			if err != nil {
				return reflect.Value{}, fmt.Errorf("ValueError: %v (type `%s') can't be coverted to `time.Duration': %v", dpb, reflect.TypeOf(dpb), err)
			}

			return reflect.ValueOf(d), nil
		}
	case *protoRepeated:
		return valueFromStarlark(t, sky.list)
	case *starlark.List:
		if t.Kind() == reflect.Slice {
			elemType := t.Elem()
			val := reflect.MakeSlice(t, sky.Len(), sky.Len())
			for ii := 0; ii < sky.Len(); ii++ {
				elem, err := valueFromStarlark(elemType, sky.Index(ii))
				if err != nil {
					return reflect.Value{}, err
				}
				val.Index(ii).Set(elem)
			}
			return val, nil
		}
	case *protoMap:
		return valueFromStarlark(t, sky.dict)
	case *starlark.Dict:
		if t.Kind() == reflect.Map {
			keyType := t.Key()
			elemType := t.Elem()
			val := reflect.MakeMapWithSize(t, sky.Len())
			for _, item := range sky.Items() {
				key, err := valueFromStarlark(keyType, item[0])
				if err != nil {
					return reflect.Value{}, err
				}
				elem, err := valueFromStarlark(elemType, item[1])
				if err != nil {
					return reflect.Value{}, err
				}
				val.SetMapIndex(key, elem)
			}
			return val, nil
		}
	}
	return reflect.Value{}, typeError(t, sky)
}

func scalarFromStarlark(t reflect.Type, sky starlark.Value) (reflect.Value, error) {
	k := t.Kind()
	// Handling special case of Starlark string to []byte (aka []uint8 aka
	// proto "bytes") assigment.
	if k == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
		val, ok := sky.(starlark.String)
		if !ok {
			return reflect.Value{}, typeError(t, sky)
		}
		return reflect.ValueOf([]byte(val)), nil
	}

	switch k {
	case reflect.Ptr:
		val := reflect.New(t.Elem())
		elem, err := scalarFromStarlark(t.Elem(), sky)
		if err != nil {
			// Recompute the type error based on the pointer type.
			return reflect.Value{}, typeError(t, sky)
		}

		// In case target is aliased ptr string.
		elem, ok := maybeConvertString(elem, val.Elem().Type())
		if !ok {
			return reflect.Value{}, typeError(t, sky)
		}

		val.Elem().Set(elem)
		return val, nil
	case reflect.Bool:
		if val, ok := sky.(starlark.Bool); ok {
			return reflect.ValueOf(bool(val)), nil
		}
	case reflect.String:
		if val, ok := sky.(starlark.String); ok {
			return reflect.ValueOf(string(val)), nil
		}
	case reflect.Float64:
		if val, ok := starlark.AsFloat(sky); ok {
			return reflect.ValueOf(val), nil
		}
	case reflect.Float32:
		if val, ok := starlark.AsFloat(sky); ok {
			return reflect.ValueOf(float32(val)), nil
		}
	case reflect.Int64:
		if skyInt, ok := sky.(starlark.Int); ok {
			if val, ok := skyInt.Int64(); ok {
				return reflect.ValueOf(val), nil
			}
			return reflect.Value{}, fmt.Errorf("ValueError: value %v overflows type `int64'.", skyInt)
		}
	case reflect.Uint64:
		if skyInt, ok := sky.(starlark.Int); ok {
			if val, ok := skyInt.Uint64(); ok {
				return reflect.ValueOf(val), nil
			}
			return reflect.Value{}, fmt.Errorf("ValueError: value %v overflows type `uint64'.", skyInt)
		}
	case reflect.Int32:
		if skyInt, ok := sky.(starlark.Int); ok {
			if val, ok := skyInt.Int64(); ok && val >= math.MinInt32 && val <= math.MaxInt32 {
				return reflect.ValueOf(int32(val)), nil
			}
			return reflect.Value{}, fmt.Errorf("ValueError: value %v overflows type `int32'.", skyInt)
		}
	case reflect.Uint32:
		if skyInt, ok := sky.(starlark.Int); ok {
			if val, ok := skyInt.Uint64(); ok && val <= math.MaxUint32 {
				return reflect.ValueOf(uint32(val)), nil
			}
			return reflect.Value{}, fmt.Errorf("ValueError: value %v overflows type `uint32'.", skyInt)
		}
	}
	return reflect.Value{}, typeError(t, sky)
}

func enumFromStarlark(t reflect.Type, sky *skyProtoEnumValue) (reflect.Value, error) {
	if t.Kind() == reflect.Ptr {
		val := reflect.New(t.Elem())
		elem, err := enumFromStarlark(t.Elem(), sky)
		if err != nil {
			return reflect.Value{}, err
		}
		val.Elem().Set(elem)
		return val, nil
	}
	if t.Kind() == reflect.Int32 {
		if enum, ok := reflect.Zero(t).Interface().(protoEnum); ok {
			if enumTypeName(enum) == sky.typeName {
				return reflect.ValueOf(sky.value).Convert(t), nil
			}
		}
	}
	return reflect.Value{}, typeError(t, sky)
}

func typeName(t reflect.Type) string {
	// Special-case protobuf types to get more useful error messages when
	// the wrong protobuf type is assigned.
	messageType := reflect.TypeOf((*proto.Message)(nil)).Elem()
	if t.Implements(messageType) {
		return messageTypeName(reflect.Zero(t).Interface().(proto.Message))
	}
	enumType := reflect.TypeOf((*protoEnum)(nil)).Elem()
	if t.Implements(enumType) {
		return enumTypeName(reflect.Zero(t).Interface().(protoEnum))
	}
	if t.PkgPath() == "" {
		return t.String()
	}
	return fmt.Sprintf("%q.%s", t.PkgPath(), t.Name())
}

func typeError(t reflect.Type, sky starlark.Value) error {
	if sky.Type() != typeName(t) {
		return fmt.Errorf("TypeError: value %s (type `%s') can't be assigned to type `%s'.",
			sky.String(), sky.Type(), typeName(t))
	}
	return fmt.Errorf("TypeError: value %s (type `%s') can't be assigned to type `%s'.\n"+
		"(see https://github.com/stripe/skycfg/#typeerror-with-the-same-type-names)",
		sky.String(), sky.Type(), typeName(t))
}

type protoRepeated struct {
	// var x []T; reflect.ValueOf(x)
	field reflect.Value
	list  *starlark.List
}

var _ starlark.Value = (*protoRepeated)(nil)
var _ starlark.Iterable = (*protoRepeated)(nil)
var _ starlark.Sequence = (*protoRepeated)(nil)
var _ starlark.Indexable = (*protoRepeated)(nil)
var _ starlark.HasAttrs = (*protoRepeated)(nil)
var _ starlark.HasSetIndex = (*protoRepeated)(nil)
var _ starlark.HasBinary = (*protoRepeated)(nil)
var _ starlark.Comparable = (*protoRepeated)(nil)

func (r *protoRepeated) Attr(name string) (starlark.Value, error) {
	wrapper, ok := listMethods[name]
	if !ok {
		return nil, nil
	}
	if wrapper != nil {
		return wrapper(r), nil
	}
	return r.list.Attr(name)
}

func (r *protoRepeated) AttrNames() []string                 { return r.list.AttrNames() }
func (r *protoRepeated) Freeze()                             { r.list.Freeze() }
func (r *protoRepeated) Hash() (uint32, error)               { return r.list.Hash() }
func (r *protoRepeated) Index(i int) starlark.Value          { return r.list.Index(i) }
func (r *protoRepeated) Iterate() starlark.Iterator          { return r.list.Iterate() }
func (r *protoRepeated) Len() int                            { return r.list.Len() }
func (r *protoRepeated) Slice(x, y, step int) starlark.Value { return r.list.Slice(x, y, step) }
func (r *protoRepeated) String() string                      { return r.list.String() }
func (r *protoRepeated) Truth() starlark.Bool                { return r.list.Truth() }

func (r *protoRepeated) Type() string {
	return fmt.Sprintf("list<%s>", typeName(r.field.Type().Elem()))
}

func (r *protoRepeated) CompareSameType(op syntax.Token, y starlark.Value, depth int) (bool, error) {
	other, ok := y.(*protoRepeated)
	if !ok {
		return false, nil
	}

	return starlark.CompareDepth(op, r.list, other.list, depth)
}

func (r *protoRepeated) wrapClear() starlark.Value {
	impl := func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackPositionalArgs("clear", args, kwargs, 0); err != nil {
			return nil, err
		}
		if err := r.Clear(); err != nil {
			return nil, err
		}
		return starlark.None, nil
	}
	return starlark.NewBuiltin("clear", impl).BindReceiver(r)
}

func (r *protoRepeated) wrapAppend() starlark.Value {
	impl := func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var val starlark.Value
		if err := starlark.UnpackPositionalArgs("append", args, kwargs, 1, &val); err != nil {
			return nil, err
		}
		if err := r.Append(val); err != nil {
			return nil, err
		}
		return starlark.None, nil
	}
	return starlark.NewBuiltin("append", impl).BindReceiver(r)
}

func (r *protoRepeated) wrapExtend() starlark.Value {
	impl := func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var val starlark.Iterable
		if err := starlark.UnpackPositionalArgs("extend", args, kwargs, 1, &val); err != nil {
			return nil, err
		}
		if err := r.implExtend(thread, val); err != nil {
			return nil, err
		}
		return starlark.None, nil
	}
	return starlark.NewBuiltin("extend", impl).BindReceiver(r)
}

var listMethods = map[string]func(*protoRepeated) starlark.Value{
	"clear":  (*protoRepeated).wrapClear,
	"append": (*protoRepeated).wrapAppend,
	"extend": (*protoRepeated).wrapExtend,
	// "insert": (*protoRepeated).wrapInsert,
	// "pop":    (*protoRepeated).wrapPop,
	// "remove": (*protoRepeated).wrapRemove,
}

func (r *protoRepeated) Clear() error {
	if err := r.list.Clear(); err != nil {
		return err
	}
	r.field.SetLen(0)
	return nil
}

func (r *protoRepeated) Append(v starlark.Value) error {
	itemType := r.field.Type().Elem()
	if v == starlark.None {
		return typeError(itemType, v)
	}
	goVal, err := valueFromStarlark(itemType, v)
	if err != nil {
		return err
	}
	if err := r.list.Append(v); err != nil {
		return err
	}
	r.field.Set(reflect.Append(r.field, goVal))
	return nil
}

func (r *protoRepeated) implExtend(t *starlark.Thread, iterable starlark.Iterable) error {
	itemType := r.field.Type().Elem()
	var skyValues []starlark.Value
	var goValues []reflect.Value
	iter := iterable.Iterate()
	defer iter.Done()
	var skyVal starlark.Value
	for iter.Next(&skyVal) {
		if skyVal == starlark.None {
			return typeError(itemType, skyVal)
		}
		goVal, err := valueFromStarlark(itemType, skyVal)
		if err != nil {
			return err
		}
		skyValues = append(skyValues, skyVal)
		goValues = append(goValues, goVal)
	}

	listExtend, _ := r.list.Attr("extend")
	args := starlark.Tuple([]starlark.Value{
		starlark.NewList(skyValues),
	})
	if _, err := starlark.Call(t, listExtend, args, nil); err != nil {
		return err
	}
	r.field.Set(reflect.Append(r.field, goValues...))
	return nil
}

func (r *protoRepeated) SetIndex(i int, v starlark.Value) error {
	itemType := r.field.Type().Elem()
	if v == starlark.None {
		return typeError(itemType, v)
	}
	goVal, err := valueFromStarlark(itemType, v)
	if err != nil {
		return err
	}
	if err := r.list.SetIndex(i, v); err != nil {
		return err
	}
	r.field.Index(i).Set(goVal)
	return nil
}

func (r *protoRepeated) Binary(op syntax.Token, y starlark.Value, side starlark.Side) (starlark.Value, error) {
	if op == syntax.PLUS {
		if side == starlark.Left {
			switch y := y.(type) {
			case *starlark.List:
				return starlark.Binary(op, r.list, y)
			case *protoRepeated:
				return starlark.Binary(op, r.list, y.list)
			}
			return nil, nil
		}
		if side == starlark.Right {
			if _, ok := y.(*starlark.List); ok {
				return starlark.Binary(op, y, r.list)
			}
			return nil, nil
		}
	}
	return nil, nil
}

type protoMap struct {
	field reflect.Value
	dict  *starlark.Dict
}

var _ starlark.Value = (*protoMap)(nil)
var _ starlark.Iterable = (*protoMap)(nil)
var _ starlark.Sequence = (*protoMap)(nil)
var _ starlark.HasAttrs = (*protoMap)(nil)
var _ starlark.HasSetKey = (*protoMap)(nil)
var _ starlark.Comparable = (*protoMap)(nil)

func (m *protoMap) Attr(name string) (starlark.Value, error) {
	wrapper, ok := dictMethods[name]
	if !ok {
		return nil, nil
	}
	if wrapper != nil {
		return wrapper(m), nil
	}
	return m.dict.Attr(name)
}

func (m *protoMap) AttrNames() []string                                { return m.dict.AttrNames() }
func (m *protoMap) Freeze()                                            { m.dict.Freeze() }
func (m *protoMap) Hash() (uint32, error)                              { return m.dict.Hash() }
func (m *protoMap) Get(k starlark.Value) (starlark.Value, bool, error) { return m.dict.Get(k) }
func (m *protoMap) Iterate() starlark.Iterator                         { return m.dict.Iterate() }
func (m *protoMap) Len() int                                           { return m.dict.Len() }
func (m *protoMap) String() string                                     { return m.dict.String() }
func (m *protoMap) Truth() starlark.Bool                               { return m.dict.Truth() }

func (m *protoMap) Type() string {
	t := m.field.Type()
	return fmt.Sprintf("map<%s, %s>", typeName(t.Key()), typeName(t.Elem()))
}

func (m *protoMap) CompareSameType(op syntax.Token, y starlark.Value, depth int) (bool, error) {
	other, ok := y.(*protoMap)
	if !ok {
		return false, nil
	}

	return starlark.CompareDepth(op, m.dict, other.dict, depth)
}

func (m *protoMap) wrapClear() starlark.Value {
	impl := func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackPositionalArgs("clear", args, kwargs, 0); err != nil {
			return nil, err
		}
		if err := m.dict.Clear(); err != nil {
			return nil, err
		}
		m.field.Set(reflect.MakeMap(m.field.Type()))
		return starlark.None, nil
	}
	return starlark.NewBuiltin("clear", impl).BindReceiver(m)
}

func (m *protoMap) wrapSetDefault() starlark.Value {
	impl := func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var key, defaultValue starlark.Value = nil, starlark.None
		if err := starlark.UnpackPositionalArgs("setdefault", args, kwargs, 1, &key, &defaultValue); err != nil {
			return nil, err
		}
		if val, ok, err := m.dict.Get(key); err != nil {
			return nil, err
		} else if ok {
			return val, nil
		}
		return defaultValue, m.SetKey(key, defaultValue)
	}
	return starlark.NewBuiltin("setdefault", impl).BindReceiver(m)
}

func (m *protoMap) wrapUpdate() starlark.Value {
	keyType := m.field.Type().Key()
	itemType := m.field.Type().Elem()
	impl := func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		// Use the underlying starlark `dict.update()` to get a Dict containing
		// all the new values, so we don't have to recreate the API here. After
		// the temp dict is constructed, type check.
		tempDict := &starlark.Dict{}
		tempUpdate, _ := tempDict.Attr("update")
		if _, err := starlark.Call(thread, tempUpdate, args, kwargs); err != nil {
			return nil, err
		}
		for _, item := range tempDict.Items() {
			if item[0] == starlark.None {
				return nil, typeError(keyType, item[0])
			}
			if item[1] == starlark.None {
				return nil, typeError(itemType, item[1])
			}
		}
		tempMap, err := valueFromStarlark(m.field.Type(), tempDict)
		if err != nil {
			return nil, err
		}

		// tempMap is a reflected Go map containing items of the correct type.
		// Update the Dict first to catch potential immutability.
		for _, item := range tempDict.Items() {
			if err := m.dict.SetKey(item[0], item[1]); err != nil {
				return nil, err
			}
		}

		if m.field.IsNil() {
			m.field.Set(reflect.MakeMap(m.field.Type()))
		}
		for _, key := range tempMap.MapKeys() {
			m.field.SetMapIndex(key, tempMap.MapIndex(key))
		}
		return starlark.None, nil
	}
	return starlark.NewBuiltin("update", impl).BindReceiver(m)
}

func (m *protoMap) SetKey(k, v starlark.Value) error {
	keyType := m.field.Type().Key()
	itemType := m.field.Type().Elem()
	if k == starlark.None {
		return typeError(keyType, k)
	}
	if v == starlark.None {
		return typeError(itemType, v)
	}
	goKey, err := valueFromStarlark(keyType, k)
	if err != nil {
		return err
	}
	goVal, err := valueFromStarlark(itemType, v)
	if err != nil {
		return err
	}
	if err := m.dict.SetKey(k, v); err != nil {
		return err
	}
	if m.field.IsNil() {
		m.field.Set(reflect.MakeMap(m.field.Type()))
	}
	m.field.SetMapIndex(goKey, goVal)
	return nil
}

var dictMethods = map[string]func(*protoMap) starlark.Value{
	"clear": (*protoMap).wrapClear,
	"get":   nil,
	"items": nil,
	"keys":  nil,
	// "pop":        (*protoMap).wrapPop,
	// "popitem":    (*protoMap).wrapPopItem,
	"setdefault": (*protoMap).wrapSetDefault,
	"update":     (*protoMap).wrapUpdate,
	"values":     nil,
}
