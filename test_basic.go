package main

import (
	"fmt"
	"os/exec"
	"path/filepath"

	c "gopkg.in/check.v1"
)

func initApp(t *c.C, app string) string {
	dir := filepath.Join(t.MkDir(), "app")
	t.Assert(run(exec.Command("cp", "-r", filepath.Join("apps", app), dir)), Succeeds)
	t.Assert(git(dir, "init"), Succeeds)
	t.Assert(git(dir, "add", "."), Succeeds)
	t.Assert(git(dir, "commit", "-am", "init"), Succeeds)
	return dir
}

type appSuite struct {
	appDir string
}

func (s *appSuite) Flynn(args ...string) *CmdResult {
	return flynn(s.appDir, args...)
}

func (s *appSuite) Git(args ...string) *CmdResult {
	return git(s.appDir, args...)
}

type BasicSuite struct {
	appSuite
}

var _ = c.Suite(&BasicSuite{})

func (s *BasicSuite) SetUpSuite(t *c.C) {
	s.appDir = initApp(t, "basic")
}

func (s *BasicSuite) TestBasic(t *c.C) {
	name := random()[:30]
	t.Assert(s.Flynn("create", name), Outputs, fmt.Sprintf("Created %s\n", name))

	push := s.Git("push", "flynn", "master")
	t.Assert(push, Succeeds)

	// flynn scale web=3
	// flynn route-add-http
	// flynn ps
	// flynn routes
	// flynn log
	// make requests
}
