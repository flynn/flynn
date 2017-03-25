package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/Sirupsen/logrus"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/docker-receive/utils"
	"github.com/flynn/flynn/pinkerton"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/imagebuilder"
)

func main() {
	log.SetFlags(0)
	logrus.SetLevel(logrus.ErrorLevel)

	if len(os.Args) != 2 {
		log.Fatalf("usage: %s URL", os.Args[0])
	}
	if err := run(os.Args[1]); err != nil {
		log.Fatalln("ERROR:", err)
	}
}

func run(url string) error {
	client, err := controller.NewClient("", os.Getenv("CONTROLLER_KEY"))
	if err != nil {
		return err
	}

	context, err := pinkerton.BuildContext("flynn", "/tmp/docker")
	if err != nil {
		return err
	}

	builder := &imagebuilder.Builder{
		Store:   &layerStore{},
		Context: context,
	}

	// pull the docker image
	ref, err := pinkerton.NewRef(url)
	if err != nil {
		return err
	}
	if _, err := context.PullDocker(url, ioutil.Discard); err != nil {
		return err
	}

	// create squashfs for each layer
	image, err := builder.Build(ref.DockerRef(), false)
	if err != nil {
		return err
	}

	// add the app name to the manifest to change its resulting ID so
	// pushing the same image to multiple apps leads to different artifacts
	// in the controller (and hence distinct artifact events)
	if image.Meta == nil {
		image.Meta = make(map[string]string, 1)
	}
	image.Meta["docker-receive.repository"] = ref.Name()

	// upload manifest to blobstore
	rawManifest := image.RawManifest()
	imageURL := fmt.Sprintf("http://blobstore.discoverd/docker-receive/images/%s.json", image.ID())
	if err := upload(bytes.NewReader(rawManifest), imageURL); err != nil {
		return err
	}

	// create the artifact
	artifact := &ct.Artifact{
		ID:   os.Getenv("ARTIFACT_ID"),
		Type: ct.ArtifactTypeFlynn,
		URI:  imageURL,
		Meta: map[string]string{
			"blobstore":                 "true",
			"docker-receive.uri":        url,
			"docker-receive.repository": ref.Name(),
			"docker-receive.digest":     ref.ID(),
		},
		RawManifest:      rawManifest,
		Hashes:           image.Hashes(),
		Size:             int64(len(rawManifest)),
		LayerURLTemplate: utils.LayerURLTemplate,
	}
	return client.CreateArtifact(artifact)
}

func upload(data io.Reader, url string) error {
	req, err := http.NewRequest("PUT", url, data)
	if err != nil {
		return err
	}
	res, err := httphelper.RetryClient.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status: %s", res.Status)
	}
	return nil
}

type layerStore struct{}

func (l *layerStore) Load(id string) (*ct.ImageLayer, error) {
	res, err := http.Get(utils.ConfigURL(id))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		// the layer doesn't exist, just return nil
		// so that the imagebuilder builds it
		return nil, nil
	} else if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status: %s", res.Status)
	}
	var layer ct.ImageLayer
	return &layer, json.NewDecoder(res.Body).Decode(&layer)
}

func (l *layerStore) Save(id, path string, layer *ct.ImageLayer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := upload(f, utils.LayerURL(layer)); err != nil {
		return err
	}
	data, err := json.Marshal(layer)
	if err != nil {
		return err
	}
	return upload(bytes.NewReader(data), utils.ConfigURL(id))
}
