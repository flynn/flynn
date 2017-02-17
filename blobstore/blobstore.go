package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/flynn/flynn/blobstore/backend"
	"github.com/flynn/flynn/blobstore/data"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/status"
	"github.com/flynn/flynn/pkg/version"
	docopt "github.com/flynn/go-docopt"
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
			w.Header().Set("Etag", fmt.Sprintf(`"%s"`, file.ETag))
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

func main() {
	defer shutdown.Exit()

	usage := `
usage: flynn-blobstore <command> [<args>...]

Commands:
        help        show usage for a specific command
        cleanup     delete file blobs from default backend 
        migrate     move file blobs from default backend to a different backend
        server      run blobstore HTTP server

See 'flynn-blobstore help <command>' for more information on a specific command.
`[1:]
	args, _ := docopt.Parse(usage, nil, true, version.String(), true)

	cmd := args.String["<command>"]
	cmdArgs := args.All["<args>"].([]string)

	if cmd == "help" {
		if len(cmdArgs) == 0 { // `flynn-blobstore help`
			fmt.Println(usage)
			return
		} else { // `flynn-blobstore help <command>`
			cmd = cmdArgs[0]
			cmdArgs = []string{"--help"}
		}
	}

	if err := runCommand(cmd, cmdArgs); err != nil {
		log.Println(err)
		shutdown.ExitWithCode(1)
	}
}

func runServer(_ *docopt.Args) error {
	addr := ":" + os.Getenv("PORT")

	db := postgres.Wait(nil, nil)
	if err := dbMigrations.Migrate(db); err != nil {
		return fmt.Errorf("error running DB migrations: %s", err)
	}

	mux := http.NewServeMux()

	repo, err := data.NewFileRepoFromEnv(db)
	if err != nil {
		return err
	}

	hb, err := discoverd.AddServiceAndRegister("blobstore", addr)
	if err != nil {
		return err
	}
	shutdown.BeforeExit(func() { hb.Close() })

	log.Println("Blobstore serving files on " + addr)

	mux.Handle("/", handler(repo))
	mux.Handle(status.Path, status.Handler(func() status.Status {
		if err := db.Exec("SELECT 1"); err != nil {
			return status.Unhealthy
		}
		return status.Healthy
	}))

	h := httphelper.ContextInjector("blobstore", httphelper.NewRequestLogger(mux))
	return http.ListenAndServe(addr, h)
}

var dbMigrations = postgres.NewMigrations()

func init() {
	dbMigrations.Add(1,
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
	dbMigrations.Add(2,
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

	register("server", runServer, `
usage: flynn-blobstore server

Run blobstore HTTP server.
`)
}

type command struct {
	usage string
	f     func(args *docopt.Args) error
}

var commands = make(map[string]*command)

func register(cmd string, f func(args *docopt.Args) error, usage string) *command {
	c := &command{usage: strings.TrimLeftFunc(usage, unicode.IsSpace), f: f}
	commands[cmd] = c
	return c
}

func runCommand(name string, args []string) (err error) {
	argv := make([]string, 1, 1+len(args))
	argv[0] = name
	argv = append(argv, args...)

	cmd, ok := commands[name]
	if !ok {
		return fmt.Errorf("%s is not a flynn-blobstore command. See 'flynn-blobstore help'", name)
	}
	parsedArgs, err := docopt.Parse(cmd.usage, argv, true, "", false)
	if err != nil {
		return err
	}

	return cmd.f(parsedArgs)
}
