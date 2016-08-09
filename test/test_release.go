package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/template"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/types"
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

var releaseScript = bytes.NewReader([]byte(`
export TUF_TARGETS_PASSPHRASE="flynn-test"
export TUF_SNAPSHOT_PASSPHRASE="flynn-test"
export TUF_TIMESTAMP_PASSPHRASE="flynn-test"

export GOPATH=~/go
src="${GOPATH}/src/github.com/flynn/flynn"

# send all output to stderr so only version.json is output to stdout
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
  go build -o host/bin/flynn-host -ldflags="-X ${vpkg}.commit notdev -X ${vpkg}.branch dev -X ${vpkg}.tag v20160711.0-test -X ${vpkg}.dirty false" ./host
  gzip -9 --keep --force host/bin/flynn-host
  sed "s/{{FLYNN-HOST-CHECKSUM}}/$(sha512sum host/bin/flynn-host.gz | cut -d " " -f 1)/g" script/install-flynn.tmpl > script/install-flynn

  # create new images
  test/scripts/wait-for-docker
  for name in $(docker images | grep ^flynn | awk '{print $1}'); do
    docker build -t $name - < <(echo -e "FROM $name\nRUN /bin/true")
  done

  util/release/flynn-release manifest util/release/version_template.json > version.json
  popd >/dev/null

  "${src}/script/export-components" --no-compress "${src}/test/release"
  "${src}/script/release-channel" --tuf-dir "${src}/test/release" --no-sync --no-changelog "stable" "v20160711.0-test"

  dir=$(mktemp --directory)
  ln -s "${src}/test/release/repository" "${dir}/tuf"
  ln -s "${src}/script/install-flynn" "${dir}/install-flynn"

  # create a slug for testing slug based app updates
  tar c -C "${src}/test/apps/http" . | docker run -i -a stdin -a stdout -a stderr flynn/slugbuilder - > "${dir}/slug.tgz"

  # start a file server to serve the exported components
  sudo start-stop-daemon \
    --start \
    --background \
    --chdir "${dir}" \
    --exec "${src}/test/bin/flynn-test-file-server"
) >&2

cat "${src}/version.json"
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
flynn-host update --repository http://{{ .Blobstore }}/tuf --tuf-db /tmp/tuf.db
SCRIPT
`))

