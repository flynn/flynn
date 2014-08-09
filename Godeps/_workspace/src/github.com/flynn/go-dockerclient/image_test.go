// Copyright 2013 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"testing"
)

func newTestClient(rt *FakeRoundTripper) Client {
	endpoint := "http://localhost:4243"
	u, _ := parseEndpoint(endpoint)
	client := Client{
		endpoint:    endpoint,
		endpointURL: u,
		client:      &http.Client{Transport: rt},
	}
	return client
}

type stdoutMock struct {
	*bytes.Buffer
}

func (m stdoutMock) Close() error {
	return nil
}

type stdinMock struct {
	*bytes.Buffer
}

func (m stdinMock) Close() error {
	return nil
}

func TestListImages(t *testing.T) {
	body := `[
     {
             "Repository":"base",
             "Tag":"ubuntu-12.10",
             "Id":"b750fe79269d",
             "Created":1364102658
     },
     {
             "Repository":"base",
             "Tag":"ubuntu-quantal",
             "Id":"b750fe79269d",
             "Created":1364102658
     }
]`
	var expected []APIImages
	err := json.Unmarshal([]byte(body), &expected)
	if err != nil {
		t.Fatal(err)
	}
	client := newTestClient(&FakeRoundTripper{message: body, status: http.StatusOK})
	images, err := client.ListImages(false)
	if err != nil {
		t.Error(err)
	}
	if !reflect.DeepEqual(images, expected) {
		t.Errorf("ListImages: Wrong return value. Want %#v. Got %#v.", expected, images)
	}
}

func TestListImagesParameters(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "null", status: http.StatusOK}
	client := newTestClient(fakeRT)
	_, err := client.ListImages(false)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	if req.Method != "GET" {
		t.Errorf("ListImages(false: Wrong HTTP method. Want GET. Got %s.", req.Method)
	}
	if all := req.URL.Query().Get("all"); all != "0" {
		t.Errorf("ListImages(false): Wrong parameter. Want all=0. Got all=%s", all)
	}
	fakeRT.Reset()
	_, err = client.ListImages(true)
	if err != nil {
		t.Fatal(err)
	}
	req = fakeRT.requests[0]
	if all := req.URL.Query().Get("all"); all != "1" {
		t.Errorf("ListImages(true): Wrong parameter. Want all=1. Got all=%s", all)
	}
}

func TestRemoveImage(t *testing.T) {
	name := "test"
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusNoContent}
	client := newTestClient(fakeRT)
	err := client.RemoveImage(name)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	expectedMethod := "DELETE"
	if req.Method != expectedMethod {
		t.Errorf("RemoveImage(%q): Wrong HTTP method. Want %s. Got %s.", name, expectedMethod, req.Method)
	}
	u, _ := url.Parse(client.getURL("/images/" + name))
	if req.URL.Path != u.Path {
		t.Errorf("RemoveImage(%q): Wrong request path. Want %q. Got %q.", name, u.Path, req.URL.Path)
	}
}

func TestRemoveImageNotFound(t *testing.T) {
	client := newTestClient(&FakeRoundTripper{message: "no such image", status: http.StatusNotFound})
	err := client.RemoveImage("test:")
	if err != ErrNoSuchImage {
		t.Errorf("RemoveImage: wrong error. Want %#v. Got %#v.", ErrNoSuchImage, err)
	}
}

func TestInspectImage(t *testing.T) {
	body := `{
     "id":"b750fe79269d2ec9a3c593ef05b4332b1d1a02a62b4accb2c21d589ff2f5f2dc",
     "parent":"27cf784147099545",
     "created":"2013-03-23T22:24:18.818426-07:00",
     "container":"3d67245a8d72ecf13f33dffac9f79dcdf70f75acb84d308770391510e0c23ad0",
     "container_config":{"Memory":0}
}`
	var expected Image
	json.Unmarshal([]byte(body), &expected)
	fakeRT := &FakeRoundTripper{message: body, status: http.StatusOK}
	client := newTestClient(fakeRT)
	image, err := client.InspectImage(expected.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*image, expected) {
		t.Errorf("InspectImage(%q): Wrong image returned. Want %#v. Got %#v.", expected.ID, expected, *image)
	}
	req := fakeRT.requests[0]
	if req.Method != "GET" {
		t.Errorf("InspectImage(%q): Wrong HTTP method. Want GET. Got %s.", expected.ID, req.Method)
	}
	u, _ := url.Parse(client.getURL("/images/" + expected.ID + "/json"))
	if req.URL.Path != u.Path {
		t.Errorf("InspectImage(%q): Wrong request URL. Want %q. Got %q.", expected.ID, u.Path, req.URL.Path)
	}
}

