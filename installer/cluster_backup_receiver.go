package installer

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"path"
	"sync"

	ct "github.com/flynn/flynn/controller/types"
)

type ClusterBackupReceiver struct {
	backup      io.Reader
	pipe        *io.PipeWriter
	size        int
	err         error
	errMux      sync.RWMutex
	subs        []chan error
	subsMux     sync.Mutex
	cluster     *BaseCluster
	prevPercent int
	nRead       int
}

func (c *BaseCluster) NewBackupReceiver(backup io.Reader, size int) *ClusterBackupReceiver {
	r, w := io.Pipe()
	cbr := &ClusterBackupReceiver{
		cluster: c,
		backup:  io.TeeReader(backup, w),
		pipe:    w,
		size:    size,
		subs:    make([]chan error, 0),
	}
	go func() {
		tr := tar.NewReader(r)
		var h *tar.Header
		var err error
		var data struct {
			Controller *ct.ExpandedFormation
		}
		for {
			h, err = tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				cbr.readBackupError(fmt.Errorf("Error reading backup: %s", err))
				break
			}
			if path.Base(h.Name) != "flynn.json" {
				io.Copy(ioutil.Discard, tr)
				continue
			}
			if err := json.NewDecoder(tr).Decode(&data); err != nil {
				cbr.readBackupError(fmt.Errorf("Error decoding backup data: %s", err))
				break
			}
			err = fmt.Errorf("Error: flynn.json is missing DEFAULT_ROUTE_DOMAIN or AUTH_KEY for controller")
			if data.Controller == nil || data.Controller.Release == nil || data.Controller.Release.Env == nil {
				cbr.readBackupError(err)
				break
			}
			c.oldDomain = data.Controller.Release.Env["DEFAULT_ROUTE_DOMAIN"]
			c.ControllerKey = data.Controller.Release.Env["AUTH_KEY"]
			if c.oldDomain == "" || c.ControllerKey == "" {
				cbr.readBackupError(err)
			}
			break
		}
		io.Copy(ioutil.Discard, r)
	}()
	return cbr
}

func (cbr *ClusterBackupReceiver) Read(p []byte) (int, error) {
	cbr.errMux.RLock()
	if cbr.err != nil {
		cbr.errMux.RUnlock()
		return 0, cbr.err
	}
	cbr.errMux.RUnlock()

	n, err := cbr.backup.Read(p)
	if err != nil {
		return n, err
	}
	cbr.nRead = cbr.nRead + n
	percent := (cbr.nRead * 100 / cbr.size * 100) / 100
	if percent > cbr.prevPercent {
		cbr.prevPercent = percent
		cbr.cluster.SendProgress(&ProgressEvent{
			ID:          "upload-backup",
			Description: fmt.Sprintf("Uploading... [%d%%]", percent),
			Percent:     percent,
		})
	}
	return n, nil
}

func (cbr *ClusterBackupReceiver) Close() error {
	var err error
	err = cbr.pipe.Close()
	if c, ok := cbr.backup.(io.Closer); ok {
		err = c.Close()
	}
	return err
}

type readBackupError error

func (cbr *ClusterBackupReceiver) readBackupError(err error) {
	cbr.cluster.SendLog(err.Error())
	cbr.UploadComplete(readBackupError(err))
}

func (cbr *ClusterBackupReceiver) UploadComplete(err error) {
	cbr.errMux.Lock()
	defer cbr.errMux.Unlock()
	if cbr.err == nil {
		cbr.err = err
	}

	cbr.subsMux.Lock()
	for _, ch := range cbr.subs {
		ch <- err
	}
	cbr.subs = make([]chan error, 0)
	cbr.subsMux.Unlock()
}

func (cbr *ClusterBackupReceiver) Wait() error {
	cbr.errMux.RLock()
	if cbr.err != nil {
		cbr.errMux.RUnlock()
		return cbr.err
	}
	cbr.errMux.RUnlock()

	ch := make(chan error)
	cbr.subsMux.Lock()
	cbr.subs = append(cbr.subs, ch)
	cbr.subsMux.Unlock()
	return <-ch
}

func (cbr *ClusterBackupReceiver) Error() error {
	cbr.errMux.Lock()
	defer cbr.errMux.Unlock()
	return cbr.err
}
