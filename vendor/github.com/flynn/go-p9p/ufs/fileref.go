package ufs

import (
	"os"
	"sync"

	p9p "github.com/flynn/go-p9p"
)

type FileRef struct {
	sync.Mutex
	Path    string
	Info    p9p.Dir
	File    *os.File
	Readdir *p9p.Readdir
}

func (f *FileRef) Stat() error {
	f.Lock()
	defer f.Unlock()
	return f.statLocked()
}

func (f *FileRef) statLocked() error {
	info, err := os.Lstat(f.Path)
	if err != nil {
		return err
	}

	f.Info = dirFromInfo(info)
	return nil
}

func (f *FileRef) IsDir() bool {
	return f.Info.Mode&p9p.DMDIR > 0
}
