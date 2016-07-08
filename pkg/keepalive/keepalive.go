// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package keepalive provides a listener that enables TCP keepalives.
package keepalive

import (
	"net"
	"time"
)

// Listener returns a net.Listener that enables TCP keep-alive timeouts on
// accepted connections. It allows detection of dead TCP connections (e.g.
// closing laptop mid-download) to eventually go away. Derived from the Go
// net/http package.
func Listener(l net.Listener) net.Listener {
	return keepaliveListener{l.(*net.TCPListener)}
}

type keepaliveListener struct {
	*net.TCPListener
}

func (l keepaliveListener) Accept() (c net.Conn, err error) {
	tc, err := l.AcceptTCP()
	if err != nil {
		return
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)
	return tc, nil
}
