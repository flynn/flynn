package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	ct "github.com/flynn/flynn/controller/types"
	c "github.com/flynn/go-check"
)

type BlobstoreSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&BlobstoreSuite{})

func (s *BlobstoreSuite) TestBlobstoreBackendS3(t *c.C) {
	s3Config := os.Getenv("BLOBSTORE_S3_CONFIG")
	if s3Config == "" {
		// BLOBSTORE_S3_CONFIG should be set to a valid configuration like:
		// backend=s3 access_key_id=xxx secret_access_key=xxx bucket=blobstore-ci region=us-east-1
		t.Skip("missing BLOBSTORE_S3_CONFIG env var")
	}

	s.testBlobstoreBackend(t, "s3", ".+s3.amazonaws.com.+", `"BACKEND_S3=$BLOBSTORE_S3_CONFIG"`)
}

func (s *BlobstoreSuite) TestBlobstoreBackendGCS(t *c.C) {
	gcsConfig := os.Getenv("BLOBSTORE_GCS_CONFIG")
	if gcsConfig == "" {
		// BLOBSTORE_S3_CONFIG should be set to a JSON-encoded Google Cloud
		// Service Account key that includes an extra field named "bucket" that
		// specifies the bucket to use
		t.Skip("missing BLOBSTORE_GCS_CONFIG env var")
	}

	var data struct{ Bucket string }
	err := json.Unmarshal([]byte(gcsConfig), &data)
	t.Assert(err, c.IsNil)

	s.testBlobstoreBackend(t, "gcs", ".+google.+", fmt.Sprintf(`"BACKEND_GCS=backend=gcs bucket=%s"`, data.Bucket), `"BACKEND_GCS_KEY=$BLOBSTORE_GCS_CONFIG"`)
}

func (s *BlobstoreSuite) TestBlobstoreBackendAzure(t *c.C) {
	s3Config := os.Getenv("BLOBSTORE_AZURE_CONFIG")
	if s3Config == "" {
		// BLOBSTORE_AZURE_CONFIG should be set to a valid configuration like:
		// backend=azure account_name=xxx account_key=xxx container=blobstore-ci
		t.Skip("missing BLOBSTORE_AZURE_CONFIG env var")
	}

	s.testBlobstoreBackend(t, "azure", ".+blob.core.windows.net.+", `"BACKEND_AZURE=$BLOBSTORE_AZURE_CONFIG"`)
}

func (s *BlobstoreSuite) testBlobstoreBackend(t *c.C, name, redirectPattern string, env ...string) {
	x := s.bootCluster(t, 1)
	defer x.Destroy()

	r := s.newGitRepo(t, "http")
	r.cluster = x
	t.Assert(r.flynn("create", "blobstore-backend-test-"+name), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)

	// set default backend to external backend without printing secrets
	cmd := exec.Command("sh", "-c", fmt.Sprintf("%s -a blobstore env set %s DEFAULT_BACKEND=%s", args.CLI, strings.Join(env, " "), name))
	cmd.Env = flynnEnv(x.flynnrc)
	cmd.Dir = "/"
	t.Assert(cmd.Run(), c.IsNil)

	// test that downloading blob from postgres still works
	t.Assert(r.flynn("run", "echo", "1"), Succeeds)

	// get slug artifact details
	release, err := x.controller.GetAppRelease("blobstore-backend-test-" + name)
	t.Assert(err, c.IsNil)
	artifact, err := x.controller.GetArtifact(release.ArtifactIDs[1])
	t.Assert(err, c.IsNil)
	t.Assert(artifact.Type, c.Equals, ct.ArtifactTypeFlynn)

	// migrate slug to external backend
	layer := artifact.Manifest().Rootfs[0].Layers[0]
	u, err := url.Parse(artifact.LayerURL(layer))
	t.Assert(err, c.IsNil)
	migration := x.flynn("/", "-a", "blobstore", "run", "-e", "/bin/flynn-blobstore", "migrate", "--delete", "--prefix", u.Path)
	t.Assert(migration, Succeeds)
	t.Assert(migration, OutputContains, "Moving "+u.Path)
	t.Assert(migration, OutputContains, "from postgres to "+name)

	// check that slug is now stored in external backend
	curl := x.flynn("/", "-a", "blobstore", "run", "curl", "--silent", "--include", "--raw", u.String())
	t.Assert(curl, Succeeds)
	res, err := http.ReadResponse(bufio.NewReader(curl.OutputBuf), nil)
	if res == nil {
		t.Fatal(err)
	}
	res.Body.Close()
	t.Assert(res.StatusCode, c.Equals, 302)
	t.Assert(res.Header.Get("Location"), c.Matches, redirectPattern)

	// test that downloading blob from external backend works
	t.Assert(r.flynn("run", "echo", "1"), Succeeds)

	// test that deploying still works
	t.Assert(r.git("commit", "--allow-empty", "-m", "foo"), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)

	// test that build caching still works
	s.testBuildCaching(t, x)

	// test that exporting the app works
	t.Assert(r.flynn("export", "--file", "/dev/null"), Succeeds)

	// change default backend back to postgres
	t.Assert(x.flynn("/", "-a", "blobstore", "env", "set", "DEFAULT_BACKEND=postgres"), Succeeds)

	// test that downloading blob from s3 still works
	t.Assert(r.flynn("run", "echo", "1"), Succeeds)

	// test a docker push
	repo := "s3-test"
	s.buildDockerImage(t, repo, "RUN echo foo > /foo.txt")
	u, err = url.Parse(s.clusterConf(t).DockerPushURL)
	t.Assert(err, c.IsNil)
	tag := fmt.Sprintf("%s/%s:latest", u.Host, repo)
	t.Assert(run(t, exec.Command("docker", "tag", "--force", repo, tag)), Succeeds)
	t.Assert(run(t, exec.Command("docker", "push", tag)), Succeeds)

	// migrate blobs back to postgres
	migration = x.flynn("/", "-a", "blobstore", "run", "-e", "/bin/flynn-blobstore", "migrate", "--delete")
	t.Assert(migration, Succeeds)
	t.Assert(migration, OutputContains, fmt.Sprintf("from %s to postgres", name))

	// test that downloading blob from postgres still works
	t.Assert(r.flynn("run", "echo", "1"), Succeeds)

	// check that all blobs are in postgres
	t.Assert(x.flynn("/", "-a", "blobstore", "pg", "psql", "--", "-c", fmt.Sprintf("SELECT count(*) FROM files WHERE backend = '%s' AND deleted_at IS NULL", name)), OutputContains, "0")
}