func (s *ReleaseSuite) TestReleaseImages(t *c.C) {
	if testCluster == nil {
		t.Skip("cannot boot release cluster")
	}

	// stream script output to t.Log
	logReader, logWriter := io.Pipe()
	defer logWriter.Close()
	go func() {
		buf := bufio.NewReader(logReader)
		for {
			line, err := buf.ReadString('\n')
			if err != nil {
				return
			}
			debug(t, line[0:len(line)-1])
		}
	}()

	// boot the release cluster, release components to a blobstore and output the new version.json
	releaseCluster := s.addReleaseHosts(t)
	buildHost := releaseCluster.Instances[0]
	var versionJSON bytes.Buffer
	t.Assert(buildHost.Run("bash -ex", &tc.Streams{Stdin: releaseScript, Stdout: &versionJSON, Stderr: logWriter}), c.IsNil)
	var versions map[string]string
	t.Assert(json.Unmarshal(versionJSON.Bytes(), &versions), c.IsNil)

	// install Flynn from the blobstore on the vanilla host
	blobstore := struct{ Blobstore string }{buildHost.IP + ":8080"}
	installHost := releaseCluster.Instances[3]
	var script bytes.Buffer
	installScript.Execute(&script, blobstore)
	var installOutput bytes.Buffer
	out := io.MultiWriter(logWriter, &installOutput)
	t.Assert(installHost.Run("sudo bash -ex", &tc.Streams{Stdin: &script, Stdout: out, Stderr: out}), c.IsNil)

	// check the flynn-host version is correct
	var hostVersion bytes.Buffer
	t.Assert(installHost.Run("flynn-host version", &tc.Streams{Stdout: &hostVersion}), c.IsNil)
	t.Assert(strings.TrimSpace(hostVersion.String()), c.Equals, "v20160711.0-test")

	// check rebuilt images were downloaded
	for name, id := range versions {
		expected := fmt.Sprintf("%s image %s downloaded", name, id)
		if !strings.Contains(installOutput.String(), expected) {
			t.Fatalf(`expected install to download %s %s`, name, id)
		}
	}

	// installing on an instance with Flynn running should fail
	script.Reset()
	installScript.Execute(&script, blobstore)
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
	slugArtifact := &ct.Artifact{Type: host.ArtifactTypeFile, URI: fmt.Sprintf("http://%s:8080/slug.tgz", buildHost.IP)}
	t.Assert(client.CreateArtifact(slugArtifact), c.IsNil)
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
	updateScript.Execute(&script, blobstore)
	var updateOutput bytes.Buffer
	out = io.MultiWriter(logWriter, &updateOutput)
	t.Assert(updateHost.Run("bash -ex", &tc.Streams{Stdin: &script, Stdout: out, Stderr: out}), c.IsNil)

	// check rebuilt images were downloaded
	for name := range versions {
		for _, host := range releaseCluster.Instances[0:2] {
			expected := fmt.Sprintf(`"pulled image" host=%s name=%s`, host.ID, name)
			if !strings.Contains(updateOutput.String(), expected) {
				t.Fatalf(`expected update to download %s on host %s`, name, host.ID)
			}
		}
	}

	assertImage := func(uri, image string) {
		u, err := url.Parse(uri)
		t.Assert(err, c.IsNil)
		t.Assert(u.Query().Get("id"), c.Equals, versions[image])
	}

	// check system apps were deployed correctly
	for _, app := range updater.SystemApps {
		if app.ImageOnly {
			continue // we don't deploy ImageOnly updates
		}
		if app.Image == "" {
			app.Image = "flynn/" + app.Name
		}
		debugf(t, "checking new %s release is using image %s", app.Name, versions[app.Image])
		expected := fmt.Sprintf(`"finished deploy of system app" name=%s`, app.Name)
		if !strings.Contains(updateOutput.String(), expected) {
			t.Fatalf(`expected update to deploy %s`, app.Name)
		}
		release, err := client.GetAppRelease(app.Name)
		t.Assert(err, c.IsNil)
		debugf(t, "new %s release ID: %s", app.Name, release.ID)
		artifact, err := client.GetArtifact(release.ImageArtifactID())
		t.Assert(err, c.IsNil)
		debugf(t, "new %s artifact: %+v", app.Name, artifact)
		assertImage(artifact.URI, app.Image)
	}

	// check gitreceive has the correct slug env vars
	gitreceive, err = client.GetAppRelease("gitreceive")
	t.Assert(err, c.IsNil)
	for _, name := range []string{"slugbuilder", "slugrunner"} {
		artifact, err := client.GetArtifact(gitreceive.Env[strings.ToUpper(name)+"_IMAGE_ID"])
		t.Assert(err, c.IsNil)
		assertImage(artifact.URI, "flynn/"+name)
	}

	// check slug based app was deployed correctly
	release, err = client.GetAppRelease(slugApp.Name)
	t.Assert(err, c.IsNil)
	imageArtifact, err = client.GetArtifact(release.ImageArtifactID())
	t.Assert(err, c.IsNil)
	assertImage(imageArtifact.URI, "flynn/slugrunner")

	// check Redis app was deployed correctly
	release, err = client.GetAppRelease(resource.Env["FLYNN_REDIS"])
	t.Assert(err, c.IsNil)
	imageArtifact, err = client.GetArtifact(release.ImageArtifactID())
	t.Assert(err, c.IsNil)
	assertImage(imageArtifact.URI, "flynn/redis")
}
