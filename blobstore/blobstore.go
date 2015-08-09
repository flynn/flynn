package main

import (
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/flynn/flynn/discoverd/client"
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
	Open(name string) (File, error)
	Put(name string, r io.Reader, typ string) error
	Delete(name string) error
	Status() status.Status
}

var ErrNotFound = errors.New("file not found")

func handler(fs Filesystem) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case "HEAD", "GET":
			file, err := fs.Open(req.URL.Path)
			if err != nil {
				errorResponse(w, err)
				return
			}
			defer file.Close()
			log.Println("GET", req.RequestURI)
			w.Header().Set("Content-Length", strconv.FormatInt(file.Size(), 10))
			w.Header().Set("Content-Type", file.Type())
			w.Header().Set("Etag", file.ETag())
			http.ServeContent(w, req, req.URL.Path, file.ModTime(), file)
		case "PUT":
			err := fs.Put(req.URL.Path, req.Body, req.Header.Get("Content-Type"))
			if err != nil {
				errorResponse(w, err)
				return
			}
			log.Println("PUT", req.RequestURI)
		case "DELETE":
			err := fs.Delete(req.URL.Path)
			if err != nil {
				errorResponse(w, err)
				return
			}
			log.Println("DELETE", req.RequestURI)
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
		db, err := postgres.Open("", "")
		if err != nil {
			shutdown.Fatal(err)
		}
		fs, err = NewPostgresFilesystem(db.DB)
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

	http.Handle("/", handler(fs))
	status.AddHandler(fs.Status)

	shutdown.Fatal(http.ListenAndServe(addr, nil))
}
