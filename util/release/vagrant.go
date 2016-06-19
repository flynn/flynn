package main

import (
	"encoding/json"
	"log"
	"os"

	"github.com/flynn/flynn/util/release/types"
	"github.com/flynn/go-docopt"
)

func vagrant(args *docopt.Args) {
	manifest := &release.VagrantManifest{}

	if err := json.NewDecoder(os.Stdin).Decode(manifest); err != nil {
		log.Fatal(err)
	}

	manifest.Add(args.String["<version>"], &release.VagrantProvider{
		Name:         args.String["<provider>"],
		URL:          args.String["<url>"],
		Checksum:     args.String["<checksum>"],
		ChecksumType: "sha256",
	})

	if err := json.NewEncoder(os.Stdout).Encode(manifest); err != nil {
		log.Fatal(err)
	}
}
