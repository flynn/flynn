package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/template"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/random"
	tc "github.com/flynn/flynn/test/cluster"
	"github.com/flynn/flynn/updater/types"
	c "github.com/flynn/go-check"
)

type ReleaseSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&ReleaseSuite{})

func (s *ReleaseSuite) addReleaseHosts(t *c.C) *tc.BootResult {
	res, err := testCluster.AddReleaseHosts()
	t.Assert(err, c.IsNil)
	t.Assert(res.Instances, c.HasLen, 4)
	return res
}

var releaseScript = template.Must(template.New("release-script").Delims("[[", "]]").Parse(`
export TUF_TARGETS_PASSPHRASE="flynn-test"
export TUF_SNAPSHOT_PASSPHRASE="flynn-test"
export TUF_TIMESTAMP_PASSPHRASE="flynn-test"

export GOPATH=~/go
src="${GOPATH}/src/github.com/flynn/flynn"

# send all output to stderr so only images.json is output to stdout
(

  # rebuild components.
  #
  # ideally we would use tup to do this, but it hangs waiting on the
  # FUSE socket after building, so for now we do it manually.
  #
  # See https://github.com/flynn/flynn/issues/949
  pushd "${src}" >/dev/null
  sed "s/{{TUF-ROOT-KEYS}}/$(tuf --dir test/release root-keys)/g" host/cli/root_keys.go.tmpl > host/cli/root_keys.go
  vpkg="github.com/flynn/flynn/pkg/version"
  ldflags="-X ${vpkg}.commit=notdev -X ${vpkg}.branch=dev -X ${vpkg}.tag=v20161108.0-test -X ${vpkg}.dirty=false"
  go build -o host/bin/flynn-host -ldflags="${ldflags}" ./host
  gzip -9 --keep --force host/bin/flynn-host
  sed "s/{{FLYNN-HOST-CHECKSUM}}/$(sha512sum host/bin/flynn-host.gz | cut -d " " -f 1)/g" script/install-flynn.tmpl > script/install-flynn

  # create new image manifests by adding some metadata
  for name in $(jq -r 'keys | .[]' images.json); do
    jq ".[\"${name}\"].manifest + {meta: {foo: \"bar\"}}" images.json > "image/bootstrapped/${name}.json"
  done
  go build -o util/release/flynn-release -ldflags="${ldflags}" ./util/release
  util/release/flynn-release manifest --image-dir "${src}/image/bootstrapped" util/release/images_template.json > images.json
  popd >/dev/null

  "${src}/script/export-components" "${src}/test/release"
  "${src}/script/release-channel" --tuf-dir "${src}/test/release" --no-sync --no-changelog "stable" "v20161108.0-test"

  dir=$(mktemp --directory)
  ln -s "${src}/test/release/repository" "${dir}/tuf"
  ln -s "${src}/script/install-flynn" "${dir}/install-flynn"

  # create a slug for testing slug based app updates
  tar c -C "${src}/test/apps/http" . | docker run -i -a stdin -a stdout -a stderr --dns "$(ip addr show flynnbr0 | grep -oP '100\.100\.\d+\.\d+')" -e CONTROLLER_KEY="[[ .ControllerKey ]]" -e SLUG_IMAGE_ID="[[ .SlugImageID ]]" flynn/slugbuilder

  # start a file server to serve the exported components
  sudo start-stop-daemon \
    --start \
    --background \
    --chdir "${dir}" \
    --exec "${src}/test/image/bin/flynn-test-file-server"
) >&2

cat "${src}/images.json"
`))

var installScript = template.Must(template.New("install-script").Parse(`
# download to a tmp file so the script fails on download error rather than
# executing nothing and succeeding
curl -sL --fail http://{{ .Blobstore }}/install-flynn > /tmp/install-flynn
bash -e /tmp/install-flynn -r "http://{{ .Blobstore }}"
`))

