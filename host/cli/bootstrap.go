package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/bootstrap"
)

func init() {
	Register("bootstrap", runBootstrap, `
usage: flynn-host bootstrap [options] [<manifest>]

Options:
  -n, --min-hosts=MIN  minimum number of hosts required to be online
  -t, --timeout=SECS   seconds to wait for hosts to come online [default: 30]
  --json               format log output as json
  --discovery=TOKEN    use discovery token to connect to cluster
  --peer-ips=IPLIST    use IP address list to connect to cluster

Bootstrap layer 1 using the provided manifest`)
}

func readBootstrapManifest(name string) ([]byte, error) {
	if name == "" || name == "-" {
		return ioutil.ReadAll(os.Stdin)
	}
	return ioutil.ReadFile(name)
}

var manifest []byte

func runBootstrap(args *docopt.Args) {
	log.SetFlags(log.Lmicroseconds)
	logf := textLogger
	if args.Bool["--json"] {
		logf = jsonLogger
	}

	manifestFile := args.String["<manifest>"]
	if manifestFile == "" {
		manifestFile = "/etc/flynn/bootstrap-manifest.json"
	}

	var err error
	manifest, err = readBootstrapManifest(manifestFile)
	if err != nil {
		log.Fatalln("Error reading manifest:", err)
	}

	var minHosts int
	if n := args.String["--min-hosts"]; n != "" {
		if minHosts, err = strconv.Atoi(n); err != nil || minHosts < 1 {
			log.Fatalln("invalid --min-hosts value")
		}
	}

	timeout, err := strconv.Atoi(args.String["--timeout"])
	if err != nil {
		log.Fatalln("invalid --timeout value")
	}

	var ips []string
	if ipList := args.String["--peer-ips"]; ipList != "" {
		ips = strings.Split(ipList, ",")
		if minHosts == 0 {
			minHosts = len(ips)
		}
	}

	if minHosts == 0 {
		minHosts = 1
	}

	ch := make(chan *bootstrap.StepInfo)
	done := make(chan struct{})
	go func() {
		for si := range ch {
			logf(si)
		}
		close(done)
	}()

	err = bootstrap.Run(manifest, ch, args.String["--discovery"], ips, minHosts, timeout)
	<-done
	if err != nil {
		os.Exit(1)
	}
}

func highlightBytePosition(manifest []byte, pos int64) (line, col int, highlight string) {
	// This function a modified version of a function in Camlistore written by Brad Fitzpatrick
	// https://github.com/bradfitz/camlistore/blob/830c6966a11ddb7834a05b6106b2530284a4d036/pkg/errorutil/highlight.go
	line = 1
	var lastLine string
	var currLine bytes.Buffer
	for i := int64(0); i < pos; i++ {
		b := manifest[i]
		if b == '\n' {
			lastLine = currLine.String()
			currLine.Reset()
			line++
			col = 1
		} else {
			col++
			currLine.WriteByte(b)
		}
	}
	if line > 1 {
		highlight += fmt.Sprintf("%5d: %s\n", line-1, lastLine)
	}
	highlight += fmt.Sprintf("%5d: %s\n", line, currLine.String())
	highlight += fmt.Sprintf("%s^\n", strings.Repeat(" ", col+5))
	return
}

func textLogger(si *bootstrap.StepInfo) {
	switch si.State {
	case "start":
		log.Printf("%s %s", si.Action, si.ID)
	case "done":
		if s, ok := si.StepData.(fmt.Stringer); ok {
			log.Printf("%s %s %s", si.Action, si.ID, s)
		}
	case "error":
		if serr, ok := si.Err.(*json.SyntaxError); ok {
			line, col, highlight := highlightBytePosition(manifest, serr.Offset)
			fmt.Printf("Error parsing JSON: %s\nAt line %d, column %d (offset %d):\n%s", si.Err, line, col, serr.Offset, highlight)
			return
		}
		log.Printf("%s %s error: %s", si.Action, si.ID, si.Error)
	}
}

func jsonLogger(si *bootstrap.StepInfo) {
	json.NewEncoder(os.Stdout).Encode(si)
}
