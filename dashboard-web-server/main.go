package main

import (
	"errors"
	"log"
	"net/http"

	ct "github.com/flynn/flynn/controller/types"
)

var ErrNotFound = errors.New("common: not found")
var ErrInvalidLoginToken = ct.ValidationError{Field: "token", Message: "Incorrect token"}

type ServerError struct {
	Message string `json:"message"`
}

func main() {
	conf := LoadConfigFromEnv()
	addr := conf.Port
	h := APIHandler(conf)
	log.Fatal(http.ListenAndServe(addr, h))
}
