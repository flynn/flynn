package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
)

func readManifest(args *docopt.Args) map[string]string {
	var src io.Reader = os.Stdin
	if name := args.String["<manifest>"]; name != "-" && name != "" {
		f, err := os.Open(name)
		if err != nil {
			log.Fatal(err)
		}
		src = f
	}

	var manifest map[string]string
	if err := json.NewDecoder(src).Decode(&manifest); err != nil {
		log.Fatal(err)
	}
	return manifest
}

func run(cmd *exec.Cmd) {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func download(args *docopt.Args) {
	for image, id := range readManifest(args) {
		fmt.Printf("Downloading %s %s...\n", image, id)
		if !strings.HasPrefix(image, "http") {
			image = "https://registry.hub.docker.com/" + image
		}
		image += "?id=" + id
		run(exec.Command("pinkerton", "pull", image))
	}
}
