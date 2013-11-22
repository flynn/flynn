package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

var port = flag.String("p", "8888", "Port to listen on")

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:	%v -p <port> <storage-path>\n\n", os.Args[0])
		flag.PrintDefaults()
	}
}

func errorResponse(w http.ResponseWriter, e error) {
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte(e.Error()))
	log.Println("error:", e.Error())
}

func main() {
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(64)
	}

	storagepath := flag.Arg(0)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		filepath := storagepath + r.RequestURI
		switch r.Method {
		case "GET":
			file, err := os.Open(filepath)
			if err != nil {
				errorResponse(w, err)
				return
			}
			defer file.Close()
			_, err = io.Copy(w, file)
			if err != nil {
				errorResponse(w, err)
				return
			}
			log.Println("GET", r.RequestURI)
		case "PUT":
			file, err := os.Create(filepath)
			if err != nil {
				errorResponse(w, err)
				return
			}
			defer file.Close()
			_, err = io.Copy(file, r.Body)
			if err != nil {
				errorResponse(w, err)
				return
			}
			log.Println("PUT", r.RequestURI)
		case "DELETE":
			err := os.RemoveAll(filepath)
			if err != nil {
				errorResponse(w, err)
				return
			}
			log.Println("DELETE", r.RequestURI)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	log.Println("Shelf serving files on " + *port + " from " + storagepath)
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}
