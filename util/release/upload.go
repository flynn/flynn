package main

import (
	"fmt"
	"os/exec"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
)

func upload(args *docopt.Args) {
	tag := args.String["<tag>"]
	if tag == "" {
		tag = "latest"
	}
	for image, id := range readManifest(args) {
		tagged := image + ":" + tag

		run(exec.Command("docker", "tag", id, tagged))
		fmt.Println("Tagged", tagged)

		fmt.Printf("Uploading %s...\n", tagged)
		run(exec.Command("docker", "push", tagged))
	}
}
