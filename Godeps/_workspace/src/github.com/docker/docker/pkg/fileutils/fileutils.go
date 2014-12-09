package fileutils

import (
	"path/filepath"

	log "github.com/flynn/flynn/Godeps/_workspace/src/github.com/Sirupsen/logrus"
)

// Matches returns true if relFilePath matches any of the patterns
func Matches(relFilePath string, patterns []string) (bool, error) {
	for _, exclude := range patterns {
		matched, err := filepath.Match(exclude, relFilePath)
		if err != nil {
			log.Errorf("Error matching: %s (pattern: %s)", relFilePath, exclude)
			return false, err
		}
		if matched {
			if filepath.Clean(relFilePath) == "." {
				log.Errorf("Can't exclude whole path, excluding pattern: %s", exclude)
				continue
			}
			log.Debugf("Skipping excluded path: %s", relFilePath)
			return true, nil
		}
	}
	return false, nil
}
