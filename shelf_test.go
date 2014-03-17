package main

import (
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestOSFilesystem(t *testing.T) {
	dir, err := ioutil.TempDir("", "shelf")
	if err != nil {
		t.Fatal(err)
	}
	testFilesystem(NewOSFilesystem(dir), false, t)
	os.RemoveAll(dir)
}

func testFilesystem(fs Filesystem, testMeta bool, t *testing.T) {
	srv := httptest.NewServer(handler(fs))
	defer srv.Close()

	path := srv.URL + "/foo/bar"

	res, err := http.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Errorf("Expected 404 for non-existent file, got %d", res.StatusCode)
	}

	res, err = http.Head(path)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Errorf("Expected 404 for non-existent file, got %d", res.StatusCode)
	}

	data := "foobar"
	req, err := http.NewRequest("PUT", path, strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Errorf("Expected 200 for successful PUT, got %d", res.StatusCode)
	}

	res, err = http.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	resData, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Errorf("Expected 200 for GET, got %d", res.StatusCode)
	}
	if string(resData) != data {
		t.Errorf("Expected data to be %q, got %q", data, string(resData))
	}

	res, err = http.Head(path)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Errorf("Expected 200 for HEAD, got %d", res.StatusCode)
	}
	if cl := res.Header.Get("Content-Length"); cl != "6" {
		t.Errorf(`Expected Content-Length to be "6", got %q`, cl)
	}

	newData := "foobaz2"
	req, err = http.NewRequest("PUT", path, strings.NewReader(newData))
	if err != nil {
		log.Fatal(err)
	}
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Errorf("Expected 200 for update PUT, got %d", res.StatusCode)
	}

	res, err = http.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	resData, err = ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Errorf("Expected 200 for update GET, got %d", res.StatusCode)
	}
	if string(resData) != newData {
		t.Errorf("Expected data to be %q, got %q", newData, string(resData))
	}

	res, err = http.Head(path)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Errorf("Expected 200 for update HEAD, got %d", res.StatusCode)
	}
	if cl := res.Header.Get("Content-Length"); cl != "7" {
		t.Errorf(`Expected Content-Length to be "7", got %q`, cl)
	}

	req, err = http.NewRequest("DELETE", path, nil)
	if err != nil {
		log.Fatal(err)
	}
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Errorf("Expected 200 for DELETE, got %d", res.StatusCode)
	}

	res, err = http.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Errorf("Expected 200 for deleted GET, got %d", res.StatusCode)
	}

	res, err = http.Head(path)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Errorf("Expected 200 for deleted HEAD, got %d", res.StatusCode)
	}
}
