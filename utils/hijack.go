package utils

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
)

type writeCloser interface {
	io.WriteCloser
	CloseWrite() error
}

type ReadWriteCloser interface {
	io.ReadWriteCloser
	CloseWrite() error
}

func HijackRequest(req *http.Request, dial func(string, string) (net.Conn, error)) (*http.Response, ReadWriteCloser, error) {
	if dial == nil {
		dial = net.Dial
	}
	conn, err := dial("tcp", req.URL.Host)
	if err != nil {
		return nil, nil, err
	}
	clientconn := httputil.NewClientConn(conn, nil)
	res, err := clientconn.Do(req)
	if err != nil && err != httputil.ErrPersistEOF {
		return nil, nil, err
	}
	if res.StatusCode != http.StatusSwitchingProtocols {
		return res, nil, &url.Error{
			Op:  req.Method,
			URL: req.URL.String(),
			Err: fmt.Errorf("controller: unexpected status %d", res.StatusCode),
		}
	}
	var rwc io.ReadWriteCloser
	var buf *bufio.Reader
	rwc, buf = clientconn.Hijack()
	if buf.Buffered() > 0 {
		rwc = struct {
			io.Reader
			writeCloser
		}{
			io.MultiReader(io.LimitReader(buf, int64(buf.Buffered())), rwc),
			rwc.(writeCloser),
		}
	}
	return res, rwc.(ReadWriteCloser), nil
}
