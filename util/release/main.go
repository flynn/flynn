package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-dockerclient"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
)

var imageIDPattern = regexp.MustCompile(`\$image_id\[[^\]]+\]`)

func interpolateManifest(lookup idLookupFunc, src io.Reader, dest io.Writer) error {
	manifest, err := ioutil.ReadAll(src)
	if err != nil {
		return err
	}
	var replaceErr interface{}
	func() {
		defer func() {
			replaceErr = recover()
		}()
		manifest = imageIDPattern.ReplaceAllFunc(manifest, func(raw []byte) []byte {
			imageName := string(raw[10 : len(raw)-1])
			if !strings.Contains(imageName, "/") {
				imageName = "flynn/" + imageName
			}
			res, err := lookup(imageName)
			if err != nil {
				panic(err)
			}
			return res
		})
	}()
	if replaceErr != nil {
		return replaceErr.(error)
	}
	_, err = dest.Write(manifest)
	return err
}

func dockerLookupFunc() (idLookupFunc, error) {
	d, err := docker.NewClient("unix:///var/run/docker.sock")
	if err != nil {
		return nil, err
	}
	return func(name string) ([]byte, error) {
		image, err := d.InspectImage(name)
		if err != nil {
			return nil, fmt.Errorf("error inspecting %q: %s", name, err)
		}
		return []byte(image.ID), nil
	}, nil
}

func fileLookupFunc(filename string) (idLookupFunc, error) {
	ids := make(map[string]string)
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &ids); err != nil {
		return nil, err
	}
	return func(name string) ([]byte, error) {
		if id, ok := ids[name]; ok {
			return []byte(id), nil
		}
		return nil, fmt.Errorf("unknown image %q", name)
	}, nil
}

type idLookupFunc func(name string) ([]byte, error)

func main() {
	usage := `flynn-release generates Flynn releases.

Usage:
  flynn-release manifest [--output=<dest>] [--id-file=<file>] <template>

Options:
  -o --output=<dest>   Output destination file ("-" for stdout) [default: -]
  -i --id-file=<file>  JSON file containing ID mappings
`
	args, _ := docopt.Parse(usage, nil, true, "", false)

	var dest io.Writer = os.Stdout
	if name := args.String["--output"]; name != "-" && name != "" {
		f, err := os.Create(name)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		dest = f
	}

	var src io.Reader = os.Stdin
	if name := args.String["<template>"]; name != "-" && name != "" {
		f, err := os.Open(name)
		if err != nil {
			log.Fatal(err)
		}
		src = f
	}

	var lookup idLookupFunc
	var err error
	if file := args.String["--id-file"]; file != "" {
		lookup, err = fileLookupFunc(file)
	} else {
		lookup, err = dockerLookupFunc()
	}
	if err != nil {
		log.Fatal(err)
	}

	if err := interpolateManifest(lookup, src, dest); err != nil {
		log.Fatal(err)
	}
}