var updateScript = template.Must(template.New("update-script").Parse(`
timeout --signal=QUIT --kill-after=10 10m bash -ex <<-SCRIPT
cd ~/go/src/github.com/flynn/flynn
tuf --dir test/release root-keys | tuf-client init --store /tmp/tuf.db http://{{ .Blobstore }}/tuf
echo stable | sudo tee /etc/flynn/channel.txt
export DISCOVERD="{{ .Discoverd }}"
flynn-host update --repository http://{{ .Blobstore }}/tuf --tuf-db /tmp/tuf.db
SCRIPT
`))

func (s *ReleaseSuite) TestReleaseImages(t *c.C) {
	if testCluster == nil {
		t.Skip("cannot boot release cluster")
	}

	// stream script output to t.Log
	logWriter := debugLogWriter(t)

	// boot the release cluster, release components to a blobstore and output the new images.json
	releaseCluster := s.addReleaseHosts(t)
	buildHost := releaseCluster.Instances[0]
	var imagesJSON bytes.Buffer
	var script bytes.Buffer
	slugImageID := random.UUID()
	releaseScript.Execute(&script, struct{ ControllerKey, SlugImageID string }{releaseCluster.ControllerKey, slugImageID})
	t.Assert(buildHost.Run("bash -ex", &tc.Streams{Stdin: &script, Stdout: &imagesJSON, Stderr: logWriter}), c.IsNil)
	var images map[string]*ct.Artifact
	t.Assert(json.Unmarshal(imagesJSON.Bytes(), &images), c.IsNil)

	// install Flynn from the blobstore on the vanilla host
	blobstoreAddr := buildHost.IP + ":8080"
	installHost := releaseCluster.Instances[3]
	script.Reset()
	installScript.Execute(&script, map[string]string{"Blobstore": blobstoreAddr})
	var installOutput bytes.Buffer
	out := io.MultiWriter(logWriter, &installOutput)
	t.Assert(installHost.Run("sudo bash -ex", &tc.Streams{Stdin: &script, Stdout: out, Stderr: out}), c.IsNil)

	// check the flynn-host version is correct
	var hostVersion bytes.Buffer
	t.Assert(installHost.Run("flynn-host version", &tc.Streams{Stdout: &hostVersion}), c.IsNil)
	t.Assert(strings.TrimSpace(hostVersion.String()), c.Equals, "v20161108.0-test")

	// check rebuilt images were downloaded
	assertInstallOutput := func(format string, v ...interface{}) {
		expected := fmt.Sprintf(format, v...)
		if !strings.Contains(installOutput.String(), expected) {
			t.Fatalf(`expected install to output %q`, expected)
		}
	}
	for name, image := range images {
		assertInstallOutput("pulling %s image", name)
		for _, layer := range image.Manifest().Rootfs[0].Layers {
			assertInstallOutput("pulling %s layer %s", name, layer.ID)
		}
	}

	// installing on an instance with Flynn running should fail
	script.Reset()
	installScript.Execute(&script, map[string]string{"Blobstore": blobstoreAddr})
	installOutput.Reset()
	err := buildHost.Run("sudo bash -ex", &tc.Streams{Stdin: &script, Stdout: out, Stderr: out})
	if err == nil || !strings.Contains(installOutput.String(), "ERROR: Flynn is already installed.") {
		t.Fatal("expected Flynn install to fail but it didn't")
	}

	// create a controller client for the release cluster
	pin, err := base64.StdEncoding.DecodeString(releaseCluster.ControllerPin)
	t.Assert(err, c.IsNil)
	client, err := controller.NewClientWithConfig(
		"https://"+buildHost.IP,
		releaseCluster.ControllerKey,
		controller.Config{Pin: pin, Domain: releaseCluster.ControllerDomain},
	)
	t.Assert(err, c.IsNil)

	// deploy a slug based app + Redis resource
	slugApp := &ct.App{}
	t.Assert(client.CreateApp(slugApp), c.IsNil)
	gitreceive, err := client.GetAppRelease("gitreceive")
	t.Assert(err, c.IsNil)
	imageArtifact, err := client.GetArtifact(gitreceive.Env["SLUGRUNNER_IMAGE_ID"])
	t.Assert(err, c.IsNil)
	slugArtifact, err := client.GetArtifact(slugImageID)
	t.Assert(err, c.IsNil)
	resource, err := client.ProvisionResource(&ct.ResourceReq{ProviderID: "redis", Apps: []string{slugApp.ID}})
	t.Assert(err, c.IsNil)
	release := &ct.Release{
		ArtifactIDs: []string{imageArtifact.ID, slugArtifact.ID},
		Processes:   map[string]ct.ProcessType{"web": {Args: []string{"/runner/init", "bin/http"}}},
		Meta:        map[string]string{"git": "true"},
		Env:         resource.Env,
	}
	t.Assert(client.CreateRelease(release), c.IsNil)
	t.Assert(client.SetAppRelease(slugApp.ID, release.ID), c.IsNil)
	watcher, err := client.WatchJobEvents(slugApp.ID, release.ID)
	t.Assert(err, c.IsNil)
	defer watcher.Close()
	t.Assert(client.PutFormation(&ct.Formation{
		AppID:     slugApp.ID,
		ReleaseID: release.ID,
		Processes: map[string]int{"web": 1},
	}), c.IsNil)
	err = watcher.WaitFor(ct.JobEvents{"web": {ct.JobStateUp: 1}}, scaleTimeout, nil)
	t.Assert(err, c.IsNil)

	// run a cluster update from the blobstore
	updateHost := releaseCluster.Instances[1]
	script.Reset()
	updateScript.Execute(&script, map[string]string{"Blobstore": blobstoreAddr, "Discoverd": updateHost.IP + ":1111"})
	var updateOutput bytes.Buffer
	out = io.MultiWriter(logWriter, &updateOutput)
	t.Assert(updateHost.Run("bash -ex", &tc.Streams{Stdin: &script, Stdout: out, Stderr: out}), c.IsNil)

	// check rebuilt images were downloaded
	for name := range images {
		for _, host := range releaseCluster.Instances[0:2] {
			expected := fmt.Sprintf(`"pulling %s image" host=%s`, name, host.ID)
			if !strings.Contains(updateOutput.String(), expected) {
				t.Fatalf(`expected update to download %s on host %s`, name, host.ID)
			}
		}
	}

	assertImage := func(uri, image string) {
		t.Assert(uri, c.Equals, images[image].URI)
	}

	// check system apps were deployed correctly
	for _, app := range updater.SystemApps {
		if app.ImageOnly {
			continue // we don't deploy ImageOnly updates
		}
		debugf(t, "checking new %s release is using image %s", app.Name, images[app.Name].URI)
		expected := fmt.Sprintf(`"finished deploy of system app" name=%s`, app.Name)
		if !strings.Contains(updateOutput.String(), expected) {
			t.Fatalf(`expected update to deploy %s`, app.Name)
		}
		release, err := client.GetAppRelease(app.Name)
		t.Assert(err, c.IsNil)
		debugf(t, "new %s release ID: %s", app.Name, release.ID)
		artifact, err := client.GetArtifact(release.ArtifactIDs[0])
		t.Assert(err, c.IsNil)
		debugf(t, "new %s artifact: %+v", app.Name, artifact)
		assertImage(artifact.URI, app.Name)
	}

	// check gitreceive has the correct slug env vars
	gitreceive, err = client.GetAppRelease("gitreceive")
	t.Assert(err, c.IsNil)
	for _, name := range []string{"slugbuilder", "slugrunner"} {
		artifact, err := client.GetArtifact(gitreceive.Env[strings.ToUpper(name)+"_IMAGE_ID"])
		t.Assert(err, c.IsNil)
		assertImage(artifact.URI, name)
	}

	// check slug based app was deployed correctly
	release, err = client.GetAppRelease(slugApp.Name)
	t.Assert(err, c.IsNil)
	imageArtifact, err = client.GetArtifact(release.ArtifactIDs[0])
	t.Assert(err, c.IsNil)
	assertImage(imageArtifact.URI, "slugrunner")

	// check Redis app was deployed correctly
	release, err = client.GetAppRelease(resource.Env["FLYNN_REDIS"])
	t.Assert(err, c.IsNil)
	imageArtifact, err = client.GetArtifact(release.ArtifactIDs[0])
	t.Assert(err, c.IsNil)
	assertImage(imageArtifact.URI, "redis")
}
