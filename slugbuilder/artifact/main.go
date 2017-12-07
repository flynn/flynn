package main

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/docker/go-units"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	hh "github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/typeconv"
	"gopkg.in/yaml.v2"
)

func main() {
	dir := flag.String("dir", "", "slug parent directory")
	uid := flag.Int("uid", -1, "UID")
	gid := flag.Int("gid", -1, "GID")
	flag.Parse()

	if *dir == "" {
		fmt.Fprintf(os.Stderr, "missing --dir")
		os.Exit(1)
	}
	if *uid < 0 {
		fmt.Fprintf(os.Stderr, "missing --uid")
		os.Exit(1)
	}
	if *gid < 0 {
		fmt.Fprintf(os.Stderr, "missing --gid")
		os.Exit(1)
	}

	if err := run(*dir, *uid, *gid); err != nil {
		log.Fatalln("ERROR:", "could not create slug image:", err)
	}
}

func run(dir string, uid, gid int) error {
	client, err := controller.NewClient("", os.Getenv("CONTROLLER_KEY"))
	if err != nil {
		return err
	}

	// create a squashfs layer
	layer, err := ioutil.TempFile("", "squashfs-")
	if err != nil {
		return err
	}
	defer os.Remove(layer.Name())
	defer layer.Close()

	if out, err := exec.Command("mksquashfs", dir, layer.Name(), "-noappend").CombinedOutput(); err != nil {
		return fmt.Errorf("mksquashfs error: %s: %s", err, out)
	}

	h := sha512.New512_256()
	length, err := io.Copy(h, layer)
	if err != nil {
		return err
	}
	layerSHA := hex.EncodeToString(h.Sum(nil))

	// upload the layer to the blobstore
	if _, err := layer.Seek(0, io.SeekStart); err != nil {
		return err
	}
	layerURL := fmt.Sprintf("http://blobstore.discoverd/slugs/layers/%s.squashfs", layerSHA)
	if err := upload(layer, layerURL); err != nil {
		return err
	}

	manifest := &ct.ImageManifest{
		Type: ct.ImageManifestTypeV1,

		Entrypoints: map[string]*ct.ImageEntrypoint{
			"_default": {
				Env: map[string]string{
					"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
					"TERM": "xterm",
					"HOME": "/app",
				},
				WorkingDir: "/app",
				Args:       []string{"/runner/init", "bash"},
				Uid:        typeconv.Uint32Ptr(uint32(uid)),
				Gid:        typeconv.Uint32Ptr(uint32(gid)),
			},
		},

		Rootfs: []*ct.ImageRootfs{{
			Platform: ct.DefaultImagePlatform,
			Layers: []*ct.ImageLayer{{
				ID:     layerSHA,
				Type:   ct.ImageLayerTypeSquashfs,
				Length: length,
				Hashes: map[string]string{"sha512_256": layerSHA},
			}},
		}},
	}

	rawManifest := manifest.RawManifest()
	manifestURL := fmt.Sprintf("http://blobstore.discoverd/slugs/images/%s.json", manifest.ID())
	if err := upload(bytes.NewReader(rawManifest), manifestURL); err != nil {
		return err
	}

	processTypes := determineProcessTypes(dir)

	artifact := &ct.Artifact{
		ID:   os.Getenv("SLUG_IMAGE_ID"),
		Type: ct.ArtifactTypeFlynn,
		URI:  manifestURL,
		Meta: map[string]string{
			"blobstore":                 "true",
			"slugbuilder.process_types": strings.Join(processTypes, ","),
		},
		RawManifest:      rawManifest,
		Hashes:           manifest.Hashes(),
		Size:             int64(len(rawManifest)),
		LayerURLTemplate: "http://blobstore.discoverd/slugs/layers/{id}.squashfs",
	}

	// create artifact
	if err := client.CreateArtifact(artifact); err != nil {
		return err
	}

	fmt.Printf("-----> Compiled slug size is %s\n", units.BytesSize(float64(length)))
	return nil
}

func upload(data io.Reader, url string) error {
	req, err := http.NewRequest("PUT", url, data)
	if err != nil {
		return err
	}
	res, err := hh.RetryClient.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status: %s", res.Status)
	}
	return nil
}

func determineProcessTypes(dir string) []string {
	types := loadProcfileTypes(dir)
	if len(types) == 0 {
		types = loadDefaultTypes(dir)
	}
	return types
}

func loadProcfileTypes(dir string) []string {
	data, err := ioutil.ReadFile(filepath.Join(dir, "app", "Procfile"))
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "WARN: error reading Procfile:", err)
		}
		return nil
	}
	var procfile map[string]string
	if err := yaml.Unmarshal(data, &procfile); err != nil {
		fmt.Fprintln(os.Stderr, "WARN: error parsing Procfile:", err)
		return nil
	}
	types := make([]string, 0, len(procfile))
	for typ := range procfile {
		types = append(types, typ)
	}
	return types
}

func loadDefaultTypes(dir string) []string {
	data, err := ioutil.ReadFile(filepath.Join(dir, "app", ".release"))
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "WARN: error reading .release:", err)
		}
		return nil
	}
	var release struct {
		DefaultProcessTypes map[string]string `yaml:"default_process_types"`
	}
	if err := yaml.Unmarshal(data, &release); err != nil {
		fmt.Fprintln(os.Stderr, "WARN: error parsing .release:", err)
		return nil
	}
	types := make([]string, 0, len(release.DefaultProcessTypes))
	for typ := range release.DefaultProcessTypes {
		types = append(types, typ)
	}
	return types
}
