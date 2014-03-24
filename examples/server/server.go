package main

import (
	"log"
	"net/http"
	"os"

	"github.com/flynn/go-discoverd"
)

func main() {
	d, err := discoverd.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	if err := d.Register("example-server", os.Args[1]); err != nil {
		log.Fatal(err)
	}
	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("Listening on " + os.Args[1]))
	})
	log.Fatal(http.ListenAndServe(":"+os.Args[1], nil))
}
