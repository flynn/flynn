// +build ignore

package p9p

import (
	"log"
	"os"
)

type logging struct {
	session Session
	logger  log.Logger
}

var _ Session = &logging{}

func NewLogger(prefix string, session Session) Session {
	return &logging{
		session: session,
		logger:  *log.New(os.Stdout, prefix, 0),
	}
}

func (l *logging) Auth(afid Fid, uname, aname string) (Qid, error) {
	qid, err := l.session.Auth(afid, uname, aname)
	l.logger.Printf("Auth(%v, %s, %s) -> (%v, %v)", afid, uname, aname, qid, err)
	return qid, err
}

func (l *logging) Attach(fid, afid Fid, uname, aname string) (Qid, error) {
	qid, err := l.session.Attach(fid, afid, uname, aname)
	l.logger.Printf("Attach(%v, %v, %s, %s) -> (%v, %v)", fid, afid, uname, aname, qid, err)
	return qid, err
}

func (l *logging) Clunk(fid Fid) error {
	return l.session.Clunk(fid)
}

func (l *logging) Remove(fid Fid) (err error) {
	defer func() {
		l.logger.Printf("Remove(%v) -> %v", fid, err)
	}()
	return l.session.Remove(fid)
}

func (l *logging) Walk(fid Fid, newfid Fid, names ...string) ([]Qid, error) {
	return l.session.Walk(fid, newfid, names...)
}

func (l *logging) Read(fid Fid, p []byte, offset int64) (n int, err error) {
	return l.session.Read(fid, p, offset)
}

func (l *logging) Write(fid Fid, p []byte, offset int64) (n int, err error) {
	return l.session.Write(fid, p, offset)
}

func (l *logging) Open(fid Fid, mode int32) (Qid, error) {
	return l.session.Open(fid, mode)
}

func (l *logging) Create(parent Fid, name string, perm uint32, mode uint32) (Qid, error) {
	return l.session.Create(parent, name, perm, mode)
}

func (l *logging) Stat(fid Fid) (Dir, error) {
	return l.session.Stat(fid)
}

func (l *logging) WStat(fid Fid, dir Dir) error {
	return l.session.WStat(fid, dir)
}

func (l *logging) Version(msize int32, version string) (int32, string, error) {
	return l.session.Version(msize, version)
}
