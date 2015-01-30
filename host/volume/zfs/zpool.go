package zfs

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
)

func zpoolImportFile(fileVdevPath string) error {
	// make tmpdir with symlink to make it possible to actually look at a single file with 'zpool import'
	tempDir, err := ioutil.TempDir("/tmp/", "zfs-import-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	if err := os.Symlink(fileVdevPath, filepath.Join(tempDir, filepath.Base(fileVdevPath))); err != nil {
		return err
	}
	if err := exec.Command("zpool", "import", "-d", tempDir, "-a").Run(); err != nil {
		return err
	}
	return nil
}
