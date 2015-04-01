package proxy

import (
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/pkg/connutil"
)

func TestServeConnClientGone(t *testing.T) {
	control, conn := net.Pipe()
	cnConn := connutil.CloseNotifyConn(conn)

	clientGone := false
	dialer = dialerFunc(func(_, _ string) (net.Conn, error) {
		if clientGone {
			err := errors.New("dial after client gone")
			t.Error(err)
			return nil, err
		}

		if err := control.Close(); err != nil {
			t.Fatal(err)
		}
		<-cnConn.(http.CloseNotifier).CloseNotify()

		clientGone = true
		return nil, &dialErr{}
	})

	fn := func() []string { return []string{"127.0.0.1:0", "127.0.0.1:0"} }
	prox := NewReverseProxy(fn, nil, false)

	prox.ServeConn(context.Background(), cnConn)
}

type dialerFunc func(string, string) (net.Conn, error)

func (f dialerFunc) Dial(network, addr string) (net.Conn, error) {
	return f(network, addr)
}
