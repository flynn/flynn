// Copyright 2009 The go9p Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package go9p

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type ufsFid struct {
	path       string
	file       *os.File
	dirs       []os.FileInfo
	direntends []int
	dirents    []byte
	diroffset  uint64
	st         os.FileInfo
}

type Ufs struct {
	Srv
	Root string
}

func toError(err error) *Error {
	var ecode uint32

	ename := err.Error()
	if e, ok := err.(syscall.Errno); ok {
		ecode = uint32(e)
	} else {
		ecode = EIO
	}

	return &Error{ename, ecode}
}

// IsBlock reports if the file is a block device
func isBlock(d os.FileInfo) bool {
	stat := d.Sys().(*syscall.Stat_t)
	return (stat.Mode & syscall.S_IFMT) == syscall.S_IFBLK
}

// IsChar reports if the file is a character device
func isChar(d os.FileInfo) bool {
	stat := d.Sys().(*syscall.Stat_t)
	return (stat.Mode & syscall.S_IFMT) == syscall.S_IFCHR
}

func (fid *ufsFid) stat() *Error {
	var err error

	fid.st, err = os.Lstat(fid.path)
	if err != nil {
		return toError(err)
	}

	return nil
}

func omode2uflags(mode uint8) int {
	ret := int(0)
	switch mode & 3 {
	case OREAD:
		ret = os.O_RDONLY
		break

	case ORDWR:
		ret = os.O_RDWR
		break

	case OWRITE:
		ret = os.O_WRONLY
		break

	case OEXEC:
		ret = os.O_RDONLY
		break
	}

	if mode&OTRUNC != 0 {
		ret |= os.O_TRUNC
	}

	return ret
}

func dir2Qid(d os.FileInfo) *Qid {
	var qid Qid

	qid.Path = d.Sys().(*syscall.Stat_t).Ino
	qid.Version = uint32(d.ModTime().UnixNano() / 1000000)
	qid.Type = dir2QidType(d)

	return &qid
}

func dir2QidType(d os.FileInfo) uint8 {
	ret := uint8(0)
	if d.IsDir() {
		ret |= QTDIR
	}

	if d.Mode()&os.ModeSymlink != 0 {
		ret |= QTSYMLINK
	}

	return ret
}

func dir2Npmode(d os.FileInfo, dotu bool) uint32 {
	ret := uint32(d.Mode() & 0777)
	if d.IsDir() {
		ret |= DMDIR
	}

	if dotu {
		mode := d.Mode()
		if mode&os.ModeSymlink != 0 {
			ret |= DMSYMLINK
		}

		if mode&os.ModeSocket != 0 {
			ret |= DMSOCKET
		}

		if mode&os.ModeNamedPipe != 0 {
			ret |= DMNAMEDPIPE
		}

		if mode&os.ModeDevice != 0 {
			ret |= DMDEVICE
		}

		if mode&os.ModeSetuid != 0 {
			ret |= DMSETUID
		}

		if mode&os.ModeSetgid != 0 {
			ret |= DMSETGID
		}
	}

	return ret
}

// Dir is an instantiation of the p.Dir structure
// that can act as a receiver for local methods.
type ufsDir struct {
	Dir
}

func dir2Dir(path string, d os.FileInfo, dotu bool, upool Users) (*Dir, error) {
	if r := recover(); r != nil {
		fmt.Print("stat failed: ", r)
		return nil, &os.PathError{"dir2Dir", path, nil}
	}
	sysif := d.Sys()
	if sysif == nil {
		return nil, &os.PathError{"dir2Dir: sysif is nil", path, nil}
	}
	sysMode := sysif.(*syscall.Stat_t)

	dir := new(ufsDir)
	dir.Qid = *dir2Qid(d)
	dir.Mode = dir2Npmode(d, dotu)
	dir.Atime = uint32(0 /*atime(sysMode).Unix()*/)
	dir.Mtime = uint32(d.ModTime().Unix())
	dir.Length = uint64(d.Size())
	dir.Name = path[strings.LastIndex(path, "/")+1:]

	if dotu {
		dir.dotu(path, d, upool, sysMode)
		return &dir.Dir, nil
	}

	unixUid := int(sysMode.Uid)
	unixGid := int(sysMode.Gid)
	dir.Uid = strconv.Itoa(unixUid)
	dir.Gid = strconv.Itoa(unixGid)

	// BUG(akumar): LookupId will never find names for
	// groups, as it only operates on user ids.
	u, err := user.LookupId(dir.Uid)
	if err == nil {
		dir.Uid = u.Username
	}
	g, err := user.LookupId(dir.Gid)
	if err == nil {
		dir.Gid = g.Username
	}

	/* For Akaros, we use the Muid as the link value. */
	if *Akaros && (d.Mode()&os.ModeSymlink != 0) {
		dir.Muid, err = os.Readlink(path)
		if err == nil {
			dir.Mode |= DMSYMLINK
		}
	}
	return &dir.Dir, nil
}

