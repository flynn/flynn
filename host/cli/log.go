package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/go-docopt"
)

func init() {
	Register("log", runLog, `
usage: flynn-host log [--init] [-f|--follow] [--lines=<number>] [--split-stderr] ID

Get the logs of a job`)
}

func runLog(args *docopt.Args, client *cluster.Client) error {
	jobID := args.String["ID"]
	hostID, err := cluster.ExtractHostID(jobID)
	if err != nil {
		return err
	}

	lines := 0
	if args.String["--lines"] != "" {
		lines, err = strconv.Atoi(args.String["--lines"])
		if err != nil {
			return err
		}
	}

	stderr := os.Stdout
	if args.Bool["--split-stderr"] {
		stderr = os.Stderr
	}

	if lines > 0 {
		stdoutR, stdoutW := io.Pipe()
		stderrR, stderrW := io.Pipe()

		go func() {
			getLog(hostID, jobID, client, false, args.Bool["--init"], stdoutW, stderrW)
			stdoutW.Close()
			stderrW.Close()
		}()
		tailLogs(stdoutR, stderrR, lines, os.Stdout, stderr)
		return nil
	}
	return getLog(
		hostID,
		jobID,
		client,
		args.Bool["-f"] || args.Bool["--follow"],
		args.Bool["--init"],
		os.Stdout,
		stderr,
	)
}

func getLog(hostID, jobID string, client *cluster.Client, follow, init bool, stdout, stderr io.Writer) error {
	hostClient, err := client.Host(hostID)
	if err != nil {
		return fmt.Errorf("could not connect to host %s: %s", hostID, err)
	}
	attachReq := &host.AttachReq{
		JobID: jobID,
		Flags: host.AttachFlagStdout | host.AttachFlagStderr | host.AttachFlagLogs,
	}
	if follow {
		attachReq.Flags |= host.AttachFlagStream
	}
	if init {
		attachReq.Flags |= host.AttachFlagInitLog
	}
	attachClient, err := hostClient.Attach(attachReq, false)
	if err != nil {
		switch err {
		case host.ErrJobNotRunning, host.ErrAttached:
			return nil
		case cluster.ErrWouldWait:
			return errors.New("no such job")
		}
		return err
	}
	defer attachClient.Close()
	_, err = attachClient.Receive(stdout, stderr)
	return err
}

type LogLine struct {
	Token   int
	Content []byte
}

type LogRing struct {
	logLines []*LogLine
	start    int
}

func NewLogRing(capacity int) *LogRing {
	return &LogRing{
		logLines: make([]*LogLine, 0, capacity),
	}
}

func (r *LogRing) Add(l *LogLine) {
	if len(r.logLines) < cap(r.logLines) {
		r.logLines = append(r.logLines, l)
	} else {
		r.logLines[r.start] = l
		r.start++

		if r.start == cap(r.logLines) {
			r.start = 0
		}
	}
}

func (r *LogRing) Read() []*LogLine {
	buf := make([]*LogLine, len(r.logLines))
	if n := copy(buf, r.logLines[r.start:len(r.logLines)]); n < len(r.logLines) {
		copy(buf[n:], r.logLines[:r.start])
	}
	return buf
}

func scanLogs(reader io.Reader, token int, output chan LogLine) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		buf := make([]byte, len(scanner.Bytes())+1)
		copy(buf, scanner.Bytes())
		buf[len(buf)-1] = '\n'
		output <- LogLine{Token: token, Content: buf}
	}
	close(output)
}

func tailLogs(stdoutR, stderrR io.Reader, lines int, stdoutW, stderrW io.Writer) {
	gather1 := make(chan LogLine)
	gather2 := make(chan LogLine)
	r := NewLogRing(lines)
	go scanLogs(stdoutR, 0, gather1)
	go scanLogs(stderrR, 1, gather2)
	for {
		select {
		case v, ok := <-gather1:
			if ok {
				r.Add(&v)
			} else {
				gather1 = nil
			}
		case v, ok := <-gather2:
			if ok {
				r.Add(&v)
			} else {
				gather2 = nil
			}
		}
		if gather1 == nil && gather2 == nil {
			break
		}
	}

	for _, ll := range r.Read() {
		switch ll.Token {
		case 0:
			stdoutW.Write(ll.Content)
		case 1:
			stderrW.Write(ll.Content)
		}
	}
}
