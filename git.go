package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

const gitURLSuf = ".git"

func gitURLPre(gitHost string) string {
	return "ssh://git@" + gitHost + "/"
}

func mapOutput(out []byte, sep, term string) map[string]string {
	m := make(map[string]string)
	lines := strings.Split(string(out), term)
	for _, line := range lines[:len(lines)-1] { // omit trailing ""
		parts := strings.SplitN(line, sep, 2)
		m[parts[0]] = parts[1]
	}
	return m
}

type remoteApp struct {
	Server *ServerConfig
	Name   string
}

func gitRemotes() (map[string]remoteApp, error) {
	b, err := exec.Command("git", "remote", "-v").Output()
	if err != nil {
		return nil, err
	}
	return parseGitRemoteOutput(b)
}

func appFromGitURL(remote string) *remoteApp {
	for _, s := range config.Servers {
		if strings.HasPrefix(remote, gitURLPre(s.GitHost)) && strings.HasSuffix(remote, gitURLSuf) {
			return &remoteApp{s, remote[len(gitURLPre(s.GitHost)) : len(remote)-len(gitURLSuf)]}
		}
	}
	return nil
}

func parseGitRemoteOutput(b []byte) (results map[string]remoteApp, err error) {
	s := bufio.NewScanner(bytes.NewBuffer(b))
	s.Split(bufio.ScanLines)

	results = make(map[string]remoteApp)

	for s.Scan() {
		by := s.Bytes()
		f := bytes.Fields(by)
		if len(f) != 3 || string(f[2]) != "(push)" {
			// this should have 3 tuples + be a push remote, skip it if not
			continue
		}

		if app := appFromGitURL(string(f[1])); app != nil {
			results[string(f[0])] = *app
		}
	}
	if err = s.Err(); err != nil {
		return nil, err
	}
	return
}

func remoteFromGitConfig() string {
	b, err := exec.Command("git", "config", "flynn.remote").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

var errMultipleFlynnRemotes = errors.New("multiple apps in git remotes")

func appFromGitRemote(remote string) (*remoteApp, error) {
	if remote != "" {
		b, err := exec.Command("git", "config", "remote."+remote+".url").Output()
		if err != nil {
			if isNotFound(err) {
				wdir, _ := os.Getwd()
				return nil, fmt.Errorf("could not find git remote "+remote+" in %s", wdir)
			}
			return nil, err
		}

		out := strings.TrimSpace(string(b))

		app := appFromGitURL(out)
		if app == nil {
			return nil, fmt.Errorf("could not find app name in " + remote + " git remote")
		}
		return app, nil
	}

	// no remote specified, see if there is a single Flynn app remote
	remotes, err := gitRemotes()
	if err != nil {
		return nil, nil // hide this error
	}
	if len(remotes) > 1 {
		return nil, errMultipleFlynnRemotes
	}
	for _, v := range remotes {
		return &v, nil
	}
	return nil, fmt.Errorf("no apps in git remotes")
}

func isNotFound(err error) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		if ws, ok := ee.ProcessState.Sys().(syscall.WaitStatus); ok {
			return ws.ExitStatus() == 1
		}
	}
	return false
}
