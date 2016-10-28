package pinkerton

import (
	"io/ioutil"
	"net/http/httptest"
	"os"
	"os/exec"
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
	testImageID     = "sha256:4cd1305ddefa8e7660f501af8d6c8024765720d3bcb7e160e1bad5497f2735ce"
	testImageData   = "foo\n"
)

func TestDocker(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("pinkerton: must be root to create AUFS mounts")
	}

	// extract the registry files
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp, err := ioutil.TempDir("", "pinkerton-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)
	cmd := exec.Command("tar", "xf", filepath.Join(cwd, "test", "files.tar"), "-C", tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("error extracting registry files: %s: %s", out, err)
	}

	// start Docker registry using test files
	config := &configuration.Configuration{
		Storage: configuration.Storage{
			"filesystem": configuration.Parameters{
				"rootdirectory": tmp,
			},
		},
	}
	logrus.SetLevel(logrus.ErrorLevel)
	app := handlers.NewApp(context.Background(), config)
	srv := httptest.NewServer(app)
	defer srv.Close()

	// create context
	ctx, err := BuildContext("aufs", tmp)
	if err != nil {
		t.Fatal(err)
	}

	// pull image using digest
	img, err := ctx.PullDocker(srv.URL+"?name=pinkerton-test&id="+testImageDigest, NopProgress)
	if err != nil {
		t.Fatal(err)
	}
	if img.ID().String() != testImageID {
		t.Fatalf("expected image to have ID %q, got %q", testImageID, img.ID())
	}

	// checkout image
	name := random.String(8)
	path, err := ctx.Checkout(name, img.ID())
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
