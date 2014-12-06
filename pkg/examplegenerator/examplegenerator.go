package examplegenerator

import (
	"encoding/json"
	"io"

	"github.com/flynn/flynn/pkg/httprecorder"
)

type Example struct {
	Name   string
	Runner func()
}

func WriteOutput(r *httprecorder.Recorder, examples []Example, out io.Writer) error {
	res := make(map[string]*httprecorder.CompiledRequest)
	for _, ex := range examples {
		ex.Runner()
		res[ex.Name] = r.GetRequests()[0]
	}

	var err error
	data, err := json.MarshalIndent(res, "", "\t")
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	return err
}
