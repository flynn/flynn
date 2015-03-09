package main

import (
	"bytes"

	"github.com/flynn/flynn/pkg/syslog/rfc5424"
)

type filter interface {
	Match(m *rfc5424.Message) bool
}

type filterJobID struct {
	jobID []byte
}

func (f filterJobID) Match(m *rfc5424.Message) bool {
	_, jobID := splitProcID(m.ProcID)
	return bytes.Equal(f.jobID, jobID)
}

type filterProcessType struct {
	processType []byte
}

func (f filterProcessType) Match(m *rfc5424.Message) bool {
	procType, _ := splitProcID(m.ProcID)
	return bytes.Equal(f.processType, procType)
}

func allFiltersMatch(msg *rfc5424.Message, filters []filter) bool {
	for _, filter := range filters {
		if !filter.Match(msg) {
			return false
		}
	}
	return true
}

func filterMessages(unfiltered []*rfc5424.Message, filters []filter) []*rfc5424.Message {
	messages := make([]*rfc5424.Message, 0, len(unfiltered))
	for _, msg := range unfiltered {
		if allFiltersMatch(msg, filters) {
			messages = append(messages, msg)
		}
	}
	return messages
}
