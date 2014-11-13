package cliutil

import (
	"encoding/json"
	"io"
	"os"
)

// DecodeJSONArg decodes JSON into v from a file named name, if name is empty or
// "-", stdin is used.
func DecodeJSONArg(name string, v interface{}) error {
	var src io.Reader = os.Stdin
	if name != "-" && name != "" {
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		src = f
	}
	return json.NewDecoder(src).Decode(v)
}
