// Don't build to avoid vendoring uniline
// +build ignore

package main

import (
	"os"
	"strings"

	"github.com/flynn/flynn/appliance/postgresql/simulator"
	"github.com/tiborvass/uniline"
)

func main() {
	sim := simulator.New(false, os.Stdout, os.Stdout)
	scanner := uniline.DefaultScanner()
	for scanner.Scan("> ") {
		line := scanner.Text()
		if len(line) == 0 {
			continue
		}

		sim.RunCommand(strings.TrimSpace(line))
		scanner.AddToHistory(line)
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
}
