package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/kardianos/osext"
)

func CACertFile(name string) (*os.File, error) {
	dir := filepath.Join(Dir(), "ca-certs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return os.Create(filepath.Join(dir, name+".pem"))
}

func gitConfig(args ...string) error {
	args = append([]string{"config", "--global"}, args...)
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error %q running %q: %q", err, strings.Join(cmd.Args, " "), out)
	}
	return nil
}

func WriteGlobalGitConfig(gitURL, caFile string) error {
	if err := gitConfig(fmt.Sprintf("http.%s.sslCAInfo", gitURL), caFile); err != nil {
		return err
	}
	self, err := osext.Executable()
	if err != nil {
		return err
	}

	// Ensure the path uses `/`s
	// Git on windows can't handle `\`s
	self = filepath.ToSlash(self)

	if err := gitConfig(fmt.Sprintf("credential.%s.helper", gitURL), self+" git-credentials"); err != nil {
		return err
	}
	return nil
}

func RemoveGlobalGitConfig(gitURL string) {
	for _, k := range []string{
		fmt.Sprintf("http.%s", gitURL),
		fmt.Sprintf("credential.%s", gitURL),
	} {
		gitConfig("--remove-section", k)
	}
}
