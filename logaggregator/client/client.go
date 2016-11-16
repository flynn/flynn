package client

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	logagg "github.com/flynn/flynn/logaggregator/types"
	"github.com/flynn/flynn/logaggregator/utils"
	"github.com/flynn/flynn/pkg/httpclient"
	"github.com/flynn/flynn/pkg/httphelper"
)

// ErrNotFound is returned when a resource is not found (HTTP status 404).
var ErrNotFound = errors.New("logaggregator: resource not found")

type Client struct {
	*httpclient.Client
}

// newClient creates a generic Client object, additional attributes must
// be set by the caller
func newClient(url string, http *http.Client) *Client {
	return &Client{
		Client: &httpclient.Client{
			ErrNotFound: ErrNotFound,
			URL:         url,
			HTTP:        http,
		},
	}
}

// NewClient creates a new Client pointing at uri.
func New(uri string) (*Client, error) {
	return NewWithHTTP(uri, httphelper.RetryClient)
}

// NewClient creates a new Client pointing at uri with the specified http client.
func NewWithHTTP(uri string, httpClient *http.Client) (*Client, error) {
	if uri == "" {
		uri = "http://logaggregator.discoverd"
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
func (c *Client) GetLog(channelID string, opts *logagg.LogOpts) (io.ReadCloser, error) {
	path := fmt.Sprintf("/log/%s", channelID)
	if opts != nil {
		if encodedQuery := opts.EncodedQuery(); encodedQuery != "" {
			path = fmt.Sprintf("%s?%s", path, encodedQuery)
		}
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
	Stream logagg.StreamType `json:"stream,omitempty"`
	// Timestamp is the time that this log line was emitted.
	Timestamp time.Time `json:"timestamp,omitempty"`
}

func (c *Client) GetCursors() (map[string]utils.HostCursor, error) {
	var res map[string]utils.HostCursor
	return res, c.Get("/cursors", &res)
}

func (c *Client) GetSnapshot() (io.ReadCloser, error) {
	res, err := c.RawReq("GET", "/snapshot", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}
