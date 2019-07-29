package main

import (
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	controller "github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/backup"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"golang.org/x/net/context"
)

func (c *controllerAPI) GetBackup(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	if !strings.Contains(req.Header.Get("Accept"), "json") {
		c.createAndStreamBackup(ctx, w, req)
		return
	}
	b, err := c.backupRepo.GetLatest()
	if err != nil {
		respondWithError(w, err)
		return
	}
	httphelper.JSON(w, 200, b)
}

type sizeWriter struct {
	size int
	w    io.Writer
}

func (sw *sizeWriter) Write(p []byte) (int, error) {
	n, err := sw.w.Write(p)
	if err != nil {
		return n, err
	}
	sw.size = sw.size + n
	return n, nil
}

func (sw *sizeWriter) Close() error {
	if c, ok := sw.w.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (sw *sizeWriter) Size() int {
	return sw.size
}

func newSizeWriter(w io.Writer) *sizeWriter {
	return &sizeWriter{w: w}
}

func (c *controllerAPI) createAndStreamBackup(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/tar")
	filename := "flynn-backup-" + time.Now().UTC().Format("2006-01-02_150405") + ".tar"
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	handleError := func(err error) {
		if l, ok := ctxhelper.LoggerFromContext(ctx); ok {
			l.Error(err.Error())
			w.WriteHeader(500)
		}
	}

	client, err := controller.NewClient("", c.config.keys[0])
	if err != nil {
		handleError(err)
		return
	}

	b := &ct.ClusterBackup{
		Status: ct.ClusterBackupStatusRunning,
	}
	if err := c.backupRepo.Add(b); err != nil {
		handleError(err)
		return
	}

	h := sha512.New()
	hw := io.MultiWriter(h, w)
	sw := newSizeWriter(hw)

	if err := backup.Run(client, sw, nil); err != nil {
		b.Status = ct.ClusterBackupStatusError
		b.Error = err.Error()
		now := time.Now()
		b.CompletedAt = &now
		if err := c.backupRepo.Update(b); err != nil {
			handleError(err)
			return
		}
		handleError(err)
		return
	}

	b.Status = ct.ClusterBackupStatusComplete
	b.SHA512 = hex.EncodeToString(h.Sum(nil))
	b.Size = int64(sw.Size())
	now := time.Now()
	b.CompletedAt = &now
	if err := c.backupRepo.Update(b); err != nil {
		handleError(err)
	}
}
