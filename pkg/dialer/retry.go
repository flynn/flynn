package dialer

import (
	"net"
	"time"

	"github.com/flynn/flynn/pkg/attempt"
)

var Default = net.Dialer{
	Timeout:   time.Second,
	KeepAlive: 30 * time.Second,
}

var Retry = RetryDialer{
	Attempts: attempt.Strategy{
		Total: 30 * time.Second,
		Delay: 500 * time.Millisecond,
	},
}

type RetryDialer struct {
	Attempts attempt.Strategy
}

func (r RetryDialer) Dial(network, addr string) (net.Conn, error) {
	var conn net.Conn
	if err := r.Attempts.Run(func() (err error) {
		conn, err = Default.Dial(network, addr)
		return
	}); err != nil {
		return nil, err
	}
	return conn, nil
}

func (r RetryDialer) DialTimeout(network, addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout(network, addr, timeout)
}
