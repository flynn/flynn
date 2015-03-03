package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/flynn/flynn/controller/client"
	logaggc "github.com/flynn/flynn/logaggregator/client"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
)

func init() {
	register("log", runLog, `
usage: flynn log [options]

Stream log for an app.

Options:
	-f, --follow         stream new lines after printing log buffer
	-j, --job <id>       filter logs to a specific job ID
	-n, --number <lines> return at most n lines from the log buffer
	-r, --raw-output     output raw log messages with no prefix
	-s, --split-stderr   send stderr lines to stderr
`)
}

// like time.RFC3339Nano except it only goes to 6 decimals and doesn't drop
// trailing zeros
const rfc3339micro = "2006-01-02T15:04:05.000000Z07:00"

func runLog(args *docopt.Args, client *controller.Client) error {
	rawOutput := args.Bool["--raw-output"]
	lines := 0
	if strlines := args.String["--number"]; strlines != "" {
		var err error
		if lines, err = strconv.Atoi(strlines); err != nil {
			return err
		}
	}
	rc, err := client.GetAppLog(mustApp(), lines, args.Bool["--follow"])
	if err != nil {
		return err
	}
	defer rc.Close()

	var stderr io.Writer = os.Stdout
	if args.Bool["--split-stderr"] {
		stderr = os.Stderr
	}

	dec := json.NewDecoder(rc)
	for {
		var msg logaggc.Message
		err := dec.Decode(&msg)
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}

		var stream io.Writer = os.Stdout
		if msg.Stream == "stderr" {
			stream = stderr
		}
		if rawOutput {
			fmt.Fprintln(stream, msg.Msg)
		} else {
			tstamp := msg.Timestamp.Format(rfc3339micro)
			fmt.Fprintf(stream, "%s %s[%s.%s]: %s\n",
				tstamp,
				msg.Source,
				msg.ProcessType,
				msg.JobID,
				msg.Msg,
			)
		}
	}
}

func shorten(msg string, maxLength int) string {
	if len(msg) > maxLength {
		return msg[:maxLength]
	}
	return msg
}
