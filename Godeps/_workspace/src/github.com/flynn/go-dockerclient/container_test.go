// Copyright 2013 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"testing"
)

func TestListContainers(t *testing.T) {
	jsonContainers := `[
     {
             "Id": "8dfafdbc3a40",
             "Image": "base:latest",
             "Command": "echo 1",
             "Created": 1367854155,
             "Ports":[{"PrivatePort": 2222, "PublicPort": 3333, "Type": "tcp"}],
             "Status": "Exit 0"
     },
     {
             "Id": "9cd87474be90",
             "Image": "base:latest",
             "Command": "echo 222222",
             "Created": 1367854155,
             "Ports":[{"PrivatePort": 2222, "PublicPort": 3333, "Type": "tcp"}],
             "Status": "Exit 0"
     },
     {
             "Id": "3176a2479c92",
             "Image": "base:latest",
             "Command": "echo 3333333333333333",
             "Created": 1367854154,
             "Ports":[{"PrivatePort": 2221, "PublicPort": 3331, "Type": "tcp"}],
             "Status": "Exit 0"
     },
     {
             "Id": "4cb07b47f9fb",
             "Image": "base:latest",
             "Command": "echo 444444444444444444444444444444444",
             "Ports":[{"PrivatePort": 2223, "PublicPort": 3332, "Type": "tcp"}],
             "Created": 1367854152,
             "Status": "Exit 0"
     }
]`
	var expected []APIContainers
	err := json.Unmarshal([]byte(jsonContainers), &expected)
	if err != nil {
		t.Fatal(err)
	}
	client := newTestClient(&FakeRoundTripper{message: jsonContainers, status: http.StatusOK})
	containers, err := client.ListContainers(ListContainersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(containers, expected) {
		t.Errorf("ListContainers: Expected %#v. Got %#v.", expected, containers)
	}
}

func TestListContainersParams(t *testing.T) {
	var tests = []struct {
		input  ListContainersOptions
		params map[string][]string
	}{
		{ListContainersOptions{}, map[string][]string{}},
		{ListContainersOptions{All: true}, map[string][]string{"all": {"1"}}},
		{ListContainersOptions{All: true, Limit: 10}, map[string][]string{"all": {"1"}, "limit": {"10"}}},
		{
			ListContainersOptions{All: true, Limit: 10, Since: "adf9983", Before: "abdeef"},
			map[string][]string{"all": {"1"}, "limit": {"10"}, "since": {"adf9983"}, "before": {"abdeef"}},
		},
	}
	fakeRT := &FakeRoundTripper{message: "[]", status: http.StatusOK}
	client := newTestClient(fakeRT)
	u, _ := url.Parse(client.getURL("/containers/json"))
	for _, tt := range tests {
		client.ListContainers(tt.input)
		got := map[string][]string(fakeRT.requests[0].URL.Query())
		if !reflect.DeepEqual(got, tt.params) {
			t.Errorf("Expected %#v, got %#v.", tt.params, got)
		}
		if path := fakeRT.requests[0].URL.Path; path != u.Path {
			t.Errorf("Wrong path on request. Want %q. Got %q.", u.Path, path)
		}
		if meth := fakeRT.requests[0].Method; meth != "GET" {
			t.Errorf("Wrong HTTP method. Want GET. Got %s.", meth)
		}
		fakeRT.Reset()
	}
}

func TestListContainersFailure(t *testing.T) {
	var tests = []struct {
		status  int
		message string
	}{
		{400, "bad parameter"},
		{500, "internal server error"},
	}
	for _, tt := range tests {
		client := newTestClient(&FakeRoundTripper{message: tt.message, status: tt.status})
		expected := Error{Status: tt.status, Message: tt.message}
		containers, err := client.ListContainers(ListContainersOptions{})
		if !reflect.DeepEqual(expected, *err.(*Error)) {
			t.Errorf("Wrong error in ListContainers. Want %#v. Got %#v.", expected, err)
		}
		if len(containers) > 0 {
			t.Errorf("ListContainers failure. Expected empty list. Got %#v.", containers)
		}
	}
}

func TestInspectContainer(t *testing.T) {
	jsonContainer := `{
             "Id": "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2",
             "Created": "2013-05-07T14:51:42.087658+02:00",
             "Path": "date",
             "Args": [],
             "Config": {
                     "Hostname": "4fa6e0f0c678",
                     "User": "",
                     "Memory": 0,
                     "MemorySwap": 0,
                     "AttachStdin": false,
                     "AttachStdout": true,
                     "AttachStderr": true,
                     "PortSpecs": null,
                     "Tty": false,
                     "OpenStdin": false,
                     "StdinOnce": false,
                     "Env": null,
                     "Cmd": [
                             "date"
                     ],
                     "Dns": null,
                     "Image": "base",
                     "Volumes": {},
                     "VolumesFrom": ""
             },
             "State": {
                     "Running": false,
                     "Pid": 0,
                     "ExitCode": 0,
                     "StartedAt": "2013-05-07T14:51:42.087658+02:00",
                     "Ghost": false
             },
             "Image": "b750fe79269d2ec9a3c593ef05b4332b1d1a02a62b4accb2c21d589ff2f5f2dc",
             "NetworkSettings": {
                     "IpAddress": "",
                     "IpPrefixLen": 0,
                     "Gateway": "",
                     "Bridge": "",
                     "PortMapping": null
             },
             "SysInitPath": "/home/kitty/go/src/github.com/dotcloud/docker/bin/docker",
             "ResolvConfPath": "/etc/resolv.conf",
             "Volumes": {}
}`
	var expected Container
	err := json.Unmarshal([]byte(jsonContainer), &expected)
	if err != nil {
		t.Fatal(err)
	}
	fakeRT := &FakeRoundTripper{message: jsonContainer, status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c678"
	container, err := client.InspectContainer(id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*container, expected) {
		t.Errorf("InspectContainer(%q): Expected %#v. Got %#v.", id, expected, container)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/4fa6e0f0c678/json"))
	if gotPath := fakeRT.requests[0].URL.Path; gotPath != expectedURL.Path {
		t.Errorf("InspectContainer(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
}

func TestInspectContainerFailure(t *testing.T) {
	client := newTestClient(&FakeRoundTripper{message: "server error", status: 500})
	expected := Error{Status: 500, Message: "server error"}
	container, err := client.InspectContainer("abe033")
	if container != nil {
		t.Errorf("InspectContainer: Expected <nil> container, got %#v", container)
	}
	if !reflect.DeepEqual(expected, *err.(*Error)) {
		t.Errorf("InspectContainer: Wrong error information. Want %#v. Got %#v.", expected, err)
	}
}

func TestInspectContainerNotFound(t *testing.T) {
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: 404})
	container, err := client.InspectContainer("abe033")
	if container != nil {
		t.Errorf("InspectContainer: Expected <nil> container, got %#v", container)
	}
	expected := &NoSuchContainer{ID: "abe033"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("InspectContainer: Wrong error information. Want %#v. Got %#v.", expected, err)
	}
}

func TestCreateContainer(t *testing.T) {
	jsonContainer := `{
             "Id": "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2",
	     "Warnings": []
}`
	var expected Container
	err := json.Unmarshal([]byte(jsonContainer), &expected)
	if err != nil {
		t.Fatal(err)
	}
	fakeRT := &FakeRoundTripper{message: jsonContainer, status: http.StatusOK}
	client := newTestClient(fakeRT)
	config := Config{AttachStdout: true, AttachStdin: true}
	container, err := client.CreateContainer(&config)
	if err != nil {
		t.Fatal(err)
	}
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	if container.ID != id {
		t.Errorf("CreateContainer: wrong ID. Want %q. Got %q.", id, container.ID)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("CreateContainer: wrong HTTP method. Want %q. Got %q.", "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/create"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("CreateContainer: Wrong path in request. Want %q. Got %q.", expectedURL.Path, gotPath)
	}
	var gotBody Config
	err = json.NewDecoder(req.Body).Decode(&gotBody)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateContainerImageNotFound(t *testing.T) {
	client := newTestClient(&FakeRoundTripper{message: "No such image", status: http.StatusNotFound})
	config := Config{AttachStdout: true, AttachStdin: true}
	container, err := client.CreateContainer(&config)
	if container != nil {
		t.Errorf("CreateContainer: expected <nil> container, got %#v.", container)
	}
	if !reflect.DeepEqual(err, ErrNoSuchImage) {
		t.Errorf("CreateContainer: Wrong error type. Want %#v. Got %#v.", ErrNoSuchImage, err)
	}
}

func TestStartContainer(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	err := client.StartContainer(id, &HostConfig{})
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("StartContainer(%q): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/start"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("StartContainer(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
	expectedContentType := "application/json"
	if contentType := req.Header.Get("Content-Type"); contentType != expectedContentType {
		t.Errorf("StartContainer(%q): Wrong content-type in request. Want %q. Got %q.", id, expectedContentType, contentType)
	}
}

func TestStartContainerNotFound(t *testing.T) {
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	err := client.StartContainer("a2344", &HostConfig{})
	expected := &NoSuchContainer{ID: "a2344"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("StartContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestStopContainer(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusNoContent}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	err := client.StopContainer(id, 10)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("StopContainer(%q, 10): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/stop"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("StopContainer(%q, 10): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
}

func TestStopContainerNotFound(t *testing.T) {
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	err := client.StopContainer("a2334", 10)
	expected := &NoSuchContainer{ID: "a2334"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("StopContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestRestartContainer(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusNoContent}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	err := client.RestartContainer(id, 10)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("RestartContainer(%q, 10): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/restart"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("RestartContainer(%q, 10): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
}

func TestRestartContainerNotFound(t *testing.T) {
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	err := client.RestartContainer("a2334", 10)
	expected := &NoSuchContainer{ID: "a2334"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("RestartContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestKillContainer(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusNoContent}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	err := client.KillContainer(id)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("KillContainer(%q): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/kill"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("KillContainer(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
}

func TestKillContainerNotFound(t *testing.T) {
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	err := client.KillContainer("a2334")
	expected := &NoSuchContainer{ID: "a2334"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("KillContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestRemoveContainer(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	err := client.RemoveContainer(id)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "DELETE" {
		t.Errorf("RemoveContainer(%q): wrong HTTP method. Want %q. Got %q.", id, "DELETE", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("RemoveContainer(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
}

func TestRemoveContainerNotFound(t *testing.T) {
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	err := client.RemoveContainer("a2334")
	expected := &NoSuchContainer{ID: "a2334"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("RemoveContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestWaitContainer(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: `{"StatusCode": 56}`, status: http.StatusOK}
	client := newTestClient(fakeRT)
	id := "4fa6e0f0c6786287e131c3852c58a2e01cc697a68231826813597e4994f1d6e2"
	status, err := client.WaitContainer(id)
	if err != nil {
		t.Fatal(err)
	}
	if status != 56 {
		t.Errorf("WaitContainer(%q): wrong return. Want 56. Got %d.", id, status)
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("WaitContainer(%q): wrong HTTP method. Want %q. Got %q.", id, "POST", req.Method)
	}
	expectedURL, _ := url.Parse(client.getURL("/containers/" + id + "/wait"))
	if gotPath := req.URL.Path; gotPath != expectedURL.Path {
		t.Errorf("WaitContainer(%q): Wrong path in request. Want %q. Got %q.", id, expectedURL.Path, gotPath)
	}
}

func TestWaitContainerNotFound(t *testing.T) {
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	_, err := client.WaitContainer("a2334")
	expected := &NoSuchContainer{ID: "a2334"}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("WaitContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestCommitContainer(t *testing.T) {
	response := `{"Id":"596069db4bf5"}`
	client := newTestClient(&FakeRoundTripper{message: response, status: http.StatusOK})
	id := "596069db4bf5"
	image, err := client.CommitContainer(CommitContainerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if image.ID != id {
		t.Errorf("CommitContainer: Wrong image id. Want %q. Got %q.", id, image.ID)
	}
}

func TestCommitContainerParams(t *testing.T) {
	cfg := Config{Memory: 67108864}
	b, _ := json.Marshal(&cfg)
	var tests = []struct {
		input  CommitContainerOptions
		params map[string][]string
	}{
		{CommitContainerOptions{}, map[string][]string{}},
		{CommitContainerOptions{Container: "44c004db4b17"}, map[string][]string{"container": {"44c004db4b17"}}},
		{
			CommitContainerOptions{Container: "44c004db4b17", Repository: "tsuru/python", Message: "something"},
			map[string][]string{"container": {"44c004db4b17"}, "repo": {"tsuru/python"}, "m": {"something"}},
		},
		{
			CommitContainerOptions{Container: "44c004db4b17", Run: &cfg},
			map[string][]string{"container": {"44c004db4b17"}, "run": {string(b)}},
		},
	}
	fakeRT := &FakeRoundTripper{message: "[]", status: http.StatusOK}
	client := newTestClient(fakeRT)
	u, _ := url.Parse(client.getURL("/commit"))
	for _, tt := range tests {
		client.CommitContainer(tt.input)
		got := map[string][]string(fakeRT.requests[0].URL.Query())
		if !reflect.DeepEqual(got, tt.params) {
			t.Errorf("Expected %#v, got %#v.", tt.params, got)
		}
		if path := fakeRT.requests[0].URL.Path; path != u.Path {
			t.Errorf("Wrong path on request. Want %q. Got %q.", u.Path, path)
		}
		if meth := fakeRT.requests[0].Method; meth != "POST" {
			t.Errorf("Wrong HTTP method. Want POST. Got %s.", meth)
		}
		fakeRT.Reset()
	}
}

func TestCommitContainerFailure(t *testing.T) {
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusInternalServerError})
	_, err := client.CommitContainer(CommitContainerOptions{})
	if err == nil {
		t.Error("Expected non-nil error, got <nil>.")
	}
}

func TestCommitContainerNotFound(t *testing.T) {
	client := newTestClient(&FakeRoundTripper{message: "no such container", status: http.StatusNotFound})
	_, err := client.CommitContainer(CommitContainerOptions{})
	expected := &NoSuchContainer{ID: ""}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("CommitContainer: Wrong error returned. Want %#v. Got %#v.", expected, err)
	}
}

func TestAttachToContainerLogs(t *testing.T) {
	var req http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("something happened"))
		req = *r
	}))
	defer server.Close()
	client, _ := NewClient(server.URL)
	var buf bytes.Buffer
	opts := AttachToContainerOptions{
		Container:    "a123456",
		OutputStream: &buf,
		Stdout:       true,
		Stderr:       true,
		Logs:         true,
	}
	err := client.AttachToContainer(opts)
	if err != nil {
		t.Fatal(err)
	}
	expected := "something happened"
	if buf.String() != expected {
		t.Errorf("AttachToContainer for logs: wrong output. Want %q. Got %q.", expected, buf.String())
	}
	if req.Method != "POST" {
		t.Errorf("AttachToContainer: wrong HTTP method. Want POST. Got %s.", req.Method)
	}
	u, _ := url.Parse(client.getURL("/containers/a123456/attach"))
	if req.URL.Path != u.Path {
		t.Errorf("AttachToContainer for logs: wrong HTTP path. Want %q. Got %q.", u.Path, req.URL.Path)
	}
	expectedQs := map[string][]string{
		"logs":   {"1"},
		"stdout": {"1"},
		"stderr": {"1"},
	}
	got := map[string][]string(req.URL.Query())
	if !reflect.DeepEqual(got, expectedQs) {
		t.Errorf("AttachToContainer: wrong query string. Want %#v. Got %#v.", expectedQs, got)
	}
}

func TestAttachToContainer(t *testing.T) {
	file, err := os.OpenFile("/tmp/docker-temp-file.txt", os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	file.Write([]byte("send value"))
	file.Seek(0, 0)
	var req http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("something happened!"))
		req = *r
	}))
	defer server.Close()
	client, _ := NewClient(server.URL)
	var stdout, stderr bytes.Buffer
	opts := AttachToContainerOptions{
		Container:    "a123456",
		OutputStream: &stdout,
		ErrorStream:  &stderr,
		InputStream:  file,
		Stdin:        true,
		Stdout:       true,
		Stderr:       true,
		Stream:       true,
	}
	err = client.AttachToContainer(opts)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string][]string{
		"stdin":  {"1"},
		"stdout": {"1"},
		"stderr": {"1"},
		"stream": {"1"},
	}
	got := map[string][]string(req.URL.Query())
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("AttachToContainer: wrong query string. Want %#v. Got %#v.", expected, got)
	}
}

func TestAttachToContainerWithoutContainer(t *testing.T) {
	var client Client
	err := client.AttachToContainer(AttachToContainerOptions{})
	expected := &NoSuchContainer{ID: ""}
	if !reflect.DeepEqual(err, expected) {
		t.Errorf("AttachToContainer: wrong error. Want %#v. Got %#v.", expected, err)
	}
}

func TestNoSuchContainerError(t *testing.T) {
	var err error = &NoSuchContainer{ID: "i345"}
	expected := "No such container: i345"
	if got := err.Error(); got != expected {
		t.Errorf("NoSuchContainer: wrong message. Want %q. Got %q.", expected, got)
	}
}

func TestExportContainer(t *testing.T) {
	content := "exported container tar content"
	out := stdoutMock{bytes.NewBufferString(content)}
	client := newTestClient(&FakeRoundTripper{status: http.StatusOK})
	err := client.ExportContainer("4fa6e0f0c678", out)
	if err != nil {
		t.Errorf("ExportContainer: caugh error %#v while exporting container, expected nil", err.Error())
	}
	if out.String() != content {
		t.Errorf("ExportContainer: wrong stdout. Want %#v. Got %#v.", content, out.String())
	}
}

func TestExportContainerNoId(t *testing.T) {
	client := Client{}
	out := stdoutMock{bytes.NewBufferString("")}
	err := client.ExportContainer("", out)
	if err != (NoSuchContainer{}) {
		t.Errorf("ExportContainer: wrong error. Want %#v. Got %#v.", NoSuchContainer{}, err)
	}
}
