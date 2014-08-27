package main

import (
	"log"
	"net/http"

	ct "github.com/flynn/flynn/controller/types"
)

var ErrInvalidLoginToken = ct.ValidationError{Field: "token", Message: "Incorrect token"}

type ServerError struct {
	Message string `json:"message"`
}

func main() {
	conf := LoadConfigFromEnv()
	h := APIHandler(conf)
	log.Fatal(http.ListenAndServe(conf.Addr, h))
}
