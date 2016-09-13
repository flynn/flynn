package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/attempt"
	c "github.com/flynn/go-check"
)

type BackupSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&BackupSuite{})

func (s *BackupSuite) TestClusterBackups(t *c.C) {
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

func (s *BackupSuite) testClusterBackup(t *c.C, index int, path string) {
	debugf(t, "restoring cluster backup %s", filepath.Base(path))

	// boot the cluster using an RFC 5737 TEST-NET IP, avoiding conflicts
	// with those used by script/bootstrap-flynn so the test can be run in
	// development
	ip := fmt.Sprintf("192.0.2.%d", index+100)
	device := fmt.Sprintf("eth0:%d", index+10)
	t.Assert(run(t, exec.Command("sudo", "ifconfig", device, ip)), Succeeds)

	dir := t.MkDir()
	debugf(t, "using tempdir %s", dir)

	debug(t, "starting flynn-host")
	cmd := exec.Command(
		"sudo",
		"../host/bin/flynn-host",
		"daemon",
		"--id", fmt.Sprintf("backup%d", index),
		"--external-ip", ip,
		"--listen-ip", ip,
		"--bridge-name", fmt.Sprintf("backupbr%d", index),
		"--state", filepath.Join(dir, "host-state.bolt"),
		"--volpath", filepath.Join(dir, "volumes"),
		"--log-dir", filepath.Join(dir, "logs"),
		"--flynn-init", "../host/bin/flynn-init",
	)
	out, err := os.Create(filepath.Join(dir, "flynn-host.log"))
	t.Assert(err, c.IsNil)
	defer out.Close()
	cmd.Stdout = out
	cmd.Stderr = out
	t.Assert(cmd.Start(), c.IsNil)
	go cmd.Process.Wait()
	defer exec.Command("sudo", "kill", strconv.Itoa(cmd.Process.Pid)).Run()

	debugf(t, "bootstrapping flynn from backup")
	cmd = exec.Command(
		"../host/bin/flynn-host",
		"bootstrap",
		"--peer-ips", ip,
		"--from-backup", path,
		"../bootstrap/bin/manifest.json",
	)
	cmd.Env = []string{
		"CLUSTER_DOMAIN=1.localflynn.com",
		fmt.Sprintf("DISCOVERD=%s:1111", ip),
		fmt.Sprintf("FLANNEL_NETWORK=100.%d.0.0/16", index+101),
	}
	logR, logW := io.Pipe()
	defer logW.Close()
	go func() {
		buf := bufio.NewReader(logR)
		for {
			line, err := buf.ReadString('\n')
			if err != nil {
				return
			}
			debug(t, line[0:len(line)-1])
		}
	}()
	cmd.Stdout = logW
	cmd.Stderr = logW
	t.Assert(cmd.Run(), c.IsNil)

	debug(t, "waiting for nodejs-web service")
	disc := discoverd.NewClientWithURL(fmt.Sprintf("http://%s:1111", ip))
	_, err = disc.Instances("nodejs-web", 30*time.Second)
	t.Assert(err, c.IsNil)

	debug(t, "checking HTTP requests")
	req, err := http.NewRequest("GET", "http://"+ip, nil)
	t.Assert(err, c.IsNil)
	req.Host = "nodejs.1.localflynn.com"
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

	debug(t, "configuring flynn CLI")
	controllerInstances, err := disc.Instances("controller", 30*time.Second)
	t.Assert(err, c.IsNil)
	flynnrc := filepath.Join(dir, ".flynnrc")
	conf := &config.Config{}
	t.Assert(conf.Add(&config.Cluster{
		Name:          "default",
		ControllerURL: "http://" + controllerInstances[0].Addr,
		Key:           controllerInstances[0].Meta["AUTH_KEY"],
	}, true), c.IsNil)
	t.Assert(conf.SaveTo(flynnrc), c.IsNil)
	flynn := func(cmdArgs ...string) *CmdResult {
		cmd := exec.Command(args.CLI, cmdArgs...)
		cmd.Env = flynnEnv(flynnrc)
		cmd.Env = append(cmd.Env, "FLYNN_APP=nodejs")
		return run(t, cmd)
	}

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
