package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"

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
	if err := os.MkdirAll(args.String["--root"], 0755); err != nil {
		log.Fatalf("error creating root dir: %s", err)
	}
	for image, id := range readManifest(args) {
		fmt.Printf("Downloading %s %s...\n", image, id)
		image += "?id=" + id
		run(exec.Command("pinkerton", "pull", "--root", args.String["--root"], "--driver", args.String["--driver"], image))
	}
}
