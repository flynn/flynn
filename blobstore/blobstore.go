package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/status"
)

var (
	storageDir       = flag.String("s", "", "Path to store files, instead of Postgres")
	listenPort       = flag.String("p", "3001", "Port to listen on")
	serviceDiscovery = flag.Bool("d", true, "Register with service discovery")
)

func errorResponse(w http.ResponseWriter, err error) {
	if err == ErrNotFound {
		http.Error(w, "NotFound", 404)
		return
	}
	log.Println("error:", err)
	http.Error(w, "Internal Server Error", 500)
}

type File interface {
	io.ReadSeeker
	io.Closer
	Size() int64
	ModTime() time.Time
	Type() string
	ETag() string
}

type Filesystem interface {
	List(dir string) ([]string, error)
	Open(name string) (File, error)
	Put(name string, r io.Reader, offset int64, typ string) error
	Copy(dst, src string) error
	Delete(name string) error
	Status() status.Status
}

var ErrNotFound = errors.New("file not found")

func handler(fs Filesystem) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		path := path.Clean(req.URL.Path)

		if req.Method == "GET" && path == "/" {
			paths, err := fs.List(req.URL.Query().Get("dir"))
			if err != nil && err != ErrNotFound {
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
			file, err := fs.Open(path)
			if err != nil {
				errorResponse(w, err)
				return
			}
			defer file.Close()
			w.Header().Set("Content-Length", strconv.FormatInt(file.Size(), 10))
			w.Header().Set("Content-Type", file.Type())
			w.Header().Set("Etag", file.ETag())
			http.ServeContent(w, req, path, file.ModTime(), file)
		case "PUT":
			var err error
			if src := req.Header.Get("Blobstore-Copy-From"); src != "" {
				err = fs.Copy(path, src)
			} else {
				var offset int64
				if s := req.Header.Get("Blobstore-Offset"); s != "" {
					offset, err = strconv.ParseInt(s, 10, 64)
					if err != nil {
						errorResponse(w, err)
						return
					}
				}
				err = fs.Put(path, req.Body, offset, req.Header.Get("Content-Type"))
			}
			if err != nil {
				errorResponse(w, err)
				return
			}
			w.WriteHeader(200)
		case "DELETE":
			err := fs.Delete(path)
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

	flag.Parse()

	addr := os.Getenv("PORT")
	if addr == "" {
		addr = *listenPort
	}
	addr = ":" + addr

	var fs Filesystem
	var storageDesc string

	if *storageDir != "" {
		fs = NewOSFilesystem(*storageDir)
		storageDesc = *storageDir
	} else {
		var err error
		db := postgres.Wait(nil, nil)
		fs, err = NewPostgresFilesystem(db)
		if err != nil {
			shutdown.Fatal(err)
		}
		storageDesc = "Postgres"
	}

	if *serviceDiscovery {
		hb, err := discoverd.AddServiceAndRegister("blobstore", addr)
		if err != nil {
			shutdown.Fatal(err)
		}
		shutdown.BeforeExit(func() { hb.Close() })
	}

	log.Println("Blobstore serving files on " + addr + " from " + storageDesc)

	mux := http.NewServeMux()
	mux.Handle("/", handler(fs))
	mux.Handle(status.Path, status.Handler(fs.Status))

	h := httphelper.ContextInjector("blobstore", httphelper.NewRequestLogger(mux))
	shutdown.Fatal(http.ListenAndServe(addr, h))
}
