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
	"compress/gzip"
	"fmt"
	"io/ioutil"
	"reflect"
	"strings"

	"github.com/golang/protobuf/descriptor"
	"github.com/golang/protobuf/proto"
	descriptor_pb "github.com/golang/protobuf/protoc-gen-go/descriptor"
)

func mustParseFileDescriptor(gzBytes []byte) *descriptor_pb.FileDescriptorProto {
	gz, err := gzip.NewReader(bytes.NewReader(gzBytes))
	if err != nil {
		panic(fmt.Sprintf("EnumDescriptor: %v", err))
	}
	defer gz.Close()

	fileDescBytes, err := ioutil.ReadAll(gz)
	if err != nil {
		panic(fmt.Sprintf("EnumDescriptor: %v", err))
	}

	fileDesc := &descriptor_pb.FileDescriptorProto{}
	if err := proto.Unmarshal(fileDescBytes, fileDesc); err != nil {
		panic(fmt.Sprintf("EnumDescriptor: %v", err))
	}
	return fileDesc
}

func messageTypeName(msg proto.Message) string {
	if hasName, ok := msg.(interface {
		XXX_MessageName() string
	}); ok {
		return hasName.XXX_MessageName()
	}

	hasDesc, ok := msg.(descriptor.Message)
	if !ok {
		return proto.MessageName(msg)
	}

	gzBytes, path := hasDesc.Descriptor()
	fileDesc := mustParseFileDescriptor(gzBytes)
	var chunks []string
	if pkg := fileDesc.GetPackage(); pkg != "" {
		chunks = append(chunks, pkg)
	}

	msgDesc := fileDesc.MessageType[path[0]]
	for ii := 1; ii < len(path); ii++ {
		chunks = append(chunks, msgDesc.GetName())
		msgDesc = msgDesc.NestedType[path[ii]]
	}
	chunks = append(chunks, msgDesc.GetName())
	return strings.Join(chunks, ".")
}

// Wrapper around proto.GetProperties with a reflection-based fallback
// around oneof parsing for GoGo.
func protoGetProperties(t reflect.Type) *proto.StructProperties {
	got := proto.GetProperties(t)

	// If OneofTypes was already populated, then the go-protobuf
	// properties parser was fully successful and we don't need to do
	// anything more.
	if len(got.OneofTypes) > 0 {
		return got
	}

	// If the oneofs map is empty, it might be because the message
	// contains no oneof fields. We also don't need to do anything.
	expectOneofs := false
	for ii := 0; ii < t.NumField(); ii++ {
		f := t.Field(ii)
		if f.Tag.Get("protobuf_oneof") != "" {
			expectOneofs = true
			break
		}
	}
	if !expectOneofs {
		return got
	}

	// proto.GetProperties will ignore oneofs for GoGo generated code,
	// even though the tags and structures are identical. This is a
	// side-effect of XXX_OneofFuncs() containing nominal interface types
	// in its signature, and can be worked around with reflection.
	msg := reflect.New(t)
	oneofFuncsFn := msg.MethodByName("XXX_OneofFuncs")
	if !oneofFuncsFn.IsValid() {
		return got
	}

	// proto.GetProperties returns a mutable pointer to global internal
	// state of the protobuf library. Avoid spooky behavior by doing a
	// shallow copy.
	got = &proto.StructProperties{
		Prop:       got.Prop,
		OneofTypes: make(map[string]*proto.OneofProperties),
	}

	// This will panic if the API of XXX_OneofFuncs() changes significantly.
	// Hopefully that won't happen before the go-protobuf v2 API makes this
	// workaround unnecessary.
	oneofFuncs := oneofFuncsFn.Call([]reflect.Value{})
	oneofTypes := oneofFuncs[len(oneofFuncs)-1].Interface().([]interface{})
	for _, oneofType := range oneofTypes {
		prop := &proto.OneofProperties{
			Type: reflect.ValueOf(oneofType).Type(),
			Prop: &proto.Properties{},
		}
		realField := prop.Type.Elem().Field(0)
		prop.Prop.Name = realField.Name
		prop.Prop.Parse(realField.Tag.Get("protobuf"))
		for ii := 0; ii < t.NumField(); ii++ {
			f := t.Field(ii)
			if prop.Type.AssignableTo(f.Type) {
				prop.Field = ii
				break
			}
		}
		got.OneofTypes[prop.Prop.OrigName] = prop
	}

	return got
}
