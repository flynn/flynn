package main

import (
	"log"
	"net/http"
)

func main() {
	conf := MustConfig()
	h := NewDashboardHandler(conf)
	log.Fatal(http.ListenAndServe(conf.Addr, h))
}
