package volume

import (
	"os"
	"path/filepath"
	"syscall"
)

func IsMount(path string) (bool, error) {
	pathStat, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	parentStat, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return false, err
	}
	pathDev := pathStat.Sys().(*syscall.Stat_t).Dev
	parentDev := parentStat.Sys().(*syscall.Stat_t).Dev
	return pathDev != parentDev, nil
}
