package main

import (
	"log"
	"os"
	"os/exec"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/docker-utils/registry"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/pkg/cliutil"
)

func run(cmd *exec.Cmd) {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func export(args *docopt.Args) {
	var manifest map[string]string
	if err := cliutil.DecodeJSONArg(args.String["<manifest>"], &manifest); err != nil {
		log.Fatal(err)
	}

	reg := registry.Registry{Path: args.String["<dir>"]}
	if err := reg.Init(); err != nil {
		log.Fatal(err)
	}

	images := make([]string, 0, len(manifest))
	for name, id := range manifest {
		tagged := name + ":latest"
		run(exec.Command("docker", "tag", "--force", id, tagged))
		images = append(images, tagged)
	}

	cmd := exec.Command("docker", append([]string{"save"}, images...)...)
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	if err := registry.ExtractTarWithoutTarsums(&reg, out); err != nil {
		log.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		log.Fatal(err)
	}
}
