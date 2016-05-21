// +build windows

package utils

import (
	"path/filepath"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/longpath"
)

func getContextRoot(srcPath string) (string, error) {
	cr, err := filepath.Abs(srcPath)
	if err != nil {
		return "", err
	}
	return longpath.AddPrefix(cr), nil
}
