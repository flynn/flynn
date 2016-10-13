package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"

	"github.com/flynn/flynn/controller/client"
	logaggc "github.com/flynn/flynn/logaggregator/client"
	logagg "github.com/flynn/flynn/logaggregator/types"

	"github.com/flynn/go-docopt"
)

func init() {
	register("log", runLog, `
usage: flynn log [-f] [-j <id>] [-n <lines>] [-r] [-s] [-t <type>] [-i]

Stream log for an app.

Options:
	-f, --follow               stream new lines
	-j, --job=<id>             filter logs to a specific job ID
	-n, --number=<lines>       return at most n lines from the log buffer
	-r, --raw-output           output raw log messages with no prefix
	-s, --split-stderr         send stderr lines to stderr
	-t, --process-type=<type>  filter logs to a specific process type
	-i, --init                 output containerinit logs to stderr
`)
}

// like time.RFC3339Nano except it only goes to 6 decimals and doesn't drop
// trailing zeros
const rfc3339micro = "2006-01-02T15:04:05.000000Z07:00"

func runLog(args *docopt.Args, client controller.Client) error {
	rawOutput := args.Bool["--raw-output"]
	opts := logagg.LogOpts{
		Follow: args.Bool["--follow"],
		JobID:  args.String["--job"],
		StreamTypes: []logagg.StreamType{
			logagg.StreamTypeStdout,
			logagg.StreamTypeStderr,
		},
	}
	if ptype, ok := args.String["--process-type"]; ok {
		opts.ProcessType = &ptype
	}
	if strlines := args.String["--number"]; strlines != "" {
		lines, err := strconv.Atoi(strlines)
		if err != nil {
			return err
		}
		opts.Lines = &lines
	}
	if args.Bool["--init"] {
		opts.StreamTypes = append(opts.StreamTypes, logagg.StreamTypeInit)
	}
	rc, err := client.GetAppLog(mustApp(), &opts)
	if err != nil {
		return err
	}
	defer rc.Close()

	var stderr io.Writer = os.Stdout
	if args.Bool["--split-stderr"] {
		stderr = os.Stderr
	}
	var initOut io.Writer = ioutil.Discard
	if args.Bool["--init"] {
		initOut = os.Stderr
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

		var stream io.Writer
		switch msg.Stream {
		case logagg.StreamTypeStdout:
			stream = os.Stdout
		case logagg.StreamTypeStderr:
			stream = stderr
		case logagg.StreamTypeInit:
			stream = initOut
		default:
			continue
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