func (dir *ufsDir) dotu(path string, d os.FileInfo, upool Users, sysMode *syscall.Stat_t) {
	u := upool.Uid2User(int(sysMode.Uid))
	g := upool.Gid2Group(int(sysMode.Gid))
	dir.Uid = u.Name()
	if dir.Uid == "" {
		dir.Uid = "none"
	}

	dir.Gid = g.Name()
	if dir.Gid == "" {
		dir.Gid = "none"
	}
	dir.Muid = "none"
	dir.Ext = ""
	dir.Uidnum = uint32(u.Id())
	dir.Gidnum = uint32(g.Id())
	dir.Muidnum = NOUID
	if d.Mode()&os.ModeSymlink != 0 {
		var err error
		dir.Ext, err = os.Readlink(path)
		if err != nil {
			dir.Ext = ""
		}
	} else if isBlock(d) {
		dir.Ext = fmt.Sprintf("b %d %d", sysMode.Rdev>>24, sysMode.Rdev&0xFFFFFF)
	} else if isChar(d) {
		dir.Ext = fmt.Sprintf("c %d %d", sysMode.Rdev>>24, sysMode.Rdev&0xFFFFFF)
	}
}

func (*Ufs) ConnOpened(conn *Conn) {
	if conn.Srv.Debuglevel > 0 {
		log.Println("connected")
	}
}

func (*Ufs) ConnClosed(conn *Conn) {
	if conn.Srv.Debuglevel > 0 {
		log.Println("disconnected")
	}
}

func (*Ufs) FidDestroy(sfid *SrvFid) {
	var fid *ufsFid

	if sfid.Aux == nil {
		return
	}

	fid = sfid.Aux.(*ufsFid)
	if fid.file != nil {
		fid.file.Close()
	}
}

func (ufs *Ufs) Attach(req *SrvReq) {
	if req.Afid != nil {
		req.RespondError(Enoauth)
		return
	}

	tc := req.Tc
	fid := new(ufsFid)
	// You can think of the ufs.Root as a 'chroot' of a sort.
	// clients attach are not allowed to go outside the
	// directory represented by ufs.Root
	fid.path = path.Join(ufs.Root, tc.Aname)

	req.Fid.Aux = fid
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	qid := dir2Qid(fid.st)
	req.RespondRattach(qid)
}

func (*Ufs) Flush(req *SrvReq) {}

func (*Ufs) Walk(req *SrvReq) {
	fid := req.Fid.Aux.(*ufsFid)
	tc := req.Tc

	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	if req.Newfid.Aux == nil {
		req.Newfid.Aux = new(ufsFid)
	}

	nfid := req.Newfid.Aux.(*ufsFid)
	wqids := make([]Qid, len(tc.Wname))
	path := fid.path
	i := 0
	for ; i < len(tc.Wname); i++ {
		p := path + "/" + tc.Wname[i]
		st, err := os.Lstat(p)
		if err != nil {
			if i == 0 {
				req.RespondError(Enoent)
				return
			}

			break
		}

		wqids[i] = *dir2Qid(st)
		path = p
	}

	nfid.path = path
	req.RespondRwalk(wqids[0:i])
}

func (*Ufs) Open(req *SrvReq) {
	fid := req.Fid.Aux.(*ufsFid)
	tc := req.Tc
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	var e error
	fid.file, e = os.OpenFile(fid.path, omode2uflags(tc.Mode), 0)
	if e != nil {
		req.RespondError(toError(e))
		return
	}

	req.RespondRopen(dir2Qid(fid.st), 0)
}

