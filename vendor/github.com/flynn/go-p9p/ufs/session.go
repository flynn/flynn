package ufs

import (
	"context"
	"io"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"

	"github.com/flynn/go-p9p"
)

type session struct {
	sync.Mutex
	rootRef *FileRef
	refs    map[p9p.Fid]*FileRef
}

func NewSession(ctx context.Context, root string) (p9p.Session, error) {
	return &session{
		rootRef: &FileRef{Path: root},
		refs:    make(map[p9p.Fid]*FileRef),
	}, nil
}

func (sess *session) getRef(fid p9p.Fid) (*FileRef, error) {
	sess.Lock()
	defer sess.Unlock()

	if fid == p9p.NOFID {
		return nil, p9p.ErrUnknownfid
	}

	ref, found := sess.refs[fid]
	if !found {
		return nil, p9p.ErrUnknownfid
	}

	if err := ref.Stat(); err != nil {
		return nil, err
	}

	return ref, nil
}

func (sess *session) newRef(fid p9p.Fid, path string) (*FileRef, error) {
	sess.Lock()
	defer sess.Unlock()

	if fid == p9p.NOFID {
		return nil, p9p.ErrUnknownfid
	}

	_, found := sess.refs[fid]
	if found {
		return nil, p9p.ErrDupfid
	}

	ref := &FileRef{Path: path}
	if err := ref.Stat(); err != nil {
		return nil, err
	}

	sess.refs[fid] = ref
	return ref, nil
}

func (sess *session) Auth(ctx context.Context, afid p9p.Fid, uname, aname string) (p9p.Qid, error) {
	// TODO: AuthInit?
	return p9p.Qid{}, nil //p9p.MessageRerror{Ename: "no auth"}
}

func (sess *session) Attach(ctx context.Context, fid, afid p9p.Fid, uname, aname string) (p9p.Qid, error) {
	if uname == "" {
		return p9p.Qid{}, p9p.MessageRerror{Ename: "no user"}
	}

	// TODO: AuthCheck?

	// if afid > 0 {
	// 	return p9p.Qid{}, p9p.MessageRerror{Ename: "attach: no auth"}
	// }

	if aname == "" {
		aname = sess.rootRef.Path
	}

	ref, err := sess.newRef(fid, aname)
	if err != nil {
		return p9p.Qid{}, err
	}

	return ref.Info.Qid, nil
}

func (sess *session) Clunk(ctx context.Context, fid p9p.Fid) error {
	ref, err := sess.getRef(fid)
	if err != nil {
		return err
	}

	ref.Lock()
	defer ref.Unlock()
	if ref.File != nil {
		ref.File.Close()
	}

	sess.Lock()
	defer sess.Unlock()
	delete(sess.refs, fid)

	return nil
}

func (sess *session) Remove(ctx context.Context, fid p9p.Fid) error {
	defer sess.Clunk(ctx, fid)

	ref, err := sess.getRef(fid)
	if err != nil {
		return err
	}

	// TODO: check write perms on parent

	return os.Remove(ref.Path)
}

func (sess *session) Walk(ctx context.Context, fid p9p.Fid, newfid p9p.Fid, names ...string) ([]p9p.Qid, error) {
	var qids []p9p.Qid

	ref, err := sess.getRef(fid)
	if err != nil {
		return qids, err
	}

	newref, err := sess.newRef(newfid, ref.Path)
	if err != nil {
		return qids, err
	}

	path := newref.Path
	for _, name := range names {
		newpath := filepath.Join(path, name)
		r := &FileRef{Path: newpath}
		if err := r.Stat(); err != nil {
			break
		}
		qids = append(qids, r.Info.Qid)
		path = newpath
	}

	newref.Path = path
	return qids, nil
}

func (sess *session) Read(ctx context.Context, fid p9p.Fid, p []byte, offset int64) (n int, err error) {
	ref, err := sess.getRef(fid)
	if err != nil {
		return 0, err
	}

	ref.Lock()
	defer ref.Unlock()

	if ref.IsDir() {
		if offset == 0 && ref.Readdir == nil {
			files, err := ioutil.ReadDir(ref.Path)
			if err != nil {
				return 0, err
			}
			var dirs []p9p.Dir
			for _, info := range files {
				dirs = append(dirs, dirFromInfo(info))
			}
			ref.Readdir = p9p.NewFixedReaddir(p9p.NewCodec(), dirs)
		}
		if ref.Readdir == nil {
			return 0, p9p.ErrBadoffset
		}
		return ref.Readdir.Read(ctx, p, offset)
	}

	if ref.File == nil {
		return 0, p9p.MessageRerror{Ename: "no file open"} //p9p.ErrClosed
	}

	n, err = ref.File.ReadAt(p, offset)
	if err != nil && err != io.EOF {
		return n, err
	}
	return n, nil
}

