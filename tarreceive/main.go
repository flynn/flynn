package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/flynn/flynn/controller/authorizer"
	controller "github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/archive"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/status"
	"github.com/flynn/flynn/tarreceive/utils"
	"github.com/julienschmidt/httprouter"
)

func main() {
	controllerKey := os.Getenv("CONTROLLER_KEY")
	if controllerKey == "" {
		log.Fatal("missing CONTROLLER_KEY env var")
	}
	client, err := controller.NewClient("", controllerKey)
	if err != nil {
		log.Fatal(err)
	}

	authKey := os.Getenv("AUTH_KEY")
	if authKey == "" {
		log.Fatal("missing AUTH_KEY env var")
	}
	tokenKey, err := authorizer.ParseTokenKey(os.Getenv("ACCESS_TOKEN_KEY"))
	if err != nil {
		log.Fatalln("error decoding ACCESS_TOKEN_KEY:", err)
	}
	tokenMaxValidity, err := authorizer.ParseTokenMaxValidity(os.Getenv("ACCESS_TOKEN_MAX_VALIDITY"))
	if err != nil {
		log.Fatalln("error parsing ACCESS_TOKEN_MAX_VALIDITY:", err)
	}
	auth := authorizer.New([]string{authKey}, nil, tokenKey, tokenMaxValidity)

	srv := newServer(auth, client)

	handler := httphelper.ContextInjector(
		"tarreceive",
		httphelper.NewRequestLogger(srv),
	)

	log.Fatal(http.ListenAndServe(":"+os.Getenv("PORT"), handler))
}

type server struct {
	router *httprouter.Router
	auth   *authorizer.Authorizer
	client controller.Client
}

func newServer(auth *authorizer.Authorizer, client controller.Client) *server {
	s := &server{
		router: httprouter.New(),
		auth:   auth,
		client: client,
	}
	s.router.GET("/layer/:id", s.handleGetLayer)
	s.router.POST("/layer/:id", s.handleCreateLayer)
	s.router.POST("/artifact", s.handleCreateArtifact)
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == status.Path {
		status.HealthyHandler.ServeHTTP(w, r)
		return
	}
	if _, err := s.auth.AuthorizeRequest(r); err != nil {
		w.WriteHeader(401)
		return
	}
	s.router.ServeHTTP(w, r)
}

func (s *server) handleGetLayer(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	id := p.ByName("id")
	res, err := httphelper.RetryClient.Get(utils.ConfigURL(id))
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		http.NotFound(w, r)
		return
	} else if res.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(res.Body)
		httphelper.Error(w, fmt.Errorf("unexpected blobstore response: %s: %s", res.Status, body))
		return
	}
	var layer ct.ImageLayer
	if err := json.NewDecoder(res.Body).Decode(&layer); err != nil {
		httphelper.Error(w, err)
		return
	}
	httphelper.JSON(w, http.StatusOK, &layer)
}

func (s *server) handleCreateLayer(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	id := p.ByName("id")

	layer, err := func() (*ct.ImageLayer, error) {
		// copy the tar to a temp file and generate its SHA256 hash
		tmpDir, err := ioutil.TempDir("", "")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmpDir)
		tarFile, err := os.Create(filepath.Join(tmpDir, "layer.tar"))
		if err != nil {
			return nil, err
		}
		defer tarFile.Close()
		tarHash := sha256.New()
		if _, err := io.Copy(io.MultiWriter(tarFile, tarHash), r.Body); err != nil {
			return nil, err
		}

		// check the SHA256 hash
		if hex.EncodeToString(tarHash.Sum(nil)) != id {
			return nil, errors.New("SHA256 mismatch")
		}

		// extract the tar
		if _, err := tarFile.Seek(0, os.SEEK_SET); err != nil {
			return nil, err
		}
		extractDir := filepath.Join(tmpDir, "extract")
		if err := os.MkdirAll(extractDir, 0755); err != nil {
			return nil, err
		}
		if err := archive.Unpack(tarFile, extractDir, true); err != nil {
			return nil, err
		}

		// create squashfs layer
		layerPath := filepath.Join(tmpDir, "layer.squashfs")
		if out, err := exec.Command("mksquashfs", extractDir, layerPath, "-noappend").CombinedOutput(); err != nil {
			return nil, fmt.Errorf("mksquashfs error: %s: %s", err, out)
		}

		// generate squashfs layer SHA
		layerFile, err := os.Open(layerPath)
		if err != nil {
			return nil, err
		}
		defer layerFile.Close()
		layerHash := sha512.New512_256()
		layerSize, err := io.Copy(layerHash, layerFile)
		if err != nil {
			return nil, err
		}
		layerSha := hex.EncodeToString(layerHash.Sum(nil))

		// upload squashfs layer
		if _, err := layerFile.Seek(0, os.SEEK_SET); err != nil {
			return nil, err
		}
		if err := upload(layerFile, utils.LayerURL(id)); err != nil {
			return nil, err
		}

		// upload layer JSON
		layer := &ct.ImageLayer{
			ID:     id,
			Type:   ct.ImageLayerTypeSquashfs,
			Length: layerSize,
			Hashes: map[string]string{"sha512_256": layerSha},
			Meta:   map[string]string{"tar.layer_id": id},
		}
		layerData, err := json.Marshal(layer)
		if err != nil {
			return nil, err
		}
		if err := upload(bytes.NewReader(layerData), utils.ConfigURL(id)); err != nil {
			return nil, err
		}

		return layer, nil
	}()
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	httphelper.JSON(w, http.StatusOK, layer)
}

func (s *server) handleCreateArtifact(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	var image ct.ImageManifest
	if err := json.NewDecoder(r.Body).Decode(&image); err != nil {
		httphelper.Error(w, err)
		return
	}
	rawManifest := image.RawManifest()
	imageURL := fmt.Sprintf("http://blobstore.discoverd/tarreceive/images/%s.json", image.ID())
	if err := upload(bytes.NewReader(rawManifest), imageURL); err != nil {
		httphelper.Error(w, err)
		return
	}
	artifact := &ct.Artifact{
		Type:             ct.ArtifactTypeFlynn,
		URI:              imageURL,
		Meta:             map[string]string{"blobstore": "true"},
		RawManifest:      rawManifest,
		Hashes:           image.Hashes(),
		Size:             int64(len(rawManifest)),
		LayerURLTemplate: utils.LayerURLTemplate,
	}
	if err := s.client.CreateArtifact(artifact); err != nil {
		httphelper.Error(w, err)
		return
	}
	httphelper.JSON(w, http.StatusOK, artifact)
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
