package main

import (
	"log"
	"net/http"
)

func main() {
	conf := LoadConfigFromEnv()
	h := APIHandler(conf)
	log.Fatal(http.ListenAndServe(conf.Addr, h))
}
