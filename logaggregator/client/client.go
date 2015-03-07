package client

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/flynn/flynn/pkg/httpclient"
)

// ErrNotFound is returned when a resource is not found (HTTP status 404).
var ErrNotFound = errors.New("logaggregator: resource not found")

type Client interface {
	GetLog(channelID string, lines int, follow bool) (io.ReadCloser, error)
}

type client struct {
	*httpclient.Client
}

// newClient creates a generic Client object, additional attributes must
// be set by the caller
func newClient(url string, http *http.Client) *client {
	c := &client{
		Client: &httpclient.Client{
			ErrNotFound: ErrNotFound,
			URL:         url,
			HTTP:        http,
		},
	}
	return c
}

// NewClient creates a new Client pointing at uri.
func New(uri string) (Client, error) {
	return NewWithHTTP(uri, http.DefaultClient)
}

// NewClient creates a new Client pointing at uri with the specified http client.
func NewWithHTTP(uri string, httpClient *http.Client) (Client, error) {
	if uri == "" {
		uri = "http://flynn-logaggregator-api.discoverd"
	}
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	return newClient(u.String(), httpClient), nil
}

// GetLog returns a ReadCloser log stream of the log channel with ID channelID.
// Each line returned will be a JSON serialized Message.
//
// If lines is above zero, the number of lines returned will be capped at that
// value. Otherwise, all available logs are returned. If follow is true, new log
// lines are streamed after the buffered log.
func (c *client) GetLog(channelID string, lines int, follow bool) (io.ReadCloser, error) {
	path := fmt.Sprintf("/log/%s", channelID)
	query := url.Values{}
	if lines > 0 {
		query.Add("lines", strconv.Itoa(lines))
	}
	if follow {
		query.Add("follow", "true")
	}
	if encodedQuery := query.Encode(); encodedQuery != "" {
		path = fmt.Sprintf("%s?%s", path, encodedQuery)
	}
	res, err := c.RawReq("GET", path, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

// Message represents a single log message.
type Message struct {
	// Hostname is the host that the job was running on when this log message was
	// emitted.
	HostID string `json:"host_id,omitempty"`
	// JobID is the ID of the job that emitted this log message.
	JobID string `json:"job_id,omitempty"`
	// Msg is the actual content of this log message.
	Msg string `json:"msg,omitempty"`
	// ProcessType is the type of process that emitted this log message.
	ProcessType string `json:"process_type,omitempty"`
	// Source is the source of this log message, such as "app" or "router".
	Source string `json:"source,omitempty"`
	// Stream is the I/O stream that emitted this message, such as "stdout" or
	// "stderr".
	Stream string `json:"stream,omitempty"`
	// Timestamp is the time that this log line was emitted.
	Timestamp time.Time `json:"timestamp,omitempty"`
}
