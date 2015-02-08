package main

import (
	"net/http"
	"os"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/shutdown"
)

const service = "ping-service"

func main() {
	defer shutdown.Exit()

	port := os.Getenv("PORT")
	addr := ":" + port

	hb, err := discoverd.AddServiceAndRegister(service, addr)
	if err != nil {
		shutdown.Fatal(err)
	}

	shutdown.BeforeExit(func() { hb.Close() })
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})
	http.ListenAndServe(addr, nil)
}
