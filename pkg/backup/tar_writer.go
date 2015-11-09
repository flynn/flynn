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

type TarWriter struct {
	*tar.Writer
	uid  int
	name string
}

func NewTarWriter(name string, w io.Writer) *TarWriter {
	return &TarWriter{
		Writer: tar.NewWriter(w),
		uid:    syscall.Getuid(),
		name:   name,
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
	return err
}

func (t *TarWriter) WriteCommandOutput(client *controller.Client, name string, app string, newJob *ct.NewJob) error {
	f, err := ioutil.TempFile("", name)
	if err != nil {
		return fmt.Errorf("error creating temp file: %s", err)
	}
	defer f.Close()
	defer os.Remove(f.Name())

	if err := t.runJob(client, app, newJob, f); err != nil {
		return fmt.Errorf("error running export: %s", err)
	}

	length, err := f.Seek(0, os.SEEK_CUR)
	if err != nil {
		return fmt.Errorf("error getting size: %s", err)
	}
	if err := t.WriteHeader(name, int(length)); err != nil {
		return fmt.Errorf("error writing header: %s", err)
	}
	if _, err := f.Seek(0, os.SEEK_SET); err != nil {
		return fmt.Errorf("error seeking: %s", err)
	}
	if _, err := io.Copy(t, f); err != nil {
		return fmt.Errorf("error exporting: %s", err)
	}
	return nil
}

func (t *TarWriter) runJob(client *controller.Client, app string, req *ct.NewJob, out io.Writer) error {
	rwc, err := client.RunJobAttached(app, req)
	if err != nil {
		return err
	}
	defer rwc.Close()
	attachClient := cluster.NewAttachClient(rwc)
	attachClient.CloseWrite()
	_, err = attachClient.Receive(out, os.Stderr)
	return err
}
