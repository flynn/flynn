package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/flynn/flynn-bootstrap"
)

func readManifest() ([]byte, error) {
	if flag.NArg() == 0 || flag.Arg(0) == "-" {
		return ioutil.ReadAll(os.Stdin)
	}
	return ioutil.ReadFile(flag.Arg(0))
}

var manifest []byte

func main() {
	logJSON := flag.Bool("json", false, "format log output as json")
	minHosts := flag.Int("min-hosts", 1, "minimum number of hosts required to be online")
	flag.Parse()

	log.SetFlags(log.Lmicroseconds)
	logf := textLogger
	if *logJSON {
		logf = jsonLogger
	}

	var err error
	manifest, err = readManifest()
	if err != nil {
		log.Fatalln("Error reading manifest:", err)
	}

	ch := make(chan *bootstrap.StepInfo)
	done := make(chan struct{})
	go func() {
		for si := range ch {
			logf(si)
		}
		close(done)
	}()

	err = bootstrap.Run(manifest, ch, *minHosts)
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
