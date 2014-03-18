package main

import (
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/titanous/go-sql"
	_ "github.com/titanous/pq"
)

func TestOSFilesystem(t *testing.T) {
	dir, err := ioutil.TempDir("", "shelf")
	if err != nil {
		t.Fatal(err)
	}
	testFilesystem(NewOSFilesystem(dir), false, t)
	os.RemoveAll(dir)
}

func TestPostgresFilesystem(t *testing.T) {
	dbname := os.Getenv("PGDATABASE")
	sslmode := os.Getenv("PGSSLMODE")
	if dbname == "" {
		os.Setenv("PGDATABASE", "shelftest")
	}
	if sslmode == "" {
		os.Setenv("PGSSLMODE", "disable")
	}

	db, err := sql.Open("postgres", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec("DROP TABLE IF EXISTS files")
	if err != nil {
		t.Fatal(err)
	}

	schema, err := ioutil.ReadFile("schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(string(schema))
	if err != nil {
		t.Fatal(err)
	}

	testFilesystem(NewPostgresFilesystem(db), true, t)
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
	req.Header.Set("Content-Type", "text/plain")
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
	if testMeta {
		if ct := res.Header.Get("Content-Type"); ct != "text/plain" {
			t.Errorf(`Expected Content-Type to be "text/plain", got %q`, ct)
		}

		etag := res.Header.Get("Etag")
		if etag == "" {
			t.Error("Expected ETag to be set")
		}
		req, err := http.NewRequest("GET", path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("If-None-Match", etag)
		res, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotModified {
			t.Errorf("Expected ETag GET status to be 304, got %d", res.StatusCode)
		}
	}

	newData := "foobaz2"
	req, err = http.NewRequest("PUT", path, strings.NewReader(newData))
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/text")
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
	if testMeta {
		if ct := res.Header.Get("Content-Type"); ct != "application/text" {
			t.Errorf(`Expected Content-Type to be "application/text", got %q`, ct)
		}
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
