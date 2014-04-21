// Copyright 2013 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// ListContainersOptions specify parameters to the ListContainers function.
//
// See http://goo.gl/QpCnDN for more details.
type ListContainersOptions struct {
	All    bool
	Size   bool
	Limit  int
	Since  string
	Before string
}

// ListContainers returns a slice of containers matching the given criteria.
//
// See http://goo.gl/QpCnDN for more details.
func (c *Client) ListContainers(opts ListContainersOptions) ([]APIContainers, error) {
	path := "/containers/json?" + queryString(opts)
	body, _, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	var containers []APIContainers
	err = json.Unmarshal(body, &containers)
	if err != nil {
		return nil, err
	}
	return containers, nil
}

// InspectContainer returns information about a container by its ID.
//
// See http://goo.gl/2o52Sx for more details.
func (c *Client) InspectContainer(id string) (*Container, error) {
	path := "/containers/" + id + "/json"
	body, status, err := c.do("GET", path, nil)
	if status == http.StatusNotFound {
		return nil, &NoSuchContainer{ID: id}
	}
	if err != nil {
		return nil, err
	}
	var container Container
	err = json.Unmarshal(body, &container)
	if err != nil {
		return nil, err
	}
	return &container, nil
}

// CreateContainer creates a new container, returning the container instance,
// or an error in case of failure.
//
// See http://goo.gl/tjihUc for more details.
func (c *Client) CreateContainer(config *Config) (*Container, error) {
	path := "/containers/create"
	if config.Name != "" {
		path += "?name=" + config.Name
	}
	body, status, err := c.do("POST", path, config)
	if status == http.StatusNotFound {
		return nil, ErrNoSuchImage
	}
	if err != nil {
		return nil, err
	}
	var container Container
	err = json.Unmarshal(body, &container)
	if err != nil {
		return nil, err
	}
	return &container, nil
}

// StartContainer starts a container, returning an errror in case of failure.
//
// See http://goo.gl/y5GZlE for more details.
func (c *Client) StartContainer(id string, hostConfig *HostConfig) error {
	if hostConfig == nil {
		hostConfig = &HostConfig{}
	}
	path := "/containers/" + id + "/start"
	_, status, err := c.do("POST", path, hostConfig)
	if status == http.StatusNotFound {
		return &NoSuchContainer{ID: id}
	}
	if err != nil {
		return err
	}
	return nil
}

// StopContainer stops a container, killing it after the given timeout (in
// seconds).
//
// See http://goo.gl/X2mj8t for more details.
func (c *Client) StopContainer(id string, timeout uint) error {
	path := fmt.Sprintf("/containers/%s/stop?t=%d", id, timeout)
	_, status, err := c.do("POST", path, nil)
	if status == http.StatusNotFound {
		return &NoSuchContainer{ID: id}
	}
	if err != nil {
		return err
	}
	return nil
}

// RestartContainer stops a container, killing it after the given timeout (in
// seconds), during the stop process.
//
// See http://goo.gl/zms73Z for more details.
func (c *Client) RestartContainer(id string, timeout uint) error {
	path := fmt.Sprintf("/containers/%s/restart?t=%d", id, timeout)
	_, status, err := c.do("POST", path, nil)
	if status == http.StatusNotFound {
		return &NoSuchContainer{ID: id}
	}
	if err != nil {
		return err
	}
	return nil
}

// KillContainer kills a container, returning an error in case of failure.
//
// See http://goo.gl/DPbbBy for more details.
func (c *Client) KillContainer(id string) error {
	path := "/containers/" + id + "/kill"
	_, status, err := c.do("POST", path, nil)
	if status == http.StatusNotFound {
		return &NoSuchContainer{ID: id}
	}
	if err != nil {
		return err
	}
	return nil
}

// RemoveContainer removes a container, returning an error in case of failure.
//
// See http://goo.gl/PBvGdU for more details.
func (c *Client) RemoveContainer(id string) error {
	_, status, err := c.do("DELETE", "/containers/"+id, nil)
	if status == http.StatusNotFound {
		return &NoSuchContainer{ID: id}
	}
	if err != nil {
		return err
	}
	return nil
}

