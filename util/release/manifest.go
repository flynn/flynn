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

			manifest, err := ioutil.ReadFile(filepath.Join(imageDir, name+".json"))
			if err != nil {
				panic(err)
			}

			artifact := &ct.Artifact{
				Type:        ct.ArtifactTypeFlynn,
				RawManifest: manifest,
				Size:        int64(len(manifest)),
				Meta: map[string]string{
					"flynn.component":    name,
					"flynn.system-image": "true",
				},
			}
			artifact.URI = fmt.Sprintf("%s?target=/%s/images/%s.json", imageRepository, version.String(), artifact.Manifest().ID())
			artifact.Hashes = artifact.Manifest().Hashes()
			if version.Dev() {
				artifact.LayerURLTemplate = "file:///var/lib/flynn/layer-cache/{id}.squashfs"
			} else {
				artifact.LayerURLTemplate = fmt.Sprintf("%s?target=/%s/layers/{id}.squashfs", imageRepository, version.String())
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
