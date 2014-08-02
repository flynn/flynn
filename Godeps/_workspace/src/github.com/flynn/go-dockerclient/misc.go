// Copyright 2013 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
)

// Version returns version information about the docker server.
//
// See http://goo.gl/IqKNRE for more details.
func (c *Client) Version() (*APIVersion, error) {
	body, _, err := c.do("GET", "/version", nil)
	if err != nil {
		return nil, err
	}
	var version APIVersion
	err = json.Unmarshal(body, &version)
	if err != nil {
		return nil, err
	}
	return &version, nil
}

// Info returns system-wide information, like the number of running containers.
//
// See http://goo.gl/LOmySw for more details.
func (c *Client) Info() (*APIInfo, error) {
	body, _, err := c.do("GET", "/info", nil)
	if err != nil {
		return nil, err
	}
	var info APIInfo
	err = json.Unmarshal(body, &info)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

type EventStream struct {
	Events chan *Event
	// Error must only be read after Stream has closed
	Error error

	conn io.ReadCloser
}

func (s *EventStream) Close() error { return s.conn.Close() }

func (s *EventStream) stream() {
	decoder := json.NewDecoder(s.conn)
	for {
		event := &Event{}
		if err := decoder.Decode(event); err != nil {
			if err == io.EOF {
				err = nil
			}
			s.Error = err
			break
		}
		s.Events <- event
	}
	close(s.Events)
	s.conn.Close()
}

type readCloser struct {
	io.Reader
	io.Closer
}

// Events returns a stream of container events.
func (c *Client) Events() (*EventStream, error) {
	req, err := http.NewRequest("GET", c.getURL("/events"), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	protocol := c.endpointURL.Scheme
	var res *http.Response
	var closer io.Closer
	if protocol == "unix" {
		address := c.endpointURL.Path
		dial, err := net.Dial(protocol, address)
		if err != nil {
			return nil, err
		}
		clientconn := httputil.NewClientConn(dial, nil)
		res, err = clientconn.Do(req)
		closer = clientconn
	} else {
		res, err = c.client.Do(req)
		if err != nil {
			closer = res.Body
		}
	}
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") {
			err = ErrConnectionRefused
		}
		return nil, err
	}
	if res.StatusCode != 200 {
		body, _ := ioutil.ReadAll(res.Body)
		res.Body.Close()
		return nil, newError(res.StatusCode, body)
	}

	stream := &EventStream{
		Events: make(chan *Event),
		conn:   readCloser{res.Body, closer},
	}
	go stream.stream()
	return stream, nil
}