func (*Ufs) Create(req *SrvReq) {
	fid := req.Fid.Aux.(*ufsFid)
	tc := req.Tc
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	path := fid.path + "/" + tc.Name
	var e error = nil
	var file *os.File = nil
	switch {
	case tc.Perm&DMDIR != 0:
		e = os.Mkdir(path, os.FileMode(tc.Perm&0777))

	case tc.Perm&DMSYMLINK != 0:
		e = os.Symlink(tc.Ext, path)

	case tc.Perm&DMLINK != 0:
		n, e := strconv.ParseUint(tc.Ext, 10, 0)
		if e != nil {
			break
		}

		ofid := req.Conn.FidGet(uint32(n))
		if ofid == nil {
			req.RespondError(Eunknownfid)
			return
		}

		e = os.Link(ofid.Aux.(*ufsFid).path, path)
		ofid.DecRef()

	case tc.Perm&DMNAMEDPIPE != 0:
	case tc.Perm&DMDEVICE != 0:
		req.RespondError(&Error{"not implemented", EIO})
		return

	default:
		var mode uint32 = tc.Perm & 0777
		if req.Conn.Dotu {
			if tc.Perm&DMSETUID > 0 {
				mode |= syscall.S_ISUID
			}
			if tc.Perm&DMSETGID > 0 {
				mode |= syscall.S_ISGID
			}
		}
		file, e = os.OpenFile(path, omode2uflags(tc.Mode)|os.O_CREATE, os.FileMode(mode))
	}

	if file == nil && e == nil {
		file, e = os.OpenFile(path, omode2uflags(tc.Mode), 0)
	}

	if e != nil {
		req.RespondError(toError(e))
		return
	}

	fid.path = path
	fid.file = file
	err = fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	req.RespondRcreate(dir2Qid(fid.st), 0)
}

func (*Ufs) Read(req *SrvReq) {
	fid := req.Fid.Aux.(*ufsFid)
	tc := req.Tc
	rc := req.Rc
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	InitRread(rc, tc.Count)
	var count int
	var e error
	if fid.st.IsDir() {
		if tc.Offset == 0 {
			var e error
			// If we got here, it was open. Can't really seek
			// in most cases, just close and reopen it.
			fid.file.Close()
			if fid.file, e = os.OpenFile(fid.path, omode2uflags(req.Fid.Omode), 0); e != nil {
				req.RespondError(toError(e))
				return
			}

			if fid.dirs, e = fid.file.Readdir(-1); e != nil {
				req.RespondError(toError(e))
				return
			}

			fid.dirents = nil
			fid.direntends = nil
			for i := 0; i < len(fid.dirs); i++ {
				path := fid.path + "/" + fid.dirs[i].Name()
				st, _ := dir2Dir(path, fid.dirs[i], req.Conn.Dotu, req.Conn.Srv.Upool)
				if st == nil {
					continue
				}
				b := PackDir(st, req.Conn.Dotu)
				fid.dirents = append(fid.dirents, b...)
				count += len(b)
				fid.direntends = append(fid.direntends, count)
			}
		}

		switch {
		case tc.Offset > uint64(len(fid.dirents)):
			count = 0
		case len(fid.dirents[tc.Offset:]) > int(tc.Count):
			count = int(tc.Count)
		default:
			count = len(fid.dirents[tc.Offset:])
		}

		if !*Akaros {
			nextend := sort.SearchInts(fid.direntends, int(tc.Offset)+count)
			if nextend < len(fid.direntends) {
				if fid.direntends[nextend] > int(tc.Offset)+count {
					if nextend > 0 {
						count = fid.direntends[nextend-1] - int(tc.Offset)
					} else {
						count = 0
					}
				}
			}
			if count == 0 && int(tc.Offset) < len(fid.dirents) && len(fid.dirents) > 0 {
				req.RespondError(&Error{"too small read size for dir entry", EINVAL})
				return
			}
		}

		copy(rc.Data, fid.dirents[tc.Offset:int(tc.Offset)+count])

	} else {
		count, e = fid.file.ReadAt(rc.Data, int64(tc.Offset))
		if e != nil && e != io.EOF {
			req.RespondError(toError(e))
			return
		}

	}

	SetRreadCount(rc, uint32(count))
	req.Respond()
}

func (*Ufs) Write(req *SrvReq) {
	fid := req.Fid.Aux.(*ufsFid)
	tc := req.Tc
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	n, e := fid.file.WriteAt(tc.Data, int64(tc.Offset))
	if e != nil {
		req.RespondError(toError(e))
		return
	}

	req.RespondRwrite(uint32(n))
}

func (*Ufs) Clunk(req *SrvReq) { req.RespondRclunk() }

func (*Ufs) Remove(req *SrvReq) {
	fid := req.Fid.Aux.(*ufsFid)
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	e := os.Remove(fid.path)
	if e != nil {
		req.RespondError(toError(e))
		return
	}

	req.RespondRremove()
}

