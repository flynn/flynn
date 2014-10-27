package httpclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/rpcplus"
)

type Client struct {
	ErrNotFound error
	ErrPrefix   string
	URL         string
	Key         string
	HTTP        *http.Client
	Dial        rpcplus.DialFunc
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
	if res.StatusCode == 404 {
		res.Body.Close()
		return res, c.ErrNotFound
	}
	if res.StatusCode == 400 {
		var body ct.ValidationError
		defer res.Body.Close()
		if err = json.NewDecoder(res.Body).Decode(&body); err != nil {
			return res, err
		}
		return res, body
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		return res, &url.Error{
			Op:  req.Method,
			URL: req.URL.String(),
			Err: fmt.Errorf(c.ErrPrefix+": unexpected status %d", res.StatusCode),
		}
	}
	if out != nil {
		defer res.Body.Close()
		return res, json.NewDecoder(res.Body).Decode(out)
	}
	return res, nil
}

func (c *Client) Send(method, path string, in, out interface{}) error {
	_, err := c.RawReq(method, path, nil, in, out)
	return err
}

func (c *Client) Put(path string, in, out interface{}) error {
	return c.Send("PUT", path, in, out)
}

func (c *Client) Post(path string, in, out interface{}) error {
	return c.Send("POST", path, in, out)
}

func (c *Client) Get(path string, out interface{}) error {
	_, err := c.RawReq("GET", path, nil, nil, out)
	return err
}

func (c *Client) Delete(path string) error {
	res, err := c.RawReq("DELETE", path, nil, nil, nil)
	if err == nil {
		res.Body.Close()
	}
	return err
}
