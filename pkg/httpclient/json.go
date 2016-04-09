package httpclient

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/stream"
)

type DialFunc func(network, addr string) (net.Conn, error)

type writeCloser interface {
	io.WriteCloser
	CloseWrite() error
}

type ReadWriteCloser interface {
	io.ReadWriteCloser
	CloseWrite() error
}

type Client struct {
	ErrNotFound error
	URL         string
	Key         string
	Host        string
	HTTP        *http.Client
	HijackDial  DialFunc
}

func ToJSON(v interface{}) (io.Reader, error) {
	data, err := json.Marshal(v)
	return bytes.NewBuffer(data), err
}

func (c *Client) prepareReq(method, rawurl string, header http.Header, in interface{}) (*http.Request, error) {
	var payload io.Reader
	switch v := in.(type) {
	case io.Reader:
		payload = v
	case nil:
	default:
		var err error
		payload, err = ToJSON(in)
		if err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequest(method, rawurl, payload)
	if err != nil {
		return nil, err
	}
	if header == nil {
		header = make(http.Header)
	}
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", "application/json")
	}
	req.Header = header
	if c.Key != "" {
		req.SetBasicAuth("", c.Key)
	}
	if c.Host != "" {
		req.Host = c.Host
	}
	return req, nil
}

func (c *Client) RawReq(method, path string, header http.Header, in, out interface{}) (*http.Response, error) {
	return c.RawReqWithHTTP(method, path, header, in, out, c.HTTP)
}

func (c *Client) RawReqWithHTTP(method, path string, header http.Header, in, out interface{}, client *http.Client) (*http.Response, error) {
	rawurl := c.URL + path

	for {
		resp, err := c.rawReq(method, rawurl, header, in, out, client)

		// If this is a redirect then update the URL and try again.
		if resp != nil && resp.StatusCode == http.StatusTemporaryRedirect {
			resp.Body.Close()
			rawurl = resp.Header.Get("Location")
			continue
		}

		return resp, err
	}
}

func (c *Client) rawReq(method, rawurl string, header http.Header, in, out interface{}, client *http.Client) (*http.Response, error) {
	req, err := c.prepareReq(method, rawurl, header, in)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		defer res.Body.Close()
		if strings.Contains(res.Header.Get("Content-Type"), "application/json") {
			var jsonErr httphelper.JSONError
			if err := json.NewDecoder(res.Body).Decode(&jsonErr); err == nil {
				return res, jsonErr
			}
		}
		if res.StatusCode == 404 {
			return res, c.ErrNotFound
		}
		return res, &url.Error{
			Op:  req.Method,
			URL: req.URL.String(),
			Err: fmt.Errorf("httpclient: raw req: unexpected status %d", res.StatusCode),
		}
	}
	if out != nil {
		defer res.Body.Close()
		return res, json.NewDecoder(res.Body).Decode(out)
	}
	return res, nil
}

func (c *Client) Hijack(method, path string, header http.Header, in interface{}) (ReadWriteCloser, error) {
	uri, err := url.Parse(c.URL)
	if err != nil {
		return nil, err
	}
	dial := c.HijackDial
	if dial == nil {
		dial = net.Dial
	}

	addr := uri.Host
	if _, _, err := net.SplitHostPort(addr); err != nil { // host is missing port
		port := "443"
		if uri.Scheme == "http" {
			port = "80"
		}
		addr = net.JoinHostPort(addr, port)
	}

	conn, err := dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	clientconn := httputil.NewClientConn(conn, nil)
	req, err := c.prepareReq(method, c.URL+path, header, in)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Connection", "upgrade")
	res, err := clientconn.Do(req)
	if err != nil && err != httputil.ErrPersistEOF {
		return nil, err
	}
	if res.StatusCode != http.StatusSwitchingProtocols {
		defer res.Body.Close()
		if strings.Contains(res.Header.Get("Content-Type"), "application/json") {
			var jsonErr httphelper.JSONError
			if err := json.NewDecoder(res.Body).Decode(&jsonErr); err == nil {
				return nil, jsonErr
			}
		}
		return nil, &url.Error{
			Op:  req.Method,
			URL: req.URL.String(),
			Err: fmt.Errorf("httpclient: unexpected status %d", res.StatusCode),
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
	return rwc.(ReadWriteCloser), nil
}

// Stream returns a stream.Stream for a specific method and path. in is an
// optional json object to be sent to the server via the body, and out is a
// required channel, to which the output will be streamed.
func (c *Client) Stream(method, path string, in, out interface{}) (stream.Stream, error) {
	return c.StreamWithHeader(method, path, make(http.Header), in, out)
}

func (c *Client) ResumingStream(method, path string, ch interface{}) (stream.Stream, error) {
	// use a copy of the client with a zero timeout (it doesn't really
	// make sense to have a resuming stream with a timeout)
	httpClient := *c.HTTP
	httpClient.Timeout = 0

	connect := func(lastID int64) (*http.Response, error, bool) {
		header := http.Header{
			"Accept":        []string{"text/event-stream"},
			"Last-Event-Id": []string{strconv.FormatInt(lastID, 10)},
		}
		res, err := c.RawReqWithHTTP(method, path, header, nil, nil, &httpClient)
		return res, err, err != c.ErrNotFound
	}
	return ResumingStream(connect, ch)
}

func (c *Client) StreamWithHeader(method, path string, header http.Header, in, out interface{}) (stream.Stream, error) {
	header.Set("Accept", "text/event-stream")
	res, err := c.RawReq(method, path, header, in, nil)
	if err != nil {
		return nil, err
	}
	return Stream(res, out), nil
}

func (c *Client) Send(method, path string, in, out interface{}) error {
	h := http.Header{"Accept": []string{"application/json"}}
	res, err := c.RawReq(method, path, h, in, out)
	if err == nil && out == nil {
		res.Body.Close()
	}
	return err
}

func (c *Client) Put(path string, in, out interface{}) error {
	return c.Send("PUT", path, in, out)
}

func (c *Client) Post(path string, in, out interface{}) error {
	return c.Send("POST", path, in, out)
}

func (c *Client) Get(path string, out interface{}) error {
	return c.Send("GET", path, nil, out)
}

func (c *Client) Delete(path string) error {
	return c.Send("DELETE", path, nil, nil)
}