func (sess *session) Write(ctx context.Context, fid p9p.Fid, p []byte, offset int64) (n int, err error) {
	ref, err := sess.getRef(fid)
	if err != nil {
		return 0, err
	}

	ref.Lock()
	defer ref.Unlock()
	if ref.File == nil {
		return 0, p9p.ErrClosed
	}

	return ref.File.WriteAt(p, offset)
}

func (sess *session) Open(ctx context.Context, fid p9p.Fid, mode p9p.Flag) (p9p.Qid, uint32, error) {
	ref, err := sess.getRef(fid)
	if err != nil {
		return p9p.Qid{}, 0, err
	}

	ref.Lock()
	defer ref.Unlock()
	f, err := os.OpenFile(ref.Path, oflags(mode), 0)
	if err != nil {
		return p9p.Qid{}, 0, err
	}
	ref.File = f
	return ref.Info.Qid, 0, nil
}

func (sess *session) Create(ctx context.Context, parent p9p.Fid, name string, perm uint32, mode p9p.Flag) (p9p.Qid, uint32, error) {
	ref, err := sess.getRef(parent)
	if err != nil {
		return p9p.Qid{}, 0, err
	}

	newpath := filepath.Join(ref.Path, name)

	var file *os.File
	switch {
	case perm&p9p.DMDIR != 0:
		err = os.Mkdir(newpath, os.FileMode(perm&0777))

	case perm&p9p.DMSYMLINK != 0:
	case perm&p9p.DMNAMEDPIPE != 0:
	case perm&p9p.DMDEVICE != 0:
		err = p9p.MessageRerror{Ename: "not implemented"}

	default:
		file, err = os.OpenFile(newpath, oflags(mode)|os.O_CREATE, os.FileMode(perm&0777))
	}

	if file == nil && err == nil {
		file, err = os.OpenFile(newpath, oflags(mode), 0)
	}

	if err != nil {
		return p9p.Qid{}, 0, err
	}

	ref.Lock()
	defer ref.Unlock()
	ref.Path = newpath
	ref.File = file
	if err := ref.statLocked(); err != nil {
		return p9p.Qid{}, 0, err
	}
	return ref.Info.Qid, 0, err
}

func (sess *session) Stat(ctx context.Context, fid p9p.Fid) (p9p.Dir, error) {
	ref, err := sess.getRef(fid)
	if err != nil {
		return p9p.Dir{}, err
	}
	return ref.Info, nil
}

func (sess *session) WStat(ctx context.Context, fid p9p.Fid, dir p9p.Dir) error {
	ref, err := sess.getRef(fid)
	if err != nil {
		return err
	}

	if dir.Mode != ^uint32(0) {
		// TODO: 9P2000.u: DMSETUID DMSETGID
		err := os.Chmod(ref.Path, os.FileMode(dir.Mode&0777))
		if err != nil {
			return err
		}
	}

	if dir.UID != "" || dir.GID != "" {
		usr, err := user.Lookup(dir.UID)
		if err != nil {
			return err
		}
		uid, err := strconv.Atoi(usr.Uid)
		if err != nil {
			return err
		}
		grp, err := user.LookupGroup(dir.GID)
		if err != nil {
			return err
		}
		gid, err := strconv.Atoi(grp.Gid)
		if err != nil {
			return err
		}
		if err := os.Chown(ref.Path, uid, gid); err != nil {
			return err
		}
	}

	if dir.Name != "" {
		newpath := filepath.Join(filepath.Dir(ref.Path), dir.Name)
		if err := syscall.Rename(ref.Path, newpath); err != nil {
			return nil
		}
		ref.Lock()
		defer ref.Unlock()
		ref.Path = newpath
	}

	if dir.Length != ^uint64(0) {
		if err := os.Truncate(ref.Path, int64(dir.Length)); err != nil {
			return err
		}
	}

	// If either mtime or atime need to be changed, then
	// we must change both.
	//if dir.ModTime != time.Time{} || dir.AccessTime != ^uint32(0) {
	// mt, at := time.Unix(int64(dir.Mtime), 0), time.Unix(int64(dir.Atime), 0)
	// if cmt, cat := (dir.Mtime == ^uint32(0)), (dir.Atime == ^uint32(0)); cmt || cat {
	// 	st, e := os.Stat(fid.path)
	// 	if e != nil {
	// 		req.RespondError(toError(e))
	// 		return
	// 	}
	// 	switch cmt {
	// 	case true:
	// 		mt = st.ModTime()
	// 	default:
	// 		at = atime(st.Sys().(*syscall.Stat_t))
	// 	}
	// }
	// e := os.Chtimes(fid.path, at, mt)
	// if e != nil {
	// 	req.RespondError(toError(e))
	// 	return
	// }
	//}
	return nil
}

func (sess *session) Version() (msize int, version string) {
	return p9p.DefaultMSize, p9p.DefaultVersion
}
