package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/flynn/flynn/blobstore/backend"
	"github.com/flynn/flynn/blobstore/data"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/status"
)

func errorResponse(w http.ResponseWriter, err error) {
	if err == backend.ErrNotFound {
		http.Error(w, "NotFound", 404)
		return
	}
	log.Println("error:", err)
	http.Error(w, "Internal Server Error", 500)
}

func handler(r *data.FileRepo) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		path := path.Clean(req.URL.Path)

		if path == "/" {
			if req.Method == "HEAD" {
				return
			} else if req.Method != "GET" {
				w.WriteHeader(404)
				return
			}
			paths, err := r.List(req.URL.Query().Get("dir"))
			if err != nil && err != backend.ErrNotFound {
				errorResponse(w, err)
				return
			}
			if paths == nil {
				paths = []string{}
			}
			sort.Strings(paths)
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(paths)
			return
		}

		switch req.Method {
		case "HEAD", "GET":
			file, err := r.Get(path, req.Method == "GET")
			if err != nil {
				errorResponse(w, err)
				return
			}
			if file.FileStream != nil {
				defer file.Close()
			}
			if r, ok := file.FileStream.(backend.Redirector); ok && req.Method == "GET" {
				http.Redirect(w, req, r.RedirectURL(), http.StatusFound)
				return
			}
			w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
			w.Header().Set("Content-Type", file.Type)
			w.Header().Set("Etag", file.ETag)
			http.ServeContent(w, req, path, file.ModTime, file)
		case "PUT":
			var err error
			if src := req.Header.Get("Blobstore-Copy-From"); src != "" {
				err = r.Copy(path, src)
			} else {
				var offset int64
				if s := req.Header.Get("Blobstore-Offset"); s != "" {
					offset, err = strconv.ParseInt(s, 10, 64)
					if err != nil {
						errorResponse(w, err)
						return
					}
				}
				err = r.Put(path, req.Body, offset, req.Header.Get("Content-Type"))
			}
			if err != nil {
				errorResponse(w, err)
				return
			}
			w.WriteHeader(200)
		case "DELETE":
			err := r.Delete(path)
			if err != nil {
				errorResponse(w, err)
				return
			}
			w.WriteHeader(200)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

const configEnvPrefix = "BACKEND_"

func main() {
	defer shutdown.Exit()

	addr := ":" + os.Getenv("PORT")

	db := postgres.Wait(nil, nil)
	if err := migrateDB(db); err != nil {
		shutdown.Fatalf("error running DB migrations: %s", err)
	}

	hb, err := discoverd.AddServiceAndRegister("blobstore", addr)
	if err != nil {
		shutdown.Fatal(err)
	}
	shutdown.BeforeExit(func() { hb.Close() })

	mux := http.NewServeMux()
	backends := []backend.Backend{backend.Postgres}

	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, configEnvPrefix) {
			continue
		}
		nameInfo := strings.SplitN(env, "=", 2)
		name := strings.ToLower(strings.TrimPrefix(nameInfo[0], configEnvPrefix))
		info := parseBackendInfo(nameInfo[1])
		if info["backend"] != "s3" {
			shutdown.Fatalf("error: unknown backend %q for %s", info["backend"], name)
		}
		b, err := backend.NewS3(name, info)
		if err != nil {
			shutdown.Fatal(err)
		}
		log.Println("Configured additional backend: %s (%s)", name, info["backend"])
		backends = append(backends, b)
	}

	defaultBackend := "postgres"
	if d := os.Getenv("DEFAULT_BACKEND"); d != "" {
		defaultBackend = d
		var found bool
		for _, b := range backends {
			if b.Name() == d {
				found = true
				break
			}
		}
		if !found {
			shutdown.Fatalf("error: unknow default backend %q", d)
		}
	}

	log.Println("Blobstore serving files on " + addr)

	mux.Handle("/", handler(data.NewFileRepo(db, backends, defaultBackend)))
	mux.Handle(status.Path, status.Handler(func() status.Status {
		if err := db.Exec("SELECT 1"); err != nil {
			return status.Unhealthy
		}
		return status.Healthy
	}))

	h := httphelper.ContextInjector("blobstore", httphelper.NewRequestLogger(mux))
	shutdown.Fatal(http.ListenAndServe(addr, h))
}

func parseBackendInfo(s string) map[string]string {
	info := make(map[string]string)
	for _, token := range strings.Split(s, " ") {
		kv := strings.SplitN(token, "=", 2)
		info[kv[0]] = info[kv[1]]
	}
	return info
}

func migrateDB(db *postgres.DB) error {
	m := postgres.NewMigrations()
	m.Add(1,
		`CREATE TABLE files (
	file_id oid PRIMARY KEY DEFAULT lo_create(0),
	name text UNIQUE NOT NULL,
	size bigint,
	type text,
	digest text,
	created_at timestamp with time zone NOT NULL DEFAULT current_timestamp
)`,
		`CREATE FUNCTION delete_file() RETURNS TRIGGER AS $$
    BEGIN
        PERFORM lo_unlink(OLD.file_id);
        RETURN NULL;
    END;
$$ LANGUAGE plpgsql`,
		`CREATE TRIGGER delete_file
    AFTER DELETE ON files
    FOR EACH ROW EXECUTE PROCEDURE delete_file()`,
	)
	m.Add(2,
		`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,
		`CREATE TABLE new_files (
  file_id uuid PRIMARY KEY DEFAULT uuid_generate_v4(),
  file_oid oid,
  external_id uuid,
  backend text NOT NULL,
  name text NOT NULL,
  type text NOT NULL,
  size bigint,
  sha512 bytea,
  sha512_state bytea,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
)`,
		`INSERT INTO new_files (file_oid, backend, name, size, type, sha512, created_at, updated_at)
			SELECT file_id, 'postgres', name, size, type, decode(digest, 'hex'), created_at, created_at FROM files`,
		`DROP TABLE files`,
		`DROP FUNCTION delete_file()`,
		`ALTER TABLE new_files RENAME TO files`,
		`CREATE UNIQUE INDEX ON files (name) WHERE deleted_at IS NULL`,
		`CREATE INDEX ON files (file_oid)`,
		`CREATE FUNCTION delete_file() RETURNS TRIGGER AS $$
			BEGIN
				IF NEW.deleted_at IS NOT NULL AND NEW.file_oid IS NOT NULL THEN
					PERFORM lo_unlink(OLD.file_oid);
					NEW.file_oid := NULL;
				END IF;
				RETURN NEW;
			END;
		$$ LANGUAGE plpgsql`,
		`CREATE TRIGGER delete_file BEFORE UPDATE OF deleted_at ON files FOR EACH ROW EXECUTE PROCEDURE delete_file()`,
	)

	return m.Migrate(db)
}
