package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/testutils/postgres"
)

func TestOSFilesystem(t *testing.T) {
	dir, err := ioutil.TempDir("", "blobstore")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	fs := NewOSFilesystem(dir)
	testList(fs, t)
	testDelete(fs, t)
	testOffset(fs, t)
	testFilesystem(fs, false, t)
}

func TestPostgresFilesystem(t *testing.T) {
	dbname := "blobstoretest"
	if err := pgtestutils.SetupPostgres(dbname); err != nil {
		t.Fatal(err)
	}
	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig: pgx.ConnConfig{
			Host:     os.Getenv("PGHOST"),
			Database: dbname,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	db := postgres.New(pgxpool, nil)
	defer db.Close()

	fs, err := NewPostgresFilesystem(db)
	if err != nil {
		t.Fatal(err)
	}
	testList(fs, t)
	testDelete(fs, t)
	testOffset(fs, t)
	testFilesystem(fs, true, t)
}

func testList(fs Filesystem, t *testing.T) {
	srv := httptest.NewServer(handler(fs))
	defer srv.Close()

	assertList := func(dir string, expected []string) {
		res, err := http.Get(srv.URL + "?dir=" + dir)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var actual []string
		if err := json.NewDecoder(res.Body).Decode(&actual); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("expected list to be %v, got %v", expected, actual)
		}
	}
	put := func(path string) {
		req, err := http.NewRequest("PUT", srv.URL+path, bytes.NewReader([]byte("data")))
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for PUT %s, got %d", path, res.StatusCode)
		}
	}
	del := func(path string) {
		req, err := http.NewRequest("DELETE", srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for DELETE %s, got %d", path, res.StatusCode)
		}
	}

	assertList("/", []string{})

	put("/foo.txt")
	assertList("/", []string{"/foo.txt"})
	assertList("/foo.txt", []string{})

	put("/bar.txt")
	put("/baz.txt")
	assertList("/", []string{"/bar.txt", "/baz.txt", "/foo.txt"})

	del("/foo.txt")
	assertList("/", []string{"/bar.txt", "/baz.txt"})

	put("/dir1/foo.txt")
	put("/dir1/bar.txt")
	put("/dir1/baz.txt")
	put("/dir2/foo.txt")
	assertList("/", []string{"/bar.txt", "/baz.txt", "/dir1/", "/dir2/"})
	assertList("/dir1", []string{"/dir1/bar.txt", "/dir1/baz.txt", "/dir1/foo.txt"})
	assertList("/dir2", []string{"/dir2/foo.txt"})

	del("/dir1/foo.txt")
	assertList("/", []string{"/bar.txt", "/baz.txt", "/dir1/", "/dir2/"})
	assertList("/dir1", []string{"/dir1/bar.txt", "/dir1/baz.txt"})
	assertList("/dir2", []string{"/dir2/foo.txt"})

	del("/dir1")
	assertList("/", []string{"/bar.txt", "/baz.txt", "/dir2/"})
	assertList("/dir1", []string{})
	assertList("/dir2", []string{"/dir2/foo.txt"})
}

func testDelete(fs Filesystem, t *testing.T) {
	put := func(path string) {
		if err := fs.Put(path, bytes.NewReader([]byte("data")), 0, "text/plain"); err != nil {
			t.Fatal(err)
		}
	}
	del := func(path string) {
		if err := fs.Delete(path); err != nil {
			t.Fatal(err)
		}
	}
	assertExists := func(path string) {
		f, err := fs.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	assertNotExists := func(path string) {
		f, err := fs.Open(path)
		if err == nil {
			f.Close()
		}
		if err != ErrNotFound {
			t.Fatalf("expected path %q to not exist, got err=%v", path, err)
		}
	}

	put("/dir/foo")
	put("/dir/foo.txt")
	put("/dir/bar.txt")
	del("/dir/foo")
	assertNotExists("/dir/foo")
	assertExists("/dir/foo.txt")
	assertExists("/dir/bar.txt")

	del("/dir")
	assertNotExists("/dir/foo")
	assertNotExists("/dir/foo.txt")
	assertNotExists("/dir/bar.txt")
}

func testOffset(fs Filesystem, t *testing.T) {
	srv := httptest.NewServer(handler(fs))
	defer srv.Close()

	put := func(path, data string, offset int) {
		req, err := http.NewRequest("PUT", srv.URL+path, bytes.NewReader([]byte(data)))
		if err != nil {
			t.Fatal(err)
		}
		if offset > 0 {
			req.Header.Set("Blobstore-Offset", strconv.Itoa(offset))
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected PUT %s to return %d, got %d", http.StatusOK, res.StatusCode)
		}
	}
	assert := func(path, expected string) {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, err := ioutil.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != expected {
			t.Fatalf("expected GET %q to return %s, got %s", path, expected, string(data))
		}
	}

	put("/foo.txt", "foo", 0)
	assert("/foo.txt", "foo")
	put("/foo.txt", "bar", 3)
	assert("/foo.txt", "foobar")
	put("/foo.txt", "baz", 2)
	assert("/foo.txt", "fobazr")
}

const concurrency = 5

func testFilesystem(fs Filesystem, testMeta bool, t *testing.T) {
	srv := httptest.NewServer(handler(fs))
	defer srv.Close()

	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			path := srv.URL + "/foo/bar/" + random.Hex(16)
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

			data := random.Hex(16)
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
			if cl := res.Header.Get("Content-Length"); cl != "32" {
				t.Errorf(`Expected Content-Length to be "32", got %q`, cl)
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

			newPath := srv.URL + "/foo/bar/" + random.Hex(16)
			req, err = http.NewRequest("PUT", newPath, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Blobstore-Copy-From", strings.TrimPrefix(path, srv.URL))
			res, err = http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != 200 {
				t.Errorf("Expected 200 for copy PUT, got %d", res.StatusCode)
			}
			res, err = http.Get(newPath)
			if err != nil {
				t.Fatal(err)
			}
			resData, err = ioutil.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != 200 {
				t.Errorf("Expected 200 for copy GET, got %d", res.StatusCode)
			}
			if string(resData) != data {
				t.Errorf("Expected copied data to be %q, got %q", data, string(resData))
			}

			newData := random.Hex(32)
			req, err = http.NewRequest("PUT", path, strings.NewReader(newData))
			if err != nil {
				shutdown.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/text")
			res, err = http.DefaultClient.Do(req)
			if err != nil {
				shutdown.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != 200 {
				t.Errorf("Expected 200 for update PUT, got %d", res.StatusCode)
			}

			var wg2 sync.WaitGroup
			wg2.Add(concurrency)
			for i := 0; i < concurrency; i++ {
				go func() {
					defer wg2.Done()
					res, err := http.Get(path)
					if err != nil {
						t.Fatal(err)
					}
					resData, err := ioutil.ReadAll(res.Body)
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
					if cl := res.Header.Get("Content-Length"); cl != "64" {
						t.Errorf(`Expected Content-Length to be "64", got %q`, cl)
					}
					if testMeta {
						if ct := res.Header.Get("Content-Type"); ct != "application/text" {
							t.Errorf(`Expected Content-Type to be "application/text", got %q`, ct)
						}
					}
				}()
			}
			wg2.Wait()

			req, err = http.NewRequest("DELETE", path, nil)
			if err != nil {
				shutdown.Fatal(err)
			}
			res, err = http.DefaultClient.Do(req)
			if err != nil {
				shutdown.Fatal(err)
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
		}()
	}

	wg.Wait()
}
