package dialer

import (
	"net"
	"time"

	"github.com/flynn/flynn/pkg/attempt"
)

type DialFunc func(network, addr string) (net.Conn, error)

var Default = net.Dialer{
	Timeout:   time.Second,
	KeepAlive: 30 * time.Second,
}

var DialAttempts = attempt.Strategy{
	Total: 30 * time.Second,
	Delay: 500 * time.Millisecond,
}

var Retry = RetryDialer{Default.Dial}

func RetryDial(dial DialFunc) DialFunc {
	return RetryDialer{dial}.Dial
}

type RetryDialer struct {
	dial DialFunc
}

func (r RetryDialer) Dial(network, addr string) (net.Conn, error) {
	var conn net.Conn
	if err := DialAttempts.Run(func() (err error) {
		conn, err = r.dial(network, addr)
		return
	}); err != nil {
		return nil, err
	}
	return conn, nil
}

func (r RetryDialer) DialTimeout(network, addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout(network, addr, timeout)
}
