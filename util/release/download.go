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

func download(args *docopt.Args) {
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

	for image, id := range manifest {
		fmt.Printf("Downloading %s %s...\n", image, id)
		if !strings.HasPrefix(image, "http") {
			image = "https://registry.hub.docker.com/" + image
		}
		image += "?id=" + id
		cmd := exec.Command("pinkerton", "pull", image)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatal(err)
		}
	}
}
