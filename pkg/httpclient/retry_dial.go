package httpclient

import (
	"net"
	"time"

	"github.com/flynn/flynn/pkg/attempt"
)

var defaultDialer = net.Dialer{
	Timeout:   time.Second,
	KeepAlive: 30 * time.Second,
}

var retryAttempts = attempt.Strategy{
	Total: 30 * time.Second,
	Delay: 500 * time.Millisecond,
}

func RetryDial(network, addr string) (net.Conn, error) {
	var conn net.Conn
	if err := retryAttempts.Run(func() (err error) {
		conn, err = defaultDialer.Dial(network, addr)
		return
	}); err != nil {
		return nil, err
	}
	return conn, nil
}
