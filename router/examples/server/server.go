package main

import (
	"log"
	"net/http"
	"os"

	"github.com/flynn/flynn/discoverd/client"
)

func main() {
	addr := ":" + os.Args[1]
	hb, err := discoverd.AddServiceAndRegister("example-server", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer hb.Close()
	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("Listening on " + os.Args[1]))
	})
	if err := http.ListenAndServe(addr, nil); err != nil {
		hb.Close()
		log.Fatal(err)
	}
}
