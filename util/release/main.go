package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/flynn/go-dockerclient"
	"github.com/flynn/go-docopt"
)

var imageIDPattern = regexp.MustCompile(`\$image_id\[[^\]]+\]`)

func interpolateManifest(d *docker.Client, src io.Reader, dest io.Writer) error {
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
			image, err := d.InspectImage(imageName)
			if err != nil {
				panic(fmt.Errorf("Error inspecting %s: %s", imageName, err))
			}
			return []byte(image.ID)
		})
	}()
	if replaceErr != nil {
		return replaceErr.(error)
	}
	_, err = dest.Write(manifest)
	return err
}

func main() {
	usage := `flynn-release generates Flynn releases.

Usage:
  flynn-release manifest [--output=<dest>] <template>

Options:
  -o --output Output destination file ("-" for stdout) [default: -]
`
	args, _ := docopt.Parse(usage, nil, true, "", false)

	dc, err := docker.NewClient("unix:///var/run/docker.sock")
	if err != nil {
		log.Fatal(err)
	}

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

	if err := interpolateManifest(dc, src, dest); err != nil {
		log.Fatal(err)
	}
}
