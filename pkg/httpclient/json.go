package httpclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/stream"
)

type DialFunc func(network, addr string) (net.Conn, error)

type Client struct {
	ErrNotFound error
	ErrPrefix   string
	URL         string
	Key         string
	HTTP        *http.Client
	Dial        DialFunc
	DialClose   io.Closer
}

// Close closes the underlying transport connection.
func (c *Client) Close() error {
	if c.DialClose != nil {
		c.DialClose.Close()
	}
	return nil
}

func ToJSON(v interface{}) (io.Reader, error) {
	data, err := json.Marshal(v)
	return bytes.NewBuffer(data), err
}

func (c *Client) RawReq(method, path string, header http.Header, in, out interface{}) (*http.Response, error) {
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

	req, err := http.NewRequest(method, c.URL+path, payload)
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
	res, err := c.HTTP.Do(req)
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
			Err: fmt.Errorf("httpclient: unexpected status %d", res.StatusCode),
		}
	}
	if out != nil {
		defer res.Body.Close()
		return res, json.NewDecoder(res.Body).Decode(out)
	}
	return res, nil
}

// Stream returns a stream.Stream for a specific method and path. in is an
// optional json object to be sent to the server via the body, and out is a
// required channel, to which the output will be streamed.
func (c *Client) Stream(method, path string, in, out interface{}) (stream.Stream, error) {
	header := http.Header{"Accept": []string{"text/event-stream"}}
	res, err := c.RawReq(method, path, header, in, nil)
	if err != nil {
		return nil, err
	}
	return Stream(res, out), nil
}

func (c *Client) Send(method, path string, in, out interface{}) error {
	res, err := c.RawReq(method, path, nil, in, out)
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
