package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/template"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	tc "github.com/flynn/flynn/test/cluster"
)

type ReleaseSuite struct {
	Helper
}

var _ = c.Suite(&ReleaseSuite{})

var releaseScript = bytes.NewReader([]byte(`
export TUF_TARGETS_PASSPHRASE="flynn-test"
export TUF_SNAPSHOT_PASSPHRASE="flynn-test"
export TUF_TIMESTAMP_PASSPHRASE="flynn-test"

export GOPATH=~/go
src="${GOPATH}/src/github.com/flynn/flynn"

export GIT_COMMIT=dev
export GIT_BRANCH=dev
export GIT_TAG="v20150131.0-test"
export GIT_DIRTY=false

# send all output to stderr so only version.json is output to stdout
(
  # tup hangs waiting on the FUSE socket after building the components, so
  # for now we just kill the connection once the host manifest has been generated.
  # See https://github.com/flynn/flynn/issues/949
  (inotifywait --event modify host/bin/manifest.json; sudo bash -c 'echo 1 > /sys/fs/fuse/connections/*/abort') &

  pushd "${src}" >/dev/null

  root_keys="$(tuf --dir test/release root-keys)"
  sed "s/^CONFIG_TUF_ROOT_KEYS=.*$/CONFIG_TUF_ROOT_KEYS=${root_keys}/" -i tup.config

  # rebuild layer 0 components
  rebuild=(
    "host/bin/flynn-host"
    "host/bin/flynn-init"
    "host/bin/manifest.json"
    "script/install-flynn"
  )
  tup ${rebuild[@]}

  # manually rebuild version.json for the export to avoid rebuilding all of layer 1
  util/release/flynn-release manifest util/release/version_template.json > version.json

  popd >/dev/null

  "${src}/script/export-components" "${src}/test/release"

  dir=$(mktemp --directory)
  mv "${src}/test/release/repository" "${dir}/tuf"
  mv "${src}/script/install-flynn" "${dir}/install-flynn"

  # start a blobstore to serve the exported components
  sudo start-stop-daemon \
    --start \
    --background \
    --exec "${src}/blobstore/bin/flynn-blobstore" \
    -- \
    -d=false \
    -s="${dir}" \
    -p=8080
) >&2

cat "${src}/version.json"
`))

var installScript = template.Must(template.New("install-script").Parse(`
# download to a tmp file so the script fails on download error rather than
# executing nothing and succeeding
curl -sL --fail http://{{ .Blobstore }}/install-flynn > /tmp/install-flynn
bash -e /tmp/install-flynn -r "http://{{ .Blobstore }}"
`))

func (s *ReleaseSuite) TestReleaseImages(t *c.C) {
	// stream script output to t.Log
	logReader, logWriter := io.Pipe()
	go func() {
		buf := bufio.NewReader(logReader)
		for {
			line, err := buf.ReadString('\n')
			debug(t, line[0:len(line)-1])
			if err != nil {
				return
			}
		}
	}()

	// boot a host to release components to a local blobstore, outputting the new version.json
	buildHost := s.addHost(t)
	defer s.removeHost(t, buildHost)
	var versionJSON bytes.Buffer
	t.Assert(buildHost.Run("bash -ex", &tc.Streams{Stdin: releaseScript, Stdout: &versionJSON, Stderr: logWriter}), c.IsNil)
	var versions map[string]string
	t.Assert(json.Unmarshal(versionJSON.Bytes(), &versions), c.IsNil)

	// boot a host and install Flynn from the blobstore
	installHost := s.addVanillaHost(t)
	var script bytes.Buffer
	installScript.Execute(&script, struct{ Blobstore string }{buildHost.IP + ":8080"})
	var installOutput bytes.Buffer
	out := io.MultiWriter(logWriter, &installOutput)
	t.Assert(installHost.Run("sudo bash -ex", &tc.Streams{Stdin: &script, Stdout: out, Stderr: out}), c.IsNil)

	// check the flynn-host version is correct
	var hostVersion bytes.Buffer
	t.Assert(installHost.Run("flynn-host version", &tc.Streams{Stdout: &hostVersion}), c.IsNil)
	t.Assert(strings.TrimSpace(hostVersion.String()), c.Equals, "v20150131.0-test")

	// check rebuilt images were downloaded
	images := []string{"flynn/etcd", "flynn/discoverd", "flynn/flannel"}
	for _, name := range images {
		id, ok := versions[name]
		if !ok {
			t.Fatalf("could not determine id of %s image", name)
		}
		expected := fmt.Sprintf("%s %s downloaded", name, id)
		if !strings.Contains(installOutput.String(), expected) {
			t.Fatalf(`expected install output to contain "%s"`, expected)
		}
	}
}
