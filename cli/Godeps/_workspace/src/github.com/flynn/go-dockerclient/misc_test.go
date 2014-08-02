// Copyright 2013 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"net/http"
	"net/url"
	"reflect"
	"testing"
)

func TestVersion(t *testing.T) {
	body := `{
     "Version":"0.2.2",
     "GitCommit":"5a2a5cc+CHANGES",
     "GoVersion":"go1.0.3"
}`
	fakeRT := &FakeRoundTripper{message: body, status: http.StatusOK}
	client := newTestClient(fakeRT)
	expected := APIVersion{
		Version:   "0.2.2",
		GitCommit: "5a2a5cc+CHANGES",
		GoVersion: "go1.0.3",
	}
	version, err := client.Version()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*version, expected) {
		t.Errorf("Version(): Wrong result. Want %#v. Got %#v.", expected, version)
	}
	req := fakeRT.requests[0]
	if req.Method != "GET" {
		t.Errorf("Version(): wrong request method. Want GET. Got %s.", req.Method)
	}
	u, _ := url.Parse(client.getURL("/version"))
	if req.URL.Path != u.Path {
		t.Errorf("Version(): wrong request path. Want %q. Got %q.", u.Path, req.URL.Path)
	}
}

func TestVersionError(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "internal error", status: http.StatusInternalServerError}
	client := newTestClient(fakeRT)
	version, err := client.Version()
	if version != nil {
		t.Errorf("Version(): expected <nil> value, got %#v.", version)
	}
	if err == nil {
		t.Error("Version(): unexpected <nil> error")
	}
}

func TestInfo(t *testing.T) {
	body := `{
     "Containers":11,
     "Images":16,
     "Debug":false,
     "NFd": 11,
     "NGoroutines":21,
     "MemoryLimit":true,
     "SwapLimit":false
}`
	fakeRT := &FakeRoundTripper{message: body, status: http.StatusOK}
	client := newTestClient(fakeRT)
	expected := APIInfo{
		Containers:  11,
		Images:      16,
		Debug:       false,
		NFd:         11,
		NGoroutines: 21,
		MemoryLimit: true,
		SwapLimit:   false,
	}
	info, err := client.Info()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*info, expected) {
		t.Errorf("Info(): Wrong result. Want %#v. Got %#v.", expected, info)
	}
	req := fakeRT.requests[0]
	if req.Method != "GET" {
		t.Errorf("Info(): Wrong HTTP method. Want GET. Got %s.", req.Method)
	}
	u, _ := url.Parse(client.getURL("/info"))
	if req.URL.Path != u.Path {
		t.Errorf("Info(): Wrong request path. Want %q. Got %q.", u.Path, req.URL.Path)
	}
}

func TestInfoError(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "internal error", status: http.StatusInternalServerError}
	client := newTestClient(fakeRT)
	version, err := client.Info()
	if version != nil {
		t.Errorf("Info(): expected <nil> value, got %#v.", version)
	}
	if err == nil {
		t.Error("Info(): unexpected <nil> error")
	}
}
