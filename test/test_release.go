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

var releaseScript = template.Must(template.New("release-script").Parse(`
export DISCOVERD="{{ .Discoverd }}"
export TUF_TARGETS_PASSPHRASE="flynn-test"
export TUF_SNAPSHOT_PASSPHRASE="flynn-test"
export TUF_TIMESTAMP_PASSPHRASE="flynn-test"
export GOPATH=~/go

ROOT="${GOPATH}/src/github.com/flynn/flynn"
cd "${ROOT}"

# send all output to stderr so only images.json is output to stdout
(
  # serve the test TUF repository over HTTP
  dir="$(mktemp --directory)"
  ln -s "${ROOT}/test/release/repository" "${dir}/tuf"
  ln -s "${ROOT}/script/install-flynn" "${dir}/install-flynn"
  sudo start-stop-daemon \
    --start \
    --background \
    --chdir "${dir}" \
    --exec "${ROOT}/build/bin/flynn-test-file-server"

  # update the builder manifest to use the test TUF repository and create new
  # image manifests for each released image by updating entrypoints
  jq \
    --argjson root_keys       "$(build/bin/tuf --dir test/release root-keys)" \
    --argjson released_images "$(jq --compact-output 'keys | reduce .[] as $name ({}; .[$name] = true)' build/manifests/images.json)" \
    '.tuf.root_keys = $root_keys | .tuf.repository = "http://{{ .HostIP }}:8080/tuf" | .images |= map(if (.id | in($released_images)) then .entrypoint.env = {"FOO":"BAR"} else . end)' \
    builder/manifest.json \
    > /tmp/manifest.json
  mv /tmp/manifest.json builder/manifest.json

  # build new images and binaries
  FLYNN_VERSION="v20161108.0.test"
  script/build-flynn --host "{{ .HostID }}" --version "${FLYNN_VERSION}"

  # release components
  script/export-components --host "{{ .HostID }}" "${ROOT}/test/release"
  script/release-channel --tuf-dir "${ROOT}/test/release" --no-sync --no-changelog "stable" "${FLYNN_VERSION}"

  # create a slug for testing slug based app updates
  build/bin/flynn-host run \
    --volume /tmp \
    build/image/slugbuilder.json \
    /usr/bin/env \
    CONTROLLER_KEY="{{ .ControllerKey }}" \
    SLUG_IMAGE_ID="{{ .SlugImageID }}" \
    /builder/build.sh \
    < <(tar c -C test/apps/http .)

) </dev/null >&2

cat "${ROOT}/build/manifests/images.json"
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
build/bin/tuf --dir test/release root-keys | build/bin/tuf-client init --store /tmp/tuf.db http://{{ .Blobstore }}/tuf
echo stable | sudo tee /etc/flynn/channel.txt
export DISCOVERD="{{ .Discoverd }}"
build/bin/flynn-host update --repository http://{{ .Blobstore }}/tuf --tuf-db /tmp/tuf.db
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
	releaseScript.Execute(&script, struct {
		Discoverd, HostID, HostIP, ControllerKey, SlugImageID string
	}{fmt.Sprintf("http://%s:1111", buildHost.IP), buildHost.ID, buildHost.IP, releaseCluster.ControllerKey, slugImageID})
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
	t.Assert(strings.TrimSpace(hostVersion.String()), c.Equals, "v20161108.0.test")

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
	t.Assert(client.CreateRelease(slugApp.ID, release), c.IsNil)
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
		debugf(t, "new %s artifact: ID: %s, URI: %s", app.Name, artifact.ID, artifact.URI)
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