// WaitContainer blocks until the given container stops, return the exit code
// of the container status.
//
// See http://goo.gl/gnHJL2 for more details.
func (c *Client) WaitContainer(id string) (int, error) {
	body, status, err := c.do("POST", "/containers/"+id+"/wait", nil)
	if status == http.StatusNotFound {
		return 0, &NoSuchContainer{ID: id}
	}
	if err != nil {
		return 0, err
	}
	var r struct{ StatusCode int }
	err = json.Unmarshal(body, &r)
	if err != nil {
		return 0, err
	}
	return r.StatusCode, nil
}

// CommitContainerOptions aggregates parameters to the CommitContainer method.
//
// See http://goo.gl/628gxm for more details.
type CommitContainerOptions struct {
	Container  string
	Repository string `qs:"repo"`
	Tag        string
	Message    string `qs:"m"`
	Author     string
	Run        *Config
}

// CommitContainer creates a new image from a container's changes.
//
// See http://goo.gl/628gxm for more details.
func (c *Client) CommitContainer(opts CommitContainerOptions) (*Image, error) {
	path := "/commit?" + queryString(opts)
	body, status, err := c.do("POST", path, nil)
	if status == http.StatusNotFound {
		return nil, &NoSuchContainer{ID: opts.Container}
	}
	if err != nil {
		return nil, err
	}
	var image Image
	err = json.Unmarshal(body, &image)
	if err != nil {
		return nil, err
	}
	return &image, nil
}

// AttachToContainerOptions is the set of options that can be used when
// attaching to a container.
//
// See http://goo.gl/oPzcqH for more details.
type AttachToContainerOptions struct {
	Container    string
	InputStream  io.Reader
	OutputStream io.Writer
	ErrorStream  io.Writer

	// Get container logs, sending it to OutputStream.
	Logs bool

	// Stream the response?
	Stream bool

	// Attach to stdin, and use InputFile.
	Stdin bool

	// Attach to stdout, and use OutputStream.
	Stdout bool

	// Attach to stderr, and use ErrorStream.
	Stderr bool

	// If set, after a successful connect, a sentinel will be sent and then the
	// client will block on receive before continuing.
	Success chan struct{}
}

// AttachToContainer attaches to a container, using the given options.
//
// See http://goo.gl/oPzcqH for more details.
func (c *Client) AttachToContainer(opts AttachToContainerOptions) error {
	container := opts.Container
	if container == "" {
		return &NoSuchContainer{ID: container}
	}
	stdout := opts.OutputStream
	stderr := opts.ErrorStream
	stdin := opts.InputStream
	success := opts.Success
	opts.Container = ""
	opts.InputStream = nil
	opts.OutputStream = nil
	opts.ErrorStream = nil
	opts.Success = nil
	path := "/containers/" + container + "/attach?" + queryString(opts)
	return c.hijack("POST", path, success, stdin, stderr, stdout)
}

func (c *Client) ResizeContainerTTY(id string, height, width int) error {
	params := make(url.Values)
	params.Set("h", strconv.Itoa(height))
	params.Set("w", strconv.Itoa(width))
	_, _, err := c.do("POST", "/containers/"+id+"/resize?"+params.Encode(), nil)
	return err
}

// ExportContainer export the contents of container id as tar archive
// and prints the exported contents to stdout.
//
// see http://goo.gl/Lqk0FZ for more details.
func (c *Client) ExportContainer(id string, out io.Writer) error {
	if id == "" {
		return NoSuchContainer{ID: id}
	}
	url := fmt.Sprintf("/containers/%s/export", id)
	return c.stream("GET", url, nil, out)
}

// NoSuchContainer is the error returned when a given container does not exist.
type NoSuchContainer struct {
	ID string
}

func (err NoSuchContainer) Error() string {
	return "No such container: " + err.ID
}
