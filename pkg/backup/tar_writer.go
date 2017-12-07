package backup

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"syscall"
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/cluster"
)

type ProgressBar interface {
	Add(int) int
	io.Writer
}

type TarWriter struct {
	*tar.Writer
	uid      int
	name     string
	progress ProgressBar
}

func NewTarWriter(name string, w io.Writer, progress ProgressBar) *TarWriter {
	userid := syscall.Getuid()
	if userid < 0 {
		userid = 0
	}
	return &TarWriter{
		Writer:   tar.NewWriter(w),
		uid:      userid,
		name:     name,
		progress: progress,
	}
}

func (t *TarWriter) WriteHeader(name string, length int) error {
	return t.Writer.WriteHeader(&tar.Header{
		Name:     path.Join(t.name, name),
		Mode:     0644,
		Size:     int64(length),
		ModTime:  time.Now(),
		Typeflag: tar.TypeReg,
		Uid:      t.uid,
		Gid:      t.uid,
	})
}

func (t *TarWriter) WriteJSON(name string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := t.WriteHeader(name, len(data)+1); err != nil {
		return err
	}
	if _, err := t.Write(data); err != nil {
		return err
	}
	_, err = t.Write([]byte("\n"))
	if t.progress != nil {
		t.progress.Add(len(data) + 1)
	}
	return err
}

func (t *TarWriter) WriteCommandOutput(client controller.Client, name string, app string, newJob *ct.NewJob) error {
	f, err := ioutil.TempFile("", name)
	if err != nil {
		return fmt.Errorf("error creating temp file: %s", err)
	}
	defer f.Close()
	defer os.Remove(f.Name())

	var dest io.Writer = f
	if t.progress != nil {
		dest = io.MultiWriter(f, t.progress)
	}
	if err := t.runJob(client, app, newJob, dest); err != nil {
		return fmt.Errorf("error running %s export: %s", app, err)
	}

	length, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("error getting size: %s", err)
	}
	if err := t.WriteHeader(name, int(length)); err != nil {
		return fmt.Errorf("error writing header: %s", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("error seeking: %s", err)
	}
	if _, err := io.Copy(t, f); err != nil {
		return fmt.Errorf("error exporting: %s", err)
	}
	return nil
}

func (t *TarWriter) runJob(client controller.Client, app string, req *ct.NewJob, out io.Writer) error {
	// set deprecated Entrypoint and Cmd for old clusters
	if len(req.Args) > 0 {
		req.DeprecatedEntrypoint = []string{req.Args[0]}
	}
	if len(req.Args) > 1 {
		req.DeprecatedCmd = req.Args[1:]
	}

	rwc, err := client.RunJobAttached(app, req)
	if err != nil {
		return err
	}
	defer rwc.Close()
	attachClient := cluster.NewAttachClient(rwc)
	attachClient.CloseWrite()
	exit, err := attachClient.Receive(out, os.Stderr)
	if err != nil {
		return err
	}
	if exit != 0 {
		return fmt.Errorf("unexpected command exit status %d", exit)
	}
	return nil
}