func (*Ufs) Stat(req *SrvReq) {
	fid := req.Fid.Aux.(*ufsFid)
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	st, derr := dir2Dir(fid.path, fid.st, req.Conn.Dotu, req.Conn.Srv.Upool)
	if st == nil {
		req.RespondError(derr)
		return
	}

	req.RespondRstat(st)
}

func lookup(uid string, group bool) (uint32, *Error) {
	if uid == "" {
		return NOUID, nil
	}
	usr, e := user.Lookup(uid)
	if e != nil {
		return NOUID, toError(e)
	}
	conv := usr.Uid
	if group {
		conv = usr.Gid
	}
	u, e := strconv.Atoi(conv)
	if e != nil {
		return NOUID, toError(e)
	}
	return uint32(u), nil
}

func (u *Ufs) Wstat(req *SrvReq) {
	fid := req.Fid.Aux.(*ufsFid)
	err := fid.stat()
	if err != nil {
		req.RespondError(err)
		return
	}

	dir := &req.Tc.Dir
	if dir.Mode != 0xFFFFFFFF {
		mode := dir.Mode & 0777
		if req.Conn.Dotu {
			if dir.Mode&DMSETUID > 0 {
				mode |= syscall.S_ISUID
			}
			if dir.Mode&DMSETGID > 0 {
				mode |= syscall.S_ISGID
			}
		}
		e := os.Chmod(fid.path, os.FileMode(mode))
		if e != nil {
			req.RespondError(toError(e))
			return
		}
	}

	uid, gid := NOUID, NOUID
	if req.Conn.Dotu {
		uid = dir.Uidnum
		gid = dir.Gidnum
	}

	// Try to find local uid, gid by name.
	if (dir.Uid != "" || dir.Gid != "") && !req.Conn.Dotu {
		uid, err = lookup(dir.Uid, false)
		if err != nil {
			req.RespondError(err)
			return
		}

		// BUG(akumar): Lookup will never find gids
		// corresponding to group names, because
		// it only operates on user names.
		gid, err = lookup(dir.Gid, true)
		if err != nil {
			req.RespondError(err)
			return
		}
	}

	if uid != NOUID || gid != NOUID {
		e := os.Chown(fid.path, int(uid), int(gid))
		if e != nil {
			req.RespondError(toError(e))
			return
		}
	}

	if dir.Name != "" {
		fmt.Printf("Rename %s to %s\n", fid.path, dir.Name)
		// if first char is / it is relative to root, else relative to
		// cwd.
		var destpath string
		if dir.Name[0] == '/' {
			destpath = path.Join(u.Root, dir.Name)
			fmt.Printf("/ results in %s\n", destpath)
		} else {
			fiddir, _ := path.Split(fid.path)
			destpath = path.Join(fiddir, dir.Name)
			fmt.Printf("rel  results in %s\n", destpath)
		}
		err := syscall.Rename(fid.path, destpath)
		fmt.Printf("rename %s to %s gets %v\n", fid.path, destpath, err)
		if err != nil {
			req.RespondError(toError(err))
			return
		}
		fid.path = destpath
	}

	if dir.Length != 0xFFFFFFFFFFFFFFFF {
		e := os.Truncate(fid.path, int64(dir.Length))
		if e != nil {
			req.RespondError(toError(e))
			return
		}
	}

	// If either mtime or atime need to be changed, then
	// we must change both.
	if dir.Mtime != ^uint32(0) || dir.Atime != ^uint32(0) {
		mt, at := time.Unix(int64(dir.Mtime), 0), time.Unix(int64(dir.Atime), 0)
		if cmt, cat := (dir.Mtime == ^uint32(0)), (dir.Atime == ^uint32(0)); cmt || cat {
			st, e := os.Stat(fid.path)
			if e != nil {
				req.RespondError(toError(e))
				return
			}
			switch cmt {
			case true:
				mt = st.ModTime()
			default:
				//at = time.Time(0)//atime(st.Sys().(*syscall.Stat_t))
			}
		}
		e := os.Chtimes(fid.path, at, mt)
		if e != nil {
			req.RespondError(toError(e))
			return
		}
	}

	req.RespondRwstat()
}

/* enables "Akaros" capabilities, which right now means
 * a sane error message format.
 */
var Akaros = flag.Bool("akaros", false, "Akaros extensions")
