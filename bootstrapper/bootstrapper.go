package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/flynn/flynn-bootstrap"
)

func manifest() ([]byte, error) {
	if flag.NArg() == 0 || flag.Arg(0) == "-" {
		return ioutil.ReadAll(os.Stdin)
	}
	return ioutil.ReadFile(flag.Arg(0))
}

func main() {
	logJSON := flag.Bool("json", false, "format log output as json")
	flag.Parse()

	log.SetFlags(log.Lmicroseconds)
	logf := textLogger
	if *logJSON {
		logf = jsonLogger
	}

	manifest, err := manifest()
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

	err = bootstrap.Run(manifest, ch)
	<-done
	if err != nil {
		os.Exit(1)
	}
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
		log.Printf("%s %s error: %s", si.Action, si.ID, si.Error)
	}
}

func jsonLogger(si *bootstrap.StepInfo) {
	json.NewEncoder(os.Stdout).Encode(si)
}
