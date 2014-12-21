package main

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"sync"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/pinkerton"
	"github.com/flynn/flynn/pinkerton/store"
)

var imageNames = []string{
	"etcd",
	"discoverd",
	"postgresql",
	"controller",
	"blobstore",
	"router",
	"receiver",
	"slugbuilder",
	"slugrunner",
	"taffy",
	"dashboard",
}

type ImageManifest struct {
	Images    map[string]string `json:"images"`
	Checksums map[string]string `json:"checksums"`
}

func images(args *docopt.Args) {
	calculateChecksums := args.String["--checksums"] == "true"

	var dest io.Writer = os.Stdout
	if name := args.String["--output"]; name != "-" && name != "" {
		f, err := os.Create(name)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		dest = f
	}

	var err error
	var lookup idLookupFunc
	if file := args.String["--id-file"]; file != "" {
		lookup, err = fileLookupFunc(file)
	} else {
		lookup, err = dockerLookupFunc()
	}
	if err != nil {
		log.Fatal(err)
	}

	manifest := ImageManifest{
		Images:    make(map[string]string, len(imageNames)),
		Checksums: make(map[string]string),
	}

	var ctx *pinkerton.Context
	if calculateChecksums {
		ctx, err = pinkerton.BuildContext(args.String["--driver"], args.String["--root"])
		if err != nil {
			log.Fatal(err)
		}
	}

	var mtx sync.Mutex
	addChecksum := func(img *store.Image) error {
		mtx.Lock()
		if _, ok := manifest.Checksums[img.ID]; ok {
			mtx.Unlock()
			return nil
		} else {
			// set the value to indicate we are calculating this one
			manifest.Checksums[img.ID] = ""
		}
		mtx.Unlock()
		checksum, err := ctx.Checksum(img)
		if err != nil {
			return err
		}
		mtx.Lock()
		manifest.Checksums[img.ID] = checksum
		mtx.Unlock()
		return nil
	}
	ch := make(chan error)
	for _, name := range imageNames {
		id, err := lookup("flynn/" + name)
		if err != nil {
			log.Fatal(err)
		}
		url := args.String["--image-url-prefix"] + "/" + name
		manifest.Images[url] = string(id)

		if calculateChecksums {
			go func() {
				if err := ctx.WalkHistory(string(id), addChecksum); err != nil {
					ch <- err
					return
				}
				ch <- nil
			}()
		}
	}
	if calculateChecksums {
		for range imageNames {
			if err := <-ch; err != nil {
				log.Fatal(err)
			}
		}
	}

	if err := json.NewEncoder(dest).Encode(manifest); err != nil {
		log.Fatal(err)
	}
}
