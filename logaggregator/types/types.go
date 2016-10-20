package logaggregator

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type LogOpts struct {
	Follow      bool
	JobID       string
	Lines       *int
	ProcessType *string
	StreamTypes []StreamType
}

func (o *LogOpts) EncodedQuery() string {
	query := url.Values{}
	if o.Follow {
		query.Set("follow", "true")
	}
	if o.JobID != "" {
		query.Set("job_id", o.JobID)
	}
	if o.Lines != nil && *o.Lines >= 0 {
		query.Set("lines", strconv.Itoa(*o.Lines))
	}
	if o.ProcessType != nil {
		query.Set("process_type", *o.ProcessType)
	}
	if len(o.StreamTypes) > 0 {
		streamTypes := make([]string, len(o.StreamTypes))
		for i, typ := range o.StreamTypes {
			streamTypes[i] = string(typ)
		}
		query.Set("stream_types", strings.Join(streamTypes, ","))
	} else {
		// default to just stdout / stderr
		query.Set("stream_types", fmt.Sprintf("%s,%s", StreamTypeStdout, StreamTypeStderr))
	}
	return query.Encode()
}

type StreamType string

const (
	StreamTypeStdout  StreamType = "stdout"
	StreamTypeStderr  StreamType = "stderr"
	StreamTypeInit    StreamType = "init"
	StreamTypeUnknown StreamType = "unknown"
)

type MsgID string

const (
	MsgIDStdout MsgID = "ID1"
	MsgIDStderr MsgID = "ID2"
	MsgIDInit   MsgID = "ID3"
)
