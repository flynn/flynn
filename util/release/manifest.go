package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/version"
	"github.com/flynn/go-docopt"
)

func manifest(args *docopt.Args) {
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

	if err := interpolateManifest(args.String["--image-dir"], args.String["--image-repository"], src, dest); err != nil {
		log.Fatal(err)
	}
}

var imageArtifactPattern = regexp.MustCompile(`\$image_artifact\[[^\]]+\]`)

func interpolateManifest(imageDir, imageRepository string, src io.Reader, dest io.Writer) error {
	manifest, err := ioutil.ReadAll(src)
	if err != nil {
		return err
	}
	var replaceErr interface{}
	func() {
		defer func() {
			replaceErr = recover()
		}()
		manifest = imageArtifactPattern.ReplaceAllFunc(manifest, func(raw []byte) []byte {
			name := string(raw[16 : len(raw)-1])

			f, err := os.Open(filepath.Join(imageDir, name+".json"))
			if err != nil {
				panic(err)
			}
			defer f.Close()

			image := &ct.ImageManifest{}
			if err := json.NewDecoder(f).Decode(image); err != nil {
				panic(err)
			}

			artifact := &ct.Artifact{
				Type:             ct.ArtifactTypeFlynn,
				URI:              fmt.Sprintf("%s?target=/%s/images/%s.json", imageRepository, version.String(), name),
				Manifest:         image,
				LayerURLTemplate: "file:///var/lib/flynn/layer-cache/{id}.squashfs",
			}
			data, err := json.Marshal(artifact)
			if err != nil {
				panic(err)
			}
			return data
		})
	}()
	if replaceErr != nil {
		return replaceErr.(error)
	}
	_, err = dest.Write(manifest)
	return err
}
