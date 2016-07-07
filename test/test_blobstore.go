package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"

	c "github.com/flynn/go-check"
)

type BlobstoreSuite struct {
	Helper
}

var _ = c.Suite(&BlobstoreSuite{})

func (s *BlobstoreSuite) TestBlobstoreBackendSwitching(t *c.C) {
	s3Config := os.Getenv("BLOBSTORE_S3_CONFIG")
	if s3Config == "" {
		// BLOBSTORE_S3_CONFIG should be set to a valid configuration like:
		// backend=s3 access_key_id=xxx secret_access_key=xxx bucket=blobstore-ci region=us-east-1
		t.Skip("missing BLOBSTORE_S3_CONFIG env var")
	}

	r := s.newGitRepo(t, "http")
	t.Assert(r.flynn("create", "blobstore-backend-test"), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)

	// set default backend to S3 without printing secrets
	cmd := exec.Command("sh", "-c", args.CLI+` -a blobstore env set "BACKEND_S3=$BLOBSTORE_S3_CONFIG" DEFAULT_BACKEND=s3`)
	cmd.Env = flynnEnv(flynnrc)
	cmd.Dir = "/"
	t.Assert(cmd.Run(), c.IsNil)

	// test that downloading blob from postgres still works
	t.Assert(r.flynn("run", "echo", "1"), Succeeds)

	// get slug artifact details
	release, err := s.controllerClient(t).GetAppRelease("blobstore-backend-test")
	t.Assert(err, c.IsNil)
	artifact, err := s.controllerClient(t).GetArtifact(release.FileArtifactIDs()[0])
	t.Assert(err, c.IsNil)

	// migrate slug to s3
	u, err := url.Parse(artifact.URI)
	t.Assert(err, c.IsNil)
	migration := flynn(t, "/", "-a", "blobstore", "run", "-e", "/bin/flynn-blobstore-migrate", "--", "-delete", "-prefix", u.Path)
	t.Assert(migration, Succeeds)
	t.Assert(migration, OutputContains, "Moving "+u.Path)
	t.Assert(migration, OutputContains, "from postgres to s3")

	// check that slug is now stored in S3
	noRedirectsClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return errors.New("no redirects") },
	}
	res, err := noRedirectsClient.Get(artifact.URI)
	if res == nil {
		t.Fatal(err)
	}
	t.Assert(res.StatusCode, c.Equals, 302)
	t.Assert(res.Header.Get("Location"), c.Matches, ".+s3.amazonaws.com.+")

	// test that downloading blob from s3 works
	t.Assert(r.flynn("run", "echo", "1"), Succeeds)

	// test that deploying still works
	t.Assert(r.git("commit", "--allow-empty", "-m", "foo"), Succeeds)
	t.Assert(r.git("push", "flynn", "master"), Succeeds)

	// test that exporting the app works
	t.Assert(r.flynn("export", "--file", "/dev/null"), Succeeds)

	// change default backend back to postgres
	t.Assert(flynn(t, "/", "-a", "blobstore", "env", "set", "DEFAULT_BACKEND=postgres"), Succeeds)

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
	migration = flynn(t, "/", "-a", "blobstore", "run", "-e", "/bin/flynn-blobstore-migrate", "--", "-delete")
	t.Assert(migration, Succeeds)
	t.Assert(migration, OutputContains, "from s3 to postgres")

	// test that downloading blob from postgres still works
	t.Assert(r.flynn("run", "echo", "1"), Succeeds)

	// check that all blobs are in postgres
	t.Assert(flynn(t, "/", "-a", "blobstore", "pg", "psql", "--", "-c", "SELECT count(*) FROM files WHERE backend = 's3' AND deleted_at IS NULL"), OutputContains, "0")
}
