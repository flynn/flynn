package pinkerton

import (
	"io/ioutil"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Sirupsen/logrus"
	"github.com/docker/distribution/configuration"
	"github.com/docker/distribution/context"
	"github.com/docker/distribution/registry/handlers"
	_ "github.com/docker/distribution/registry/storage/driver/filesystem"
	"github.com/flynn/flynn/pkg/random"
)

const (
	testImageDigest = "sha256:e3d939e790435a08a456e064203d1c8688342be817dc9f6a73f1d7610540122f"
	testImageID     = "c302bbd9e1b808906d73e7d07005dd779acf16c32b7a8fbd8f5c4570fbae0696"
	testImageData   = "foo\n"
)

func TestDocker(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("pinkerton: must be root to create AUFS mounts")
	}

	// start Docker registry using test files
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(cwd, "test", "files")
	config := configuration.Configuration{
		Storage: configuration.Storage{
			"filesystem": configuration.Parameters{
				"rootdirectory": root,
			},
		},
	}
	logrus.SetLevel(logrus.ErrorLevel)
	app := handlers.NewApp(context.Background(), config)
	srv := httptest.NewServer(app)
	defer srv.Close()

	// create context
	tmp, err := ioutil.TempDir("", "pinkerton-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)
	ctx, err := BuildContext("aufs", tmp)
	if err != nil {
		t.Fatal(err)
	}

	// pull image using digest
	imageID, err := ctx.PullDocker(srv.URL+"?name=pinkerton-test&id="+testImageDigest, ioutil.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if imageID != testImageID {
		t.Fatalf("expected image to have ID %q, got %q", testImageID, imageID)
	}

	// checkout image
	name := random.String(8)
	path, err := ctx.Checkout(name, imageID)
	if err != nil {
		t.Fatal(err)
	}
	defer ctx.Cleanup(name)

	// check foo.txt exists and has correct data
	f, err := os.Open(filepath.Join(path, "foo.txt"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := ioutil.ReadAll(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(data, []byte(testImageData)) {
		t.Fatalf("expected foo.txt to contain %q, got %q", testImageData, string(data))
	}
}
