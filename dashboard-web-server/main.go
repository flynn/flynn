package main

import (
	"log"
	"net/http"
)

func main() {
	conf := LoadConfigFromEnv()
	addr := conf.Port
	h := APIHandler(conf)
	log.Fatal(http.ListenAndServe(addr, h))
}
