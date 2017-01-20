package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/flynn/flynn/pkg/attempt"
	c "github.com/flynn/go-check"
)

type BackupSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&BackupSuite{})

func (s *BackupSuite) Test_v20160309_0_nodejs_redis(t *c.C) {
	s.testClusterBackup(t, "v20160309.0-nodejs-redis.tar")
}

func (s *BackupSuite) Test_v20160423_0_nodejs_redis(t *c.C) {
	s.testClusterBackup(t, "v20160423.0-nodejs-redis.tar")
}

func (s *BackupSuite) Test_v20160624_1_nodejs_redis(t *c.C) {
	s.testClusterBackup(t, "v20160624.1-nodejs-redis.tar")
}

func (s *BackupSuite) Test_v20160721_2_nodejs_redis(t *c.C) {
	s.testClusterBackup(t, "v20160721.2-nodejs-redis.tar")
}

func (s *BackupSuite) Test_v20160814_0_nodejs_mongodb(t *c.C) {
	s.testClusterBackup(t, "v20160814.0-nodejs-mongodb.tar")
}

func (s *BackupSuite) Test_v20160814_0_nodejs_redis(t *c.C) {
	s.testClusterBackup(t, "v20160814.0-nodejs-redis.tar")
}

func (s *BackupSuite) Test_v20160814_0_nodejs_mysql(t *c.C) {
	s.testClusterBackup(t, "v20160814.0-nodejs-mysql.tar")
}

func (s *BackupSuite) Test_v20161017_1_nodejs_docker(t *c.C) {
	s.testClusterBackup(t, "v20161017.1-nodejs-docker.tar")
}

func (s *BackupSuite) Test_v20161114_0p1_nodejs_redis(t *c.C) {
	s.testClusterBackup(t, "v20161114.0p1-nodejs-redis.tar")
}

func (s *BackupSuite) testClusterBackup(t *c.C, name string) {
	if args.BootConfig.BackupsDir == "" {
		t.Skip("--backups-dir not set")
	}

	path := filepath.Join(args.BootConfig.BackupsDir, name)
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		t.Skip(fmt.Sprintf("missing backup %s", name))
	}
	t.Assert(err, c.IsNil)

	debugf(t, "restoring cluster backup %s", name)

	x := s.bootClusterFromBackup(t, path)
	defer x.Destroy()

	debug(t, "waiting for nodejs-web service")
	_, err = x.discoverd.Instances("nodejs-web", 5*time.Minute)
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