func TestInspectImageNotFound(t *testing.T) {
	client := newTestClient(&FakeRoundTripper{message: "no such image", status: http.StatusNotFound})
	name := "test"
	image, err := client.InspectImage(name)
	if image != nil {
		t.Errorf("InspectImage(%q): expected <nil> image, got %#v.", name, image)
	}
	if err != ErrNoSuchImage {
		t.Errorf("InspectImage(%q): wrong error. Want %#v. Got %#v.", name, ErrNoSuchImage, err)
	}
}

func TestPushImage(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "Pushing 1/100", status: http.StatusOK}
	client := newTestClient(fakeRT)
	var buf bytes.Buffer
	err := client.PushImage(PushImageOptions{Name: "test"}, AuthConfiguration{}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	expected := "Pushing 1/100"
	if buf.String() != expected {
		t.Errorf("PushImage: Wrong output. Want %q. Got %q.", expected, buf.String())
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("PushImage: Wrong HTTP method. Want POST. Got %s.", req.Method)
	}
	u, _ := url.Parse(client.getURL("/images/test/push"))
	if req.URL.Path != u.Path {
		t.Errorf("PushImage: Wrong request path. Want %q. Got %q.", u.Path, req.URL.Path)
	}
	if query := req.URL.Query().Encode(); query != "" {
		t.Errorf("PushImage: Wrong query string. Want no parameters, got %q.", query)
	}
	var b [2]byte
	req.Body.Read(b[:])
	if string(b[:]) != "{}" {
		t.Errorf("PushImage: wrong body. Want %q. Got %q.", "{}", string(b[:]))
	}
}

func TestPushImageWithAuthentication(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "Pushing 1/100", status: http.StatusOK}
	client := newTestClient(fakeRT)
	var buf bytes.Buffer
	inputAuth := AuthConfiguration{
		Username: "gopher",
		Password: "gopher123",
		Email:    "gopher@tsuru.io",
	}
	err := client.PushImage(PushImageOptions{Name: "test"}, inputAuth, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	var b [128]byte
	n, _ := req.Body.Read(b[:])
	var gotAuth AuthConfiguration
	err = json.Unmarshal(b[:n], &gotAuth)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotAuth, inputAuth) {
		t.Errorf("PushImage: wrong auth configuration. Want %#v. Got %#v.", inputAuth, gotAuth)
	}
}

func TestPushImageCustomRegistry(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "Pushing 1/100", status: http.StatusOK}
	client := newTestClient(fakeRT)
	var authConfig AuthConfiguration
	var buf bytes.Buffer
	err := client.PushImage(PushImageOptions{Name: "test", Registry: "docker.tsuru.io"}, authConfig, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	expectedQuery := "registry=docker.tsuru.io"
	if query := req.URL.Query().Encode(); query != expectedQuery {
		t.Errorf("PushImage: Wrong query string. Want %q. Got %q.", expectedQuery, query)
	}
}

func TestPushImageNoName(t *testing.T) {
	client := Client{}
	err := client.PushImage(PushImageOptions{}, AuthConfiguration{}, nil)
	if err != ErrNoSuchImage {
		t.Errorf("PushImage: got wrong error. Want %#v. Got %#v.", ErrNoSuchImage, err)
	}
}

func TestPullImage(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "Pulling 1/100", status: http.StatusOK}
	client := newTestClient(fakeRT)
	var buf bytes.Buffer
	err := client.PullImage(PullImageOptions{Repository: "base", OutputStream: &buf})
	if err != nil {
		t.Fatal(err)
	}
	expected := "Pulling 1/100"
	if buf.String() != expected {
		t.Errorf("PullImage: Wrong output. Want %q. Got %q.", expected, buf.String())
	}
	req := fakeRT.requests[0]
	if req.Method != "POST" {
		t.Errorf("PullImage: Wrong HTTP method. Want POST. Got %s.", req.Method)
	}
	u, _ := url.Parse(client.getURL("/images/create"))
	if req.URL.Path != u.Path {
		t.Errorf("PullImage: Wrong request path. Want %q. Got %q.", u.Path, req.URL.Path)
	}
	expectedQuery := "fromImage=base"
	if query := req.URL.Query().Encode(); query != expectedQuery {
		t.Errorf("PullImage: Wrong query strin. Want %q. Got %q.", expectedQuery, query)
	}
}

func TestPullImageCustomRegistry(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "Pulling 1/100", status: http.StatusOK}
	client := newTestClient(fakeRT)
	var buf bytes.Buffer
	opts := PullImageOptions{
		Repository:   "base",
		Registry:     "docker.tsuru.io",
		OutputStream: &buf,
	}
	err := client.PullImage(opts)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	expected := map[string][]string{"fromImage": {"base"}, "registry": {"docker.tsuru.io"}}
	got := map[string][]string(req.URL.Query())
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("PullImage: wrong query string. Want %#v. Got %#v.", expected, got)
	}
}

