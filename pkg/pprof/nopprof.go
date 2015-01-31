// +build !pprof

package pprof

import (
	"net/http"
)

const Enabled = false

var Handler = http.HandlerFunc(http.NotFound)
