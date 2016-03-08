package health

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const defaultTimeout = 2 * time.Second

type Check interface {
	Check() error
}

var _ Check = &TCPCheck{}
var _ Check = &HTTPCheck{}

type TCPCheck struct {
	Addr    string
	Timeout time.Duration
}

func (c *TCPCheck) Check() error {
	d := &net.Dialer{
		Timeout: c.Timeout,
	}
	if d.Timeout == 0 {
		d.Timeout = defaultTimeout
	}
	conn, err := d.Dial("tcp", c.Addr)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

func (c *TCPCheck) String() string {
	return "tcp://" + c.Addr
}

type HTTPCheck struct {
	// URL is the URL that will be requested
	URL string

	// Host specifies the host to be used in the Host header as well as the TLS
	// SNI extension. It is optional, if unset the host from the URL will be
	// used.
	Host string

	Timeout time.Duration
	// The response must respond with StatusCode for the check to pass. It
	// defaults to 200.
	StatusCode int
	// If set, MatchBytes must be a subset present in the first 5120 bytes of
	// the response for the check to pass.
	MatchBytes []byte
}

func (c *HTTPCheck) Check() error {
	req, err := http.NewRequest("GET", c.URL, nil)
	if err != nil {
		return err
	}
	req.Host = c.Host

	client := &http.Client{
		Timeout: c.Timeout,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			TLSClientConfig: &tls.Config{
				// Don't verify TLS certificates since this is just a health check,
				// not a connection that needs confidentiality or authenticity.
				InsecureSkipVerify: true,
				ServerName:         c.Host,
			},
		},
	}
	if client.Timeout == 0 {
		client.Timeout = defaultTimeout
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	expectedStatus := c.StatusCode
	if expectedStatus == 0 {
		expectedStatus = 200
	}
	if res.StatusCode != expectedStatus {
		return fmt.Errorf("healthcheck: expected HTTP status %d, got %d", expectedStatus, res.StatusCode)
	}
	if len(c.MatchBytes) == 0 {
		return nil
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(io.LimitReader(res.Body, 5120)); err != nil {
		return err
	}
	if !bytes.Contains(buf.Bytes(), c.MatchBytes) {
		return fmt.Errorf("healthcheck: response did not match expected bytes")
	}
	return nil
}

func (c *HTTPCheck) String() string {
	return c.URL
}
