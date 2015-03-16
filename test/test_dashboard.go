package main

import (
	"encoding/base64"
	"os"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/pkg/exec"
)

type DashboardSuite struct {
	Helper
}

var _ = c.ConcurrentSuite(&DashboardSuite{})

func (s *DashboardSuite) SetUpSuite(t *c.C) {
}

func (s *DashboardSuite) TestDashboard(t *c.C) {
	githubToken, err := base64.StdEncoding.DecodeString("YjFjOGVmYmQzY2FiNzg5MjE4Y2E3MTg3OTU2ODE2YWI1MzhmY2YyOQo=")
	t.Assert(err, c.IsNil)

	cc := s.controllerClient(t)
	release, err := cc.GetAppRelease("dashboard")
	t.Assert(err, c.IsNil)

	cmd := exec.Command(exec.DockerImage(imageURIs["test-dashboard-app"]), "/bin/test-runner.sh")
	cmd.Env = map[string]string{
		"ROUTER_IP":    routerIP,
		"URL":          release.Env["URL"],
		"LOGIN_TOKEN":  release.Env["LOGIN_TOKEN"],
		"GITHUB_TOKEN": string(githubToken),
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	t.Assert(err, c.IsNil)
}
