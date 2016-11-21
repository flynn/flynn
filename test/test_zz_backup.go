package main

import (
	"errors"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"time"

	"github.com/flynn/flynn/pkg/attempt"
	c "github.com/flynn/go-check"
)

// Prefix the suite with "ZZ" so that it runs after all other tests as it is
// pretty disruptive
type ZZBackupSuite struct {
	Helper
}

var _ = c.Suite(&ZZBackupSuite{})

func (s *ZZBackupSuite) TestClusterBackups(t *c.C) {
	if args.BootConfig.BackupsDir == "" {
		t.Skip("--backups-dir not set")
	}

	backups, err := ioutil.ReadDir(args.BootConfig.BackupsDir)
	t.Assert(err, c.IsNil)
	if len(backups) == 0 {
		t.Fatal("backups dir is empty")
	}

	for i, backup := range backups {
		s.testClusterBackup(t, i, filepath.Join(args.BootConfig.BackupsDir, backup.Name()))
	}
}

func (s *ZZBackupSuite) testClusterBackup(t *c.C, index int, path string) {
	debugf(t, "restoring cluster backup %s", filepath.Base(path))

	x := s.bootClusterFromBackup(t, path)
	defer x.Destroy()

	debug(t, "waiting for nodejs-web service")
	_, err := x.discoverd.Instances("nodejs-web", 30*time.Second)
	t.Assert(err, c.IsNil)

	debug(t, "checking HTTP requests")
	req, err := http.NewRequest("GET", "http://"+x.IP, nil)
	t.Assert(err, c.IsNil)
	req.Host = "nodejs." + x.Domain
	var res *http.Response
	// try multiple times in case we get a 503 from the router as it has
	// not seen the service yet
	err = attempt.Strategy{Total: 10 * time.Second, Delay: 100 * time.Millisecond}.Run(func() (err error) {
		res, err = http.DefaultClient.Do(req)
		if err != nil {
			return err
		} else if res.StatusCode == http.StatusServiceUnavailable {
			return errors.New("router returned 503")
		}
		return nil
	})
	t.Assert(err, c.IsNil)
	t.Assert(res.StatusCode, c.Equals, http.StatusOK)

	debug(t, "getting app release")
	release, err := x.controller.GetAppRelease("nodejs")
	t.Assert(err, c.IsNil)

	flynn := func(cmdArgs ...string) *CmdResult {
		return x.flynn("/", append([]string{"-a", "nodejs"}, cmdArgs...)...)
	}

	if _, ok := release.Env["FLYNN_REDIS"]; ok {
		debug(t, "checking redis resource")
		// try multiple times as the Redis resource is not guaranteed to be up yet
		var redisResult *CmdResult
		err = attempt.Strategy{Total: 10 * time.Second, Delay: 100 * time.Millisecond}.Run(func() error {
			redisResult = flynn("redis", "redis-cli", "--", "PING")
			return redisResult.Err
		})
		t.Assert(err, c.IsNil)
		t.Assert(redisResult, SuccessfulOutputContains, "PONG")
	}

	debug(t, "checking mysql resource")
	if _, ok := release.Env["FLYNN_MYSQL"]; ok {
		t.Assert(flynn("mysql", "console", "--", "-e", "SELECT * FROM foos"), SuccessfulOutputContains, "foobar")
	} else {
		t.Assert(flynn("resource", "add", "mysql"), Succeeds)
	}

	debug(t, "checking mongodb resource")
	if _, ok := release.Env["FLYNN_MONGO"]; ok {
		t.Assert(flynn("mongodb", "mongo", "--", "--eval", "db.foos.find()"), SuccessfulOutputContains, "foobar")
	} else {
		t.Assert(flynn("resource", "add", "mongodb"), Succeeds)
	}

	debug(t, "checking dashboard STATUS_KEY matches status AUTH_KEY")
	dashboardStatusKeyResult := x.flynn("/", "-a", "dashboard", "env", "get", "STATUS_KEY")
	t.Assert(dashboardStatusKeyResult, Succeeds)
	statusAuthKeyResult := x.flynn("/", "-a", "status", "env", "get", "AUTH_KEY")
	t.Assert(statusAuthKeyResult, Succeeds)
	t.Assert(dashboardStatusKeyResult.Output, c.Equals, statusAuthKeyResult.Output)
}
