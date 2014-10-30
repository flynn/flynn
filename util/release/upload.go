package main

import (
	"fmt"
	"log"
	"net/url"
	"os/exec"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
)

var defaultRegistry = "registry.hub.docker.com"

func upload(args *docopt.Args) {
	tag := args.String["<tag>"]
	if tag == "" {
		tag = "latest"
	}
	for image, id := range readManifest(args) {
		u, err := url.Parse(image)
		if err != nil {
			log.Fatal(err)
		}

		var tagged string
		if u.Host == defaultRegistry {
			tagged = u.Path[1:] + ":" + tag
		} else {
			tagged = u.Host + u.Path + ":" + tag
		}

		run(exec.Command("docker", "tag", id, tagged))
		fmt.Println("Tagged", tagged)

		fmt.Printf("Uploading %s...\n", tagged)
		run(exec.Command("docker", "push", tagged))
	}
}
