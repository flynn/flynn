// +build pprof

package pprof

import (
	"net/http"
	hpprof "net/http/pprof"
)

const Enabled = true

var Handler = http.NewServeMux()

func init() {
	Handler.Handle("/debug/pprof/", http.HandlerFunc(hpprof.Index))
	Handler.Handle("/debug/pprof/cmdline", http.HandlerFunc(hpprof.Cmdline))
	Handler.Handle("/debug/pprof/profile", http.HandlerFunc(hpprof.Profile))
	Handler.Handle("/debug/pprof/symbol", http.HandlerFunc(hpprof.Symbol))
}
