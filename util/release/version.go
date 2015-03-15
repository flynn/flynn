package main

import (
	"encoding/json"
	"log"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/util/release/types"
)

func version(args *docopt.Args) {
	manifest := &release.FlynnManifest{}

	if err := json.NewDecoder(os.Stdin).Decode(manifest); err != nil {
		log.Fatal(err)
	}

	if err := manifest.Add(args.String["<version>"], args.String["<commit>"]); err != nil {
		log.Fatal(err)
	}

	if err := json.NewEncoder(os.Stdout).Encode(manifest); err != nil {
		log.Fatal(err)
	}
}
