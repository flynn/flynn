package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	ch := make(chan struct{})

	http.HandleFunc("/block", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-ch
		w.Write([]byte("done"))
	})

	http.HandleFunc("/unblock", func(w http.ResponseWriter, req *http.Request) {
		close(ch)
	})

	http.HandleFunc("/ping", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(200)
	})

	if err := http.ListenAndServe(":"+os.Getenv("PORT"), nil); err != nil {
		log.Fatal(err)
	}
}
