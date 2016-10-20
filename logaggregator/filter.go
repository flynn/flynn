package main

import (
	"bytes"

	logagg "github.com/flynn/flynn/logaggregator/types"
	"github.com/flynn/flynn/logaggregator/utils"
	"github.com/flynn/flynn/pkg/syslog/rfc5424"
)

type Filter interface {
	Match(*rfc5424.Message) bool
	Filter([]*rfc5424.Message) []*rfc5424.Message
}

type filterFunc func(m *rfc5424.Message) bool

func (f filterFunc) Filter(messages []*rfc5424.Message) []*rfc5424.Message {
	msgs := make([]*rfc5424.Message, 0, len(messages))
	for _, msg := range messages {
		if f.Match(msg) {
			msgs = append(msgs, msg)
		}
	}
	return msgs
}

func (f filterFunc) Match(m *rfc5424.Message) bool {
	return f(m)
}

func filterJobID(jobID string) filterFunc {
	a := []byte(jobID)
	return func(m *rfc5424.Message) bool {
		_, b := splitProcID(m.ProcID)
		return bytes.Equal(a, b)
	}
}

func filterProcessType(processType string) filterFunc {
	a := []byte(processType)
	return func(m *rfc5424.Message) bool {
		b, _ := splitProcID(m.ProcID)
		return bytes.Equal(a, b)
	}
}

func filterStreamType(streams ...logagg.StreamType) filterFunc {
	lookup := make(map[logagg.StreamType]struct{}, len(streams))
	for _, stream := range streams {
		lookup[stream] = struct{}{}
	}
	return func(m *rfc5424.Message) bool {
		_, ok := lookup[utils.StreamType(m)]
		return ok
	}
}

type filterSlice []Filter

func (s filterSlice) Filter(unfiltered []*rfc5424.Message) []*rfc5424.Message {
	msgs := make([]*rfc5424.Message, 0, len(unfiltered))
	for _, msg := range unfiltered {
		if s.Match(msg) {
			msgs = append(msgs, msg)
		}
	}
	return msgs
}

func (s filterSlice) Match(m *rfc5424.Message) bool {
	for _, filter := range s {
		if !filter.Match(m) {
			return false
		}
	}
	return true
}
