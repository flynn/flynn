package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kardianos/osext"
)

func CACertPath(name string) string {
	return filepath.Join(Dir(), "ca-certs", name+".pem")
}

func CACertFile(name string) (*os.File, error) {
	path := CACertPath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	return os.Create(path)
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
