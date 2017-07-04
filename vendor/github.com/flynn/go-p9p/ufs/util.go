package ufs

import (
	"os"
	"syscall"

	p9p "github.com/flynn/go-p9p"
)

func dirFromInfo(info os.FileInfo) p9p.Dir {
	dir := p9p.Dir{}

	dir.Qid.Path = info.Sys().(*syscall.Stat_t).Ino
	dir.Qid.Version = uint32(info.ModTime().UnixNano() / 1000000)

	dir.Name = info.Name()
	dir.Mode = uint32(info.Mode() & 0777)
	dir.Length = uint64(info.Size())
	dir.AccessTime = atime(info.Sys().(*syscall.Stat_t))
	dir.ModTime = info.ModTime()
	dir.MUID = "none"

	if info.IsDir() {
		dir.Qid.Type |= p9p.QTDIR
		dir.Mode |= p9p.DMDIR
	}

	return dir
}

func oflags(mode p9p.Flag) int {
	flags := 0

	switch mode & 3 {
	case p9p.OREAD:
		flags = os.O_RDONLY
		break

	case p9p.ORDWR:
		flags = os.O_RDWR
		break

	case p9p.OWRITE:
		flags = os.O_WRONLY
		break

	case p9p.OEXEC:
		flags = os.O_RDONLY
		break
	}

	if mode&p9p.OTRUNC != 0 {
		flags |= os.O_TRUNC
	}

	return flags
}