func TestPullImageTag(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "Pulling 1/100", status: http.StatusOK}
	client := newTestClient(fakeRT)
	var buf bytes.Buffer
	opts := PullImageOptions{
		Repository:   "base",
		Registry:     "docker.tsuru.io",
		Tag:          "latest",
		OutputStream: &buf,
	}
	err := client.PullImage(opts)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	expected := map[string][]string{"fromImage": {"base"}, "registry": {"docker.tsuru.io"}, "tag": {"latest"}}
	got := map[string][]string(req.URL.Query())
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("PullImage: wrong query string. Want %#v. Got %#v.", expected, got)
	}
}

func TestPullImageNoRepository(t *testing.T) {
	var opts PullImageOptions
	client := Client{}
	err := client.PullImage(opts)
	if err != ErrNoSuchImage {
		t.Errorf("PullImage: got wrong error. Want %#v. Got %#v.", ErrNoSuchImage, err)
	}
}

func TestImportImageFromUrl(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusOK}
	client := newTestClient(fakeRT)
	var buf bytes.Buffer
	opts := ImportImageOptions{Source: "http://mycompany.com/file.tar", Repository: "testimage"}
	err := client.ImportImage(opts, nil, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	expected := map[string][]string{"fromSrc": {opts.Source}, "repo": {opts.Repository}}
	got := map[string][]string(req.URL.Query())
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("ImportImage: wrong query string. Want %#v. Got %#v.", expected, got)
	}
}

func TestImportImageFromInput(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusOK}
	client := newTestClient(fakeRT)
	in := bytes.NewBufferString("tar content")
	var buf bytes.Buffer
	opts := ImportImageOptions{Source: "-", Repository: "testimage"}
	err := client.ImportImage(opts, in, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	expected := map[string][]string{"fromSrc": {opts.Source}, "repo": {opts.Repository}}
	got := map[string][]string(req.URL.Query())
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("ImportImage: wrong query string. Want %#v. Got %#v.", expected, got)
	}
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		t.Errorf("ImportImage: caugth error while reading body %#v", err.Error())
	}
	e := "tar content"
	if string(body) != e {
		t.Errorf("ImportImage: wrong body. Want %#v. Got %#v.", e, string(body))
	}
}

func TestImportImageDoesNotPassesInputIfSourceIsNotDash(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusOK}
	client := newTestClient(fakeRT)
	var buf bytes.Buffer
	in := bytes.NewBufferString("foo")
	opts := ImportImageOptions{Source: "http://test.com/container.tar", Repository: "testimage"}
	err := client.ImportImage(opts, in, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	expected := map[string][]string{"fromSrc": {opts.Source}, "repo": {opts.Repository}}
	got := map[string][]string(req.URL.Query())
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("ImportImage: wrong query string. Want %#v. Got %#v.", expected, got)
	}
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		t.Errorf("ImportImage: caugth error while reading body %#v", err.Error())
	}
	if string(body) != "" {
		t.Errorf("ImportImage: wrong body. Want nothing. Got %#v.", string(body))
	}
}

func TestImportImageShouldPassTarContentToBodyWhenSourceIsFilePath(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusOK}
	client := newTestClient(fakeRT)
	var buf bytes.Buffer
	tarPath := "testing/data/container.tar"
	opts := ImportImageOptions{Source: tarPath, Repository: "testimage"}
	err := client.ImportImage(opts, nil, &buf)
	if err != nil {
		t.Fatal(err)
	}
	tar, err := os.Open(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	tarContent, err := ioutil.ReadAll(tar)
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tarContent, body) {
		t.Errorf("ImportImage: wrong body. Want %#v content. Got %#v.", tarPath, body)
	}
}

func TestImportImageShouldChangeSourceToDashWhenItsAFilePath(t *testing.T) {
	fakeRT := &FakeRoundTripper{message: "", status: http.StatusOK}
	client := newTestClient(fakeRT)
	var buf bytes.Buffer
	tarPath := "testing/data/container.tar"
	opts := ImportImageOptions{Source: tarPath, Repository: "testimage"}
	err := client.ImportImage(opts, nil, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req := fakeRT.requests[0]
	expected := map[string][]string{"fromSrc": {"-"}, "repo": {opts.Repository}}
	got := map[string][]string(req.URL.Query())
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("ImportImage: wrong query string. Want %#v. Got %#v.", expected, got)
	}
}

func TestIsUrl(t *testing.T) {
	url := "http://foo.bar/"
	result := isUrl(url)
	if !result {
		t.Errorf("isUrl: wrong match. Expected %#v to be a url. Got %#v.", url, result)
	}
	url = "/foo/bar.tar"
	result = isUrl(url)
	if result {
		t.Errorf("isUrl: wrong match. Expected %#v to not be a url. Got %#v", url, result)
	}
}
