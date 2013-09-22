package main

import (
	"net/http"

	"github.com/flynn/rpcplus"
)

func main() {
	rpcplus.HandleHTTP()
	http.ListenAndServe(":1112", nil)
}
