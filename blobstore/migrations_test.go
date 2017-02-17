package main

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flynn/flynn/blobstore/backend"
	"github.com/flynn/flynn/blobstore/data"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/jackc/pgx"
)

type testMigrator struct {
	t  *testing.T
	db *postgres.DB
	id int
}

func (t *testMigrator) migrateTo(id int) {
	if err := (*dbMigrations)[t.id:id].Migrate(t.db); err != nil {
		t.t.Fatal(err)
	}
	t.id = id
}

func TestMultiBackendMigration(t *testing.T) {
	db := createDB(t, "blobstore_backendmigration")
	defer db.Close()
	m := &testMigrator{t: t, db: db}

	m.migrateTo(1)

	// insert a file
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var id pgx.Oid
	const typ = "text/plain"
	err = tx.QueryRow("INSERT INTO files (name, type, size, digest) VALUES ($1, $2, $3, $4) RETURNING file_id", "/bar.txt", typ, 3, "f7fbba6e0636f890e56fbbf3283e524c6fa3204ae298382d624741d0dc6638326e282c41be5e4254d8820772c5518a2c5a8c0c7f7eda19594a7eb539453e1ed7").Scan(&id)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	lo, err := tx.LargeObjects()
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	obj, err := lo.Open(id, pgx.LargeObjectModeWrite)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	const expected = "foo"
	if _, err := obj.Write([]byte(expected)); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	m.migrateTo(2)

	// check that the file can be read successfully
	r := data.NewFileRepo(db, []backend.Backend{backend.Postgres}, "postgres")
	srv := httptest.NewServer(handler(r))
	defer srv.Close()
	res, err := http.Get(srv.URL + "/bar.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}
	const etag = `"9/u6bgY2+JDlb7vzKD5STG+jIErimDgtYkdB0NxmODJuKCxBvl5CVNiCB3LFUYosWowMf37aGVlKfrU5RT4e1w=="`
	if e := res.Header.Get("Etag"); e != etag {
		t.Fatalf("expected etag %q, got %q", etag, e)
	}
	if ct := res.Header.Get("Content-Type"); ct != typ {
		t.Fatalf("exected type %q, got %q", typ, ct)
	}
	contents, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if expected != string(contents) {
		t.Fatalf("expected data %q, got %q", expected, string(contents))
	}
}
