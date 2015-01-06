package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/pkg/cliutil"
)

var defaultRegistry = "registry.hub.docker.com"

func run(cmd *exec.Cmd) {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func upload(args *docopt.Args) {
	tag := args.String["<tag>"]
	if tag == "" {
		tag = "latest"
	}

	var manifest map[string]string
	if err := cliutil.DecodeJSONArg(args.String["<manifest>"], &manifest); err != nil {
		log.Fatal(err)
	}

	for image, id := range manifest {
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

		run(exec.Command("docker", "tag", "--force", id, tagged))
		fmt.Println("Tagged", tagged)

		fmt.Printf("Uploading %s...\n", tagged)
		run(exec.Command("docker", "push", tagged))
	}
}
