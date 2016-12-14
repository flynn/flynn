package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	cfg "github.com/flynn/flynn/cli/config"
	"github.com/flynn/go-docopt"
)

var gitRepo *bool

func inGitRepo() bool {
	if gitRepo != nil {
		return *gitRepo
	}
	b := exec.Command("git", "rev-parse", "--git-dir").Run() == nil
	gitRepo = &b
	return b
}

const gitURLSuffix = ".git"

func gitURL(conf *cfg.Cluster, app string) string {
	return gitHTTPURLPre(conf.GitURL) + app + gitURLSuffix
}

func gitHTTPURLPre(url string) string {
	return url + "/"
}

type remoteApp struct {
	Cluster *cfg.Cluster
	Name    string
}

func gitRemoteNames() (results []string, err error) {
	b, err := exec.Command("git", "remote").Output()
	if err != nil {
		return nil, err
	}

	s := bufio.NewScanner(bytes.NewBuffer(b))
	s.Split(bufio.ScanWords)

	for s.Scan() {
		by := s.Bytes()
		f := bytes.Fields(by)

		results = append(results, string(f[0]))
	}

	if err = s.Err(); err != nil {
		return nil, err
	}

	return
}

func gitRemotes() (map[string]remoteApp, error) {
	b, err := exec.Command("git", "remote", "-v").Output()
	if err != nil {
		return nil, err
	}
	return parseGitRemoteOutput(b)
}

func appFromGitURL(remote string) *remoteApp {
	for _, s := range config.Clusters {
		if flagCluster != "" && s.Name != flagCluster {
			continue
		}

		prefix := gitHTTPURLPre(s.GitURL)
		if strings.HasPrefix(remote, prefix) && strings.HasSuffix(remote, gitURLSuffix) {
			return &remoteApp{s, remote[len(prefix) : len(remote)-len(gitURLSuffix)]}
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

type multipleRemotesError []string

func (remotes multipleRemotesError) Error() string {
	return "error: Multiple apps listed in git remotes, please specify one with the global -a option to disambiguate.\n\nAvailable Flynn remotes:\n" + strings.Join(remotes, "\n")
}

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
		err := make(multipleRemotesError, 0, len(remotes))
		for r := range remotes {
			err = append(err, r)
		}
		return nil, err
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

func init() {
	register("git-credentials", runGitCredentials, "usage: flynn git-credentials <operation>")
}

func runGitCredentials(args *docopt.Args) error {
	if args.String["<operation>"] != "get" {
		return nil
	}

	r := bufio.NewReader(os.Stdin)
	details := make(map[string]string)
	for {
		l, _, err := r.ReadLine()
		if err != nil && err != io.EOF {
			return err
		}
		if len(l) == 0 {
			break
		}
		kv := bytes.SplitN(l, []byte("="), 2)
		if len(kv) == 2 {
			details[string(kv[0])] = string(kv[1])
		}
	}

	if details["protocol"] != "https" {
		return nil
	}
	if err := readConfig(); err != nil {
		return nil
	}

	var cluster *cfg.Cluster
	url := "https://" + details["host"]
	for _, c := range config.Clusters {
		if c.GitURL == url {
			cluster = c
			break
		}
	}
	if cluster == nil {
		return nil
	}

	fmt.Printf("protocol=https\nusername=user\nhost=%s\npassword=%s\n", details["host"], cluster.Key)
	return nil
}
